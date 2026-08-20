// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// serverWebFS embeds the hub-only static dashboard assets.
//
//go:embed web/hub
var serverWebFS embed.FS

// RegisterHubRoutes mounts the hub dashboard (leaderboard, peer list, audit events, cluster
// topology) onto the client's own web server, under the /hub/ prefix, instead of listening on
// a separate port. Config editing is intentionally not duplicated here: the client dashboard's
// existing /api/config* endpoints already read and write the same config.json file, so the hub
// UI links out to those instead of re-implementing them. Called from StartClientWebDashboard
// only when server_mode.enabled is true.
func RegisterHubRoutes(mux *http.ServeMux, app *App) {
	if subFS, err := fs.Sub(serverWebFS, "web/hub"); err == nil {
		mux.Handle("/hub/", http.StripPrefix("/hub", http.FileServer(http.FS(subFS))))
	} else {
		logInfo("[ServerWeb] Failed to mount embedded web folder: %v", err)
	}

	mux.HandleFunc("/hub/api/peers", func(w http.ResponseWriter, r *http.Request) {
		peers, err := app.DB.GetAllPeers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if app.Rank != nil {
			sort.SliceStable(peers, func(i, j int) bool {
				scoreI := app.Rank.CalculateScore(peers[i].GPUInfo)
				scoreJ := app.Rank.CalculateScore(peers[j].GPUInfo)
				if scoreI == scoreJ {
					return peers[i].PeerID < peers[j].PeerID
				}
				return scoreI > scoreJ
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)
	})

	mux.HandleFunc("/hub/api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := app.DB.GetRecentEvents(100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("/hub/api/cluster_topology", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(app.ServerProxy.GetTopologyInfo())
	})

	mux.HandleFunc("/hub/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		board, err := app.DB.GetLeaderboard()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board)
	})

	mux.HandleFunc("/hub/api/stats", func(w http.ResponseWriter, r *http.Request) {
		peers, _ := app.DB.GetAllPeers()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(computeClusterStats(peers))
	})

	mux.HandleFunc("/hub/api/debug/force_rank", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if app.ServerProxy != nil {
			app.ServerProxy.reloadBackendsFromDB()
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"Forced rank update and proxy backend reload."}`))
	})

	mux.HandleFunc("/hub/api/debug/clear_offline", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"Offline peers cleared."}`))
	})

	logInfo("[ServerWeb] Hub dashboard mounted at /hub/ on the client web port")
}

// computeClusterStats aggregates per-peer GPUInfo snapshots into cluster-wide totals.
func computeClusterStats(peers []PeerData) map[string]interface{} {
	totalGPUs, totalActiveRequests := 0, 0
	var totalGenSpeed, totalPrefillSpeed, avgTTFT, avgKVCache float64
	var totalClusterTokens, totalInTokens, totalOutTokens, totalClusterRequests int64
	ttftCount, cacheCount := 0, 0

	for _, p := range peers {
		totalClusterTokens += p.TotalTokens
		totalInTokens += p.InTokens
		totalOutTokens += p.OutTokens
		totalClusterRequests += int64(p.TotalRequests)

		var info map[string]interface{}
		if err := json.Unmarshal([]byte(p.GPUInfo), &info); err != nil {
			continue
		}
		if v, ok := info["active_requests"].(float64); ok {
			totalActiveRequests += int(v)
		}
		if v, ok := info["gen_speed"].(float64); ok {
			totalGenSpeed += v
		}
		if v, ok := info["prefill_speed"].(float64); ok {
			totalPrefillSpeed += v
		}
		if v, ok := info["avg_ttft"].(float64); ok && v > 0 {
			avgTTFT += v
			ttftCount++
		}
		if v, ok := info["kv_cache_usage"].(float64); ok {
			avgKVCache += v
			cacheCount++
		}
		if summary, ok := info["summary"].(string); ok {
			totalGPUs += countGPUsInSummary(summary)
		}
	}

	if ttftCount > 0 {
		avgTTFT /= float64(ttftCount)
	}
	if cacheCount > 0 {
		avgKVCache /= float64(cacheCount)
	}

	return map[string]interface{}{
		"total_nodes":            len(peers),
		"total_gpus":             totalGPUs,
		"total_active_requests":  totalActiveRequests,
		"total_gen_speed":        totalGenSpeed,
		"total_prefill_speed":    totalPrefillSpeed,
		"avg_ttft":               avgTTFT,
		"avg_kv_cache":           avgKVCache,
		"total_cluster_tokens":   totalClusterTokens,
		"total_in_tokens":        totalInTokens,
		"total_out_tokens":       totalOutTokens,
		"total_cluster_requests": totalClusterRequests,
	}
}

// countGPUsInSummary extracts a GPU count from a summary string using the CJK counter
// suffix (e.g. a string containing a count token followed by the unit character).
func countGPUsInSummary(summary string) int {
	marker := string(rune(0x5f35)) // CJK counter for physical items, matches the client's summary format
	if !strings.Contains(summary, marker) {
		return 0
	}
	parts := strings.Split(summary, " ")
	if len(parts) < 2 {
		return 0
	}
	numStr := strings.TrimSuffix(parts[1], marker)
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	return num
}
