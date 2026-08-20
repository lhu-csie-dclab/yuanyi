// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// serverWebFS embeds the hub-only static dashboard assets.
//
//go:embed web/hub
var serverWebFS embed.FS

// StartServerWebDashboard starts the hub dashboard on server_mode.web_port (default 50005).
// All data is served from this node's local, gossip-replicated peers.db, so any hub can
// answer requests independently of the others.
func StartServerWebDashboard(app *App) {
	mux := http.NewServeMux()

	if subFS, err := fs.Sub(serverWebFS, "web/hub"); err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	} else {
		logInfo("[ServerWeb] Failed to mount embedded web folder: %v", err)
	}

	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := app.DB.GetRecentEvents(100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("/api/cluster_topology", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(app.ServerProxy.GetTopologyInfo())
	})

	mux.HandleFunc("/api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		board, err := app.DB.GetLeaderboard()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(board)
	})

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		peers, _ := app.DB.GetAllPeers()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(computeClusterStats(peers))
	})

	mux.HandleFunc("/api/debug/force_rank", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/debug/clear_offline", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"Offline peers cleared."}`))
	})

	mux.HandleFunc("/api/config", handleHubConfigGetSet)
	mux.HandleFunc("/api/config/backups", handleHubConfigBackups)
	mux.HandleFunc("/api/config/restore", handleHubConfigRestore)

	port := app.Config.ServerMode.WebPort
	if port <= 0 {
		port = 50005
	}
	logInfo("[ServerWeb] Hub dashboard listening on :%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		logError("[ServerWeb] Failed to start: %v", err)
	}
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

// handleHubConfigGetSet serves and updates the shared config.json, timestamp-backing up
// the previous version on every write. It shares the same file as the client dashboard.
func handleHubConfigGetSet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data, err := os.ReadFile("config.json")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	if r.Method == http.MethodPost {
		os.MkdirAll("backups", 0755)
		if currentData, err := os.ReadFile("config.json"); err == nil {
			backupName := fmt.Sprintf("backups/config_%s.json", time.Now().Format("20060102_150405"))
			os.WriteFile(backupName, currentData, 0644)
		}

		newData, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("config.json", newData, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"Config saved and backed up successfully."}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleHubConfigBackups lists available config.json backups, newest first.
func handleHubConfigBackups(w http.ResponseWriter, r *http.Request) {
	files, err := os.ReadDir("backups")
	var backups []string
	if err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasPrefix(f.Name(), "config_") && strings.HasSuffix(f.Name(), ".json") {
				backups = append(backups, f.Name())
			}
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i] > backups[j] })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(backups)
}

// handleHubConfigRestore restores a named config.json backup, safety-backing up the
// current file first.
func handleHubConfigRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	backupData, err := os.ReadFile(filepath.Join("backups", req.Filename))
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	if currentData, err := os.ReadFile("config.json"); err == nil {
		os.MkdirAll("backups", 0755)
		newBackup := fmt.Sprintf("backups/config_%s_before_restore.json", time.Now().Format("20060102_150405"))
		os.WriteFile(newBackup, currentData, 0644)
	}

	if err := os.WriteFile("config.json", backupData, 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"Config restored successfully."}`))
}
