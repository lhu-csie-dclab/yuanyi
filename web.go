// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main implements the Web Dashboard HTTP server and static asset embedding.
package main

import (
	"context"       // 上下文機制 (用於向中央 Leaderboard 請求時設定超時控制)
	"embed"         // Go 1.16+ 靜態資源內嵌框架
	"encoding/json" // JSON 序列化與反序列化
	"fmt"           // 格式化字串與檔名組合
	"io"            // 基礎流讀取 (io.ReadAll)
	"io/fs"         // 虛擬檔案系統介面 (fs.Sub)
	"net/http"      // HTTP 伺服器與路由 Mux
	"os"            // 檔案讀寫與備份目錄建立 (os.ReadFile, os.WriteFile, os.ReadDir)
	"sort"          // 備份檔案切片降序排序
	"strconv"       // 埠號轉字串 (strconv.Itoa)
	"strings"       // 字串切割與前綴/後綴檢查
	"time"          // 備份檔名時間戳記格式化 (Format)
)

// webFS 利用 Go 特殊編譯標籤將 Vue + Vite 建置產出的 web-ui/dist/ 靜態資源打入二進位執行檔中。
// dist/ 由 Dockerfile 的 Node 建置階段（或本機 `npm run build`）產生，不進版控；
// 部署時無需隨附外部網頁資料夾，達成單一可執行檔運行的目標。
//
//go:embed web-ui/dist
var webFS embed.FS

// StartClientWebDashboard 啟動提供給管理者檢視的 Web UI HTTP 監控服務。
// 【邏輯說明與 RESTful API 路由大綱】
// 1. 初始化 http.NewServeMux() 建立獨立 HTTP 路由分發器。
// 2. 靜態 UI 資源掛載 (`GET /`)：使用 fs.Sub(webFS, "web") 提取子目錄，掛載至 http.FileServer 供前端渲染。
// 3. `GET /api/peers`：讀取並傳回全網已發現的 P2P 鄰居節點清單。
// 4. `GET /api/node_info`：傳回本機 PeerID 與中央 Bootstrap 伺服器 Host 地址。
// 5. `GET /api/local_stats`：取得本地推論數據，並發起 2 秒超時請求向中央 Leaderboard API 同步數據。
// 6. `GET /api/logs`：取得系統日誌、vLLM 控制台日誌與 Docker 容器日誌等 3 類日誌。
// 7. `GET /api/stats`：計算整個 P2P 叢集的總節點數、總吞吐速率、平均首字延遲 (TTFT) 與平均 KV Cache 佔用率。
// 8. `GET/POST /api/config`：讀取或線上修訂 config.json 設定檔 (POST 時自動進行時間戳記備份)。
// 9. `GET /api/config/backups`：讀取 backups/ 目錄，傳回所有已存檔的備份檔名清單 (降序排列)。
// 10. `POST /api/config/restore`：接收檔名並還原指定的設定檔 (還原前自動進行二次安全防禦備份)。
// 11. 在指定 WebPort (預設 50007) 上背景啟動 HTTP 伺服器。
func StartClientWebDashboard(app *App) {
	mux := http.NewServeMux()

	// 步驟 1: 掛載內嵌的 Vue SPA 建置產出至 "/" 根路徑 (hash-based routing，Go 端不需要 SPA fallback)
	subFS, err := fs.Sub(webFS, "web-ui/dist")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	} else {
		app.TUI.AddLog("[ERROR]", fmt.Sprintf("[Web] Embedded folder mount error: %v", err)) // 至 tui.go 記錄錯誤日誌
	}

	// 步驟 2: API 端點 - 取得已發現的 P2P 鄰居節點清單 (GET /api/peers)
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		peerMap := app.TUI.GetPeers() // 至 tui.go 取得線上鄰居節點清單 (map[nodeID]GPUInfo)
		// The web-ui's TopologyView treats this response as an array (.length, .slice() for
		// pagination) -- encoding the map directly serializes it as a JSON object instead,
		// which silently breaks both: peers.length is undefined and .slice() throws on every
		// poll tick, so the peer table never renders even when peers are actually known.
		peers := make([]GPUInfo, 0, len(peerMap))
		for _, info := range peerMap {
			peers = append(peers, info)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(peers)
	})

	// 步驟 3: API 端點 - 取得本機 PeerID 與 Central Server Host (GET /api/node_info)
	mux.HandleFunc("/api/node_info", func(w http.ResponseWriter, r *http.Request) {
		localID := ""
		if app.P2P != nil && app.P2P.host != nil {
			localID = app.P2P.host.ID().String()
		}

		serverHost := "127.0.0.1"
		parts := strings.Split(app.Config.P2P.ServerAddress, "/")
		for i, p := range parts {
			if (p == "ip4" || p == "dns4" || p == "dns") && i+1 < len(parts) {
				serverHost = parts[i+1]
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"local_node_id":    localID,
			"server_host":      serverHost,
			"hub_mode_enabled": app.Config.ServerMode.Enabled,
			// A relay-only node runs no local GPU workload, but its own dashboard/logs
			// otherwise give no persistent, at-a-glance confirmation of that: the only prior
			// signal was a one-time startup log line and other peers' GPU telemetry
			// scrolling through the same log view. Expose it so the web UI can show an
			// explicit "Relay-only" badge instead of leaving the operator to infer their own
			// node's role from ambiguous log text.
			"relay_only": app.Config.ServerMode.RelayOnly,
			// Expose ports and model so the web UI can auto-configure the chat endpoint.
			"vllm_port":  app.Config.VLLM.Port,
			"proxy_port": app.Config.ProxyPort,
			"model_name": app.Config.VLLM.ModelName,
		})
	})

	// 步驟 4: API 端點 - 取得本機統計數據，並向 Central Server Leaderboard 校驗數據 (GET /api/local_stats)
	mux.HandleFunc("/api/local_stats", func(w http.ResponseWriter, r *http.Request) {
		stats := app.TUI.GetLocalStats() // 至 tui.go 取得本地統計

		// 本節點自身的 CPU/記憶體用量 -- 純本機資料，來自 sys.go 的快取值，不經過 gossip 廣播。
		if app.Sys != nil {
			cpuPct, memRSS := app.Sys.GetProcessStats()
			stats["cpu_percent"] = cpuPct
			stats["mem_rss_mb"] = float64(memRSS) / (1024 * 1024)
		}

		localID := ""
		if app.P2P != nil && app.P2P.host != nil {
			localID = app.P2P.host.ID().String()
		}

		serverHost := "127.0.0.1"
		parts := strings.Split(app.Config.P2P.ServerAddress, "/")
		for i, p := range parts {
			if (p == "ip4" || p == "dns4" || p == "dns") && i+1 < len(parts) {
				serverHost = parts[i+1]
				break
			}
		}

		// 向 Central Server API 查詢排行榜數據以校正本地數值
		if localID != "" {
			cctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			reqURL := fmt.Sprintf("http://%s:50007/hub/api/leaderboard", serverHost) // assumes the bootstrap node runs the default web_port with hub mode enabled
			req, err := http.NewRequestWithContext(cctx, "GET", reqURL, nil)
			if err == nil {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					defer resp.Body.Close()
					var board []struct {
						PeerID        string `json:"peer_id"`
						TotalRequests int64  `json:"total_requests"`
						TotalTokens   int64  `json:"total_tokens"`
						InTokens      int64  `json:"in_tokens"`
						OutTokens     int64  `json:"out_tokens"`
					}
					if json.NewDecoder(resp.Body).Decode(&board) == nil {
						for _, item := range board {
							if item.PeerID == localID {
								// 取較大者校正本地統計
								totTok, _ := stats["total_tokens"].(int64)
								if item.TotalTokens > totTok {
									stats["total_tokens"] = item.TotalTokens
								}
								inTok, _ := stats["in_tokens"].(int64)
								if item.InTokens > inTok {
									stats["in_tokens"] = item.InTokens
								}
								outTok, _ := stats["out_tokens"].(int64)
								if item.OutTokens > outTok {
									stats["out_tokens"] = item.OutTokens
								}
								totReq, _ := stats["total_requests"].(int64)
								if item.TotalRequests > totReq {
									stats["total_requests"] = item.TotalRequests
								}
								break
							}
						}
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// 步驟 5: API 端點 - 取得全套系統日誌副本 (GET /api/logs)
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		sys, vllm, docker := app.TUI.GetLogs() // 至 tui.go 取得現有日誌
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sys_logs":    sys,
			"vllm_logs":   vllm,
			"docker_logs": docker,
		})
	})

	// 步驟 6: API 端點 - 計算全 P2P 叢集總體統計 (GET /api/stats)
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		peers := app.TUI.GetPeers() // 至 tui.go 取得線上鄰居清單
		totalNodes := len(peers)
		var totalGenSpeed, totalPrefillSpeed, avgTTFT, avgKVCache float64
		var totalActiveRequests int
		var totalTokens, inTokens, outTokens, totalRequests int64
		var totalPowerDraw, totalPowerLimit float64
		count := 0

		// 遍歷所有線上鄰居累加指標
		for _, info := range peers {
			totalActiveRequests += info.ActiveRequests
			totalGenSpeed += info.GenSpeed
			totalPrefillSpeed += info.PrefillSpeed
			if info.AvgTTFT > 0 {
				avgTTFT += info.AvgTTFT
				count++
			}
			avgKVCache += info.KVCacheUsage
			totalTokens += info.TotalTokens
			inTokens += info.InTokens
			outTokens += info.OutTokens
			totalRequests += info.TotalRequests
			// info.PowerDraw/PowerLimit are already each peer's own multi-GPU max (see
			// sys.go's GetGPUTelemetry), not that peer's own multi-GPU sum, so a node with
			// several cards under-contributes here versus its true total draw -- this is a
			// cluster-wide lower bound, not an exact wattage sum.
			totalPowerDraw += info.PowerDraw
			totalPowerLimit += info.PowerLimit
		}

		// 計算全網平均值
		if count > 0 {
			avgTTFT /= float64(count)
		}
		if totalNodes > 0 {
			avgKVCache /= float64(totalNodes)
		}

		stats := map[string]interface{}{
			"total_nodes":           totalNodes,
			"total_active_requests": totalActiveRequests,
			"total_gen_speed":       totalGenSpeed,
			"total_prefill_speed":   totalPrefillSpeed,
			"avg_ttft":              avgTTFT,
			"avg_kv_cache":          avgKVCache,
			// Cluster-wide cumulative totals, summed from each peer's self-reported GPUInfo
			// (see p2p.go's gossipPublisher) -- distinct from total_active_requests above,
			// which is a live in-flight count, not a running total.
			"total_tokens":   totalTokens,
			"in_tokens":      inTokens,
			"out_tokens":     outTokens,
			"total_requests": totalRequests,
			// See the loop comment above: a lower-bound approximation on multi-GPU nodes,
			// not an exact sum, since each peer only reports its single highest-draw GPU.
			"total_power_draw":  totalPowerDraw,
			"total_power_limit": totalPowerLimit,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// 步驟 7: API 端點 - 讀取或線上儲存 config.json (GET/POST /api/config)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		// 分支 7.1: GET 請求 -> 傳回本機 config.json 內文
		if r.Method == http.MethodGet {
			data, err := os.ReadFile("config.json")
			if err != nil {
				data, _ = json.MarshalIndent(app.Config, "", "  ")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			return
		}

		// 分支 7.2: POST 請求 -> 保存新設定檔，寫入前自動在 backups/ 備份舊檔
		if r.Method == http.MethodPost {
			os.MkdirAll("backups", 0755)
			currentData, err := os.ReadFile("config.json")
			if err == nil {
				// 建立帶有時間戳記的舊檔備份 (config_YYYYMMDD_HHMMSS.json)
				backupName := fmt.Sprintf("backups/config_%s.json", time.Now().Format("20060102_150405"))
				os.WriteFile(backupName, currentData, 0644)
			}

			newData, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// 覆寫本機 config.json
			if err := os.WriteFile("config.json", newData, 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok", "message": "Config saved and backed up successfully."}`))
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// 步驟 8: API 端點 - 列出所有的歷史備份設定檔 (GET /api/config/backups)
	mux.HandleFunc("/api/config/backups", func(w http.ResponseWriter, r *http.Request) {
		files, err := os.ReadDir("backups")
		var backups []string
		if err == nil {
			for _, f := range files {
				if !f.IsDir() && strings.HasPrefix(f.Name(), "config_") && strings.HasSuffix(f.Name(), ".json") {
					backups = append(backups, f.Name())
				}
			}
		}
		// 按檔名進行降序排序 (最新建立的備份檔排最前)
		sort.Slice(backups, func(i, j int) bool { return backups[i] > backups[j] })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(backups)
	})

	// 步驟 9: API 端點 - 還原指定的歷史備份設定檔 (POST /api/config/restore)
	mux.HandleFunc("/api/config/restore", func(w http.ResponseWriter, r *http.Request) {
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

		backupPath := "backups/" + req.Filename
		backupData, err := os.ReadFile(backupPath)
		if err != nil {
			http.Error(w, "Backup file not found", http.StatusNotFound)
			return
		}

		// 還原前做一次二次安全備份 (config_before_restore_...)
		os.MkdirAll("backups", 0755)
		currentData, err := os.ReadFile("config.json")
		if err == nil {
			safetyBackup := fmt.Sprintf("backups/config_before_restore_%s.json", time.Now().Format("20060102_150405"))
			os.WriteFile(safetyBackup, currentData, 0644)
		}

		// 將備份檔案內容寫回 config.json
		if err := os.WriteFile("config.json", backupData, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok", "message": "Config restored successfully."}`))
	})

	// 步驟 10: 若本機開啟 server_mode，將 Hub 儀表板掛載於同一個 mux 的 /hub/ 路徑下，
	// 不再另外監聽獨立埠號。
	if app.Config.ServerMode.Enabled && app.DB != nil {
		RegisterHubRoutes(mux, app) // 至 server_web.go 掛載 Hub 儀表板路由
	}

	// 步驟 11: 讀取監聽埠並啟動背景 HTTP 伺服器
	port := app.Config.WebPort
	if port <= 0 {
		port = 50007 // 預設 50007 埠
	}

	app.TUI.AddLog("[INFO]", fmt.Sprintf("Web Dashboard listening on http://localhost:%d", port)) // 至 tui.go 記錄啟動資訊
	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(port), mux); err != nil {
			app.TUI.AddLog("[ERROR]", fmt.Sprintf("Web Dashboard 啟動失敗: %v", err)) // 至 tui.go 記錄錯誤日誌
		}
	}()
}
