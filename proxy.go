// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main implements the OpenAI-compatible API Gateway and Local-First proxy dispatcher.
package main

import (
	"bufio"           // 帶緩衝區的讀取器 (用於 HTTP 協定串流讀寫)
	"bytes"           // 位元組緩衝區操作
	"context"         // 上下文機制，用於超時與取消控制
	"encoding/binary" // 大小端序位元組轉碼 (BigEndian uint16)
	"encoding/json"   // JSON 序列化與反序列化
	"fmt"             // 格式化字串與錯誤包裝
	"io"              // 基礎 IO 流複製與讀取
	"net/http"        // HTTP 協定客戶端與伺服器端實作
	"strconv"         // 字串轉數字
	"strings"         // 字串切割與搜尋
	"sync"            // 讀寫鎖 (RWMutex)
	"sync/atomic"     // 原子操作 (atomic.Bool)
	"time"            // 時間與定時器 (Ticker)

	"github.com/libp2p/go-libp2p/core/host" // libp2p 通訊主機介面
	"github.com/libp2p/go-libp2p/core/peer" // libp2p PeerID 與節點結構
)

// BackendInfo 描述一個可以接受 Prefill (首字計算) 或 Decode (文字生成) 任務的實際 vLLM 後端節點資訊。
type BackendInfo struct {
	PeerID        string `json:"peer_id"`        // 節點唯一的 libp2p PeerID 字串
	IPAddress     string `json:"ip_address"`     // 節點的實體 IP 位址
	BootstrapAddr string `json:"bootstrap_addr"` // Mooncake KV Cache 傳輸協商控制端點
	EngineID      string `json:"engine_id"`      // Mooncake 引擎識別碼
}

// ClusterTopologyResponse 描述由 Central Server 計算出的整體叢集 P/D (Prefill/Decode) 拓樸視圖。
type ClusterTopologyResponse struct {
	PrefillNodes    int           `json:"prefill_nodes"`    // 專用 Prefill 節點數量
	DecodeNodes     int           `json:"decode_nodes"`     // 專用 Decode 節點數量
	IsPDTogether    bool          `json:"is_pd_together"`   // true 表示傳統混和模式 (所有節點同時處理 Prefill/Decode)
	PrefillBackends []BackendInfo `json:"prefill_backends"` // 可用 Prefill 節點清單
	DecodeBackends  []BackendInfo `json:"decode_backends"`  // 可用 Decode 節點清單
}

// LocalDispatcher 本地 API Gateway 核心結構，實現類似 OpenAI API 代理分發器角色。
type LocalDispatcher struct {
	app         *App                    // 指向根容器 App 的指標
	host        host.Host               // libp2p 主機實例
	decodeIndex int                     // 輪詢 (Round-Robin) 節點計數器
	topology    ClusterTopologyResponse // 最新同步的叢集拓樸結構快照
	mu          sync.RWMutex            // 保護 topology 快照與 decodeIndex 的讀寫鎖
	vllmReady   atomic.Bool             // 本機 vLLM 是否已就緒 (通過 /health 確認)
	localBusy   atomic.Bool             // 本機是否正在處理另一筆請求；用來讓併發請求分流至 P2P 而非全部排本機隊
}

// NewLocalDispatcher 建構函式：建立 LocalDispatcher 實例。
func NewLocalDispatcher(app *App, h host.Host) *LocalDispatcher {
	d := &LocalDispatcher{
		app:      app,
		host:     h,
		topology: ClusterTopologyResponse{IsPDTogether: true}, // 預設採用 PD-Together 混和模式
	}
	go d.startVLLMHealthChecker() // 背景輪詢等待本機 vLLM 就緒
	return d
}

// startVLLMHealthChecker 背景輪詢本機 vLLM /health 端點，直到回應 200 為止。
// 一旦確認 vLLM 就緒，將 vllmReady 設為 true 並記錄日誌。
func (d *LocalDispatcher) startVLLMHealthChecker() {
	vllmPort := d.app.Config.VLLM.Port
	if vllmPort <= 0 {
		vllmPort = 8100
	}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", vllmPort)
	client := &http.Client{Timeout: 3 * time.Second}

	d.app.TUI.AddLog("[vLLM]", fmt.Sprintf("等待本機 vLLM 就緒 (http://127.0.0.1:%d/health)...", vllmPort))
	for {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				d.vllmReady.Store(true)
				d.app.TUI.AddLog("[vLLM]", fmt.Sprintf("本機 vLLM 已就緒! 所有請求將優先由本機 GPU 處理。"))
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
}

// syncTopologyLoop 背景 Goroutine (每 10 秒執行一次)：
// 向 Bootstrap/Hub 節點發起 HTTP GET 請求，同步最新的 P/D 叢集拓樸視圖。
// 【邏輯說明】
//  1. 從 config.json 的 ServerAddress (Multiaddress) 中拆解出 Bootstrap 節點的 Host 位址。
//  2. 構造 http://<serverHost>:50007/hub/api/cluster_topology URL
//     （Hub 儀表板現在掛在 client 自己的 web_port 底下的 /hub/ 路徑，不再有獨立埠號）。
//  3. 發起 5 秒超時的 HTTP 請求，反序列化 response JSON 並更新至 d.topology。
func (d *LocalDispatcher) syncTopologyLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// 步驟 1: 解析 Server 主機 IP/域名
	serverAddr := d.app.Config.P2P.ServerAddress
	serverHost := "127.0.0.1"
	parts := strings.Split(serverAddr, "/")
	for i, p := range parts {
		if (p == "ip4" || p == "dns4" || p == "dns") && i+1 < len(parts) {
			serverHost = parts[i+1]
			break
		}
	}

	webPort := 50007 // assumes the bootstrap/hub node runs the default web_port with hub mode enabled
	serverURL := fmt.Sprintf("http://%s:%d/hub/api/cluster_topology", serverHost, webPort)

	// 步驟 2: 發起一次同步的封裝函式
	syncOnce := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", serverURL, nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		var top ClusterTopologyResponse
		if err := json.NewDecoder(resp.Body).Decode(&top); err == nil {
			d.mu.Lock()
			d.topology = top
			d.mu.Unlock()
			d.app.TUI.AddLog("[SYNC]", fmt.Sprintf("同步 Server P/D 拓樸成功 (IsPDTogether: %v, P: %d, D: %d)", top.IsPDTogether, len(top.PrefillBackends), len(top.DecodeBackends))) // 至 tui.go 記錄同步日誌
		}
	}

	// 靜態呼叫首次同步，隨後定時器輪詢
	syncOnce()
	for range ticker.C {
		syncOnce()
	}
}

// streamToPeer 透過 libp2p ProxyProtocol 串流將 HTTP 請求轉發給指定的遠端 P2P 節點。
// 【邏輯說明】
// 1. 解碼 PeerID 字串為 peer.ID。
// 2. 呼叫 host.NewStream 開啟 /mooncake-proxy/1.0.0 流。
// 3. 以 BigEndian uint16 寫入目標 Ports (通常為 8100 埠)。
// 4. 重組並使用 req.Write(stream) 將 HTTP 請求寫入串流。
// 5. 呼叫 http.ReadResponse 讀取遠端節點的回應內容並回傳 `[]byte`。
func (d *LocalDispatcher) streamToPeer(ctx context.Context, peerIDStr string, path string, reqBytes []byte) ([]byte, error) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, err
	}

	stream, err := d.host.NewStream(ctx, pid, ProxyProtocolID)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	vllmPort := d.app.Config.VLLM.Port
	if vllmPort <= 0 {
		vllmPort = 8100
	}

	// 前置寫入 2 個位元組的目標連線埠
	if err := binary.Write(stream, binary.BigEndian, uint16(vllmPort)); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = fmt.Sprintf("127.0.0.1:%d", vllmPort)

	// 寫入原始 HTTP 請求數據
	if err := req.Write(stream); err != nil {
		return nil, err
	}

	// 讀取遠端傳回的 HTTP 回應
	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// getPortFromURL 從網址字串 (例如 "http://127.0.0.1:8998/...") 中解析出 Port 數字。
func getPortFromURL(u string) uint16 {
	parts := strings.Split(u, ":")
	if len(parts) > 2 {
		portStr := parts[2]
		portStr = strings.Split(portStr, "/")[0]
		if p, err := strconv.Atoi(portStr); err == nil {
			return uint16(p)
		}
	}
	return 8998 // 預設 8998
}

// handleKVTunnel 處理 Mooncake KV Cache 專屬傳輸隧道的 HTTP 代理請求 (/mooncake_kv/...)。
// 【邏輯說明】
// URL 格式為 "/mooncake_kv/<targetPeerID>/<targetPort>/<path...>"
// 1. 解析 URL 路徑提取 targetPeerID 與 targetPort。
// 2. 透過 libp2p 建立前往目標 Peer 的 ProxyProtocolID 串流。
// 3. 前置寫入 targetPort，隨後將帶有原始 Header/Query/Body 的 Request 原封不動轉發。
// 4. 收到 Response 後，將 Header、StatusCode 與 Body 原樣串流寫回客戶端 (Response-Writer)。
func (d *LocalDispatcher) handleKVTunnel(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(r.URL.Path, "/", 5)
	if len(parts) < 4 {
		http.Error(w, "Invalid KV tunnel path", http.StatusBadRequest)
		return
	}
	targetPeerID := parts[2]
	portStr := parts[3]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		http.Error(w, "Invalid port", http.StatusBadRequest)
		return
	}
	targetPath := "/"
	if len(parts) == 5 {
		targetPath = "/" + parts[4]
	}
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	pid, err := peer.Decode(targetPeerID)
	if err != nil {
		http.Error(w, "Invalid peer ID", http.StatusBadRequest)
		return
	}

	stream, err := d.host.NewStream(r.Context(), pid, ProxyProtocolID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to connect to peer: %v", err), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	if err := binary.Write(stream, binary.BigEndian, uint16(port)); err != nil {
		http.Error(w, "Failed to write port", http.StatusBadGateway)
		return
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetPath, r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	outReq.Header = r.Header.Clone()
	outReq.Host = fmt.Sprintf("127.0.0.1:%d", port)

	if err := outReq.Write(stream); err != nil {
		http.Error(w, "Failed to write request", http.StatusBadGateway)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(stream), outReq)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 寫回 HTTP 響應 Header
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) // 複製回應內文
}

// writeJSONError 將錯誤訊息包裝為標準 OpenAI API JSON 格式回傳，避免客戶端出現 JSONDecodeError。
func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	errObj := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	}
	json.NewEncoder(w).Encode(errObj)
}

// proxyToLocalVLLMDirect 透明串流代理：直接將客戶端請求轉發至本機 vLLM，並將 vLLM 回應(包含 SSE 串流)原封不動地 pipe 回客戶端。
// 這是壓力測試相容的核心函式，支援 stream:true (SSE) 與 stream:false (JSON) 模式，零緩衝、零延遲。
// 若 vLLM 尚未就緒，回傳 false 讓呼叫方靜默轉至 P2P。
func (d *LocalDispatcher) proxyToLocalVLLMDirect(w http.ResponseWriter, r *http.Request, reqBytes []byte) bool {
	if !d.vllmReady.Load() {
		return false
	}

	vllmPort := d.app.Config.VLLM.Port
	if vllmPort <= 0 {
		vllmPort = 8100
	}
	targetURL := fmt.Sprintf("http://127.0.0.1:%d%s", vllmPort, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(reqBytes))
	if err != nil {
		return false
	}
	// 轉發原始 Request Headers (包含 Accept, Authorization 等)
	req.Header = r.Header.Clone()
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 連線失敗，重置就緒狀態讓 health checker 重新確認
		d.vllmReady.Store(false)
		go d.startVLLMHealthChecker()
		return false
	}
	defer resp.Body.Close()

	// 原封不動地複製所有 Response Headers (包含 Content-Type: text/event-stream 等)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)

	// 透明 Pipe：直接將 vLLM 的回應串流 pipe 給客戶端，零緩衝
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush() // 即時推送每個 SSE chunk
			}
		}
		if readErr != nil {
			break
		}
	}

	// 統計 metrics (非串流模式才能從 body 中解析 token 數，串流模式估算)
	d.app.TUI.UpdateStats(func(st *Stats) {
		st.requests++
		st.decode++
		st.successCount++
	})
	return true
}

// streamToLocalVLLM 緩衝模式：用於 P/D 分離模式的 Prefill 階段，需要讀取完整回應進行判斷。
// 注意：此函式不支援串流，僅用於非最終輸出的中間處理步驟。
func (d *LocalDispatcher) streamToLocalVLLM(ctx context.Context, path string, reqBytes []byte) ([]byte, error) {
	if !d.vllmReady.Load() {
		return nil, fmt.Errorf("vLLM 尚未就緒 (warming up)")
	}

	vllmPort := d.app.Config.VLLM.Port
	if vllmPort <= 0 {
		vllmPort = 8100
	}
	targetURL := fmt.Sprintf("http://127.0.0.1:%d%s", vllmPort, path)

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.vllmReady.Store(false)
		go d.startVLLMHealthChecker()
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// recordMetrics 解析推論回應 JSON 並將 Token 使用量更新至 TUI 統計中。
// 【邏輯說明】
// 解析 vLLM 標準回應結構中的 `usage.prompt_tokens` 與 `usage.completion_tokens`，
// 呼叫 tui.go 的 UpdateStats 方法更新輸入/輸出 Token 累計與成功/失敗計數。
func (d *LocalDispatcher) recordMetrics(respBytes []byte, err error, isPrefill bool) {
	d.app.TUI.UpdateStats(func(st *Stats) { // 至 tui.go 安全更新即時統計
		st.requests++
		if isPrefill {
			st.prefill++
		} else {
			st.decode++
		}
		if err != nil || len(respBytes) == 0 {
			st.errorCount++
			return
		}

		var respObj struct {
			Error interface{} `json:"error"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(respBytes, &respObj) == nil && respObj.Error == nil {
			st.inTokens += int64(respObj.Usage.PromptTokens)
			st.outTokens += int64(respObj.Usage.CompletionTokens)
			st.successCount++
		} else {
			st.errorCount++
		}
	})
}

// handleProxyRequest OpenAI 相容 API 代理核心處理解析與分發進入點 (/v1/chat/completions 與 /v1/completions)。
// 【邏輯說明與 Mode 分支解析】
// 1. CORS 與請求方法檢查：回應 Allow-Origin，僅接受 POST 方法。
// 2. 讀取並反序列化 Request Body JSON，讀取 "model" 名稱。
// 3. 讀取目前的拓樸狀態 d.topology。
// 4. 【Mode 1: PD-Together 混和模式】(top.IsPDTogether == true 或無專用節點)
//   - 取得線上所有的已知 PeerID 列表。
//   - 進行 Round-Robin 輪詢選取 targetPeerID。
//   - 若選中本機則呼叫 streamToLocalVLLM，選中遠端則呼叫 streamToPeer。
//   - 若遠端連線失敗，自動降級發送給本機 (Local Fallback) 作為備援。
//
// 5. 【Mode 2: P/D 獨立分離模式】(top.IsPDTogether == false)
//   - 輪詢選出專用 Prefill 節點 (prefillBackend) 與 Decode 節點 (decodeBackend)。
//   - 階段 1 (Prefill)：構造帶有 `max_tokens: 1`, `mooncake_peer`, `mooncake_engine` 的請求發送至 Prefill 節點，完成 Prompt KV Cache 預計算與傳送。
//   - 階段 2 (Decode)：發送帶有預計算 KV 參照的解碼請求給 Decode 節點，產出最終結果回應給客戶端。
func (d *LocalDispatcher) handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	// 步驟 1: 設定跨域 CORS Header
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		writeJSONError(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	// 步驟 2: 讀取並解析請求內文 JSON
	body, err := io.ReadAll(r.Body)
	if err != nil {
		d.recordMetrics(nil, err, false) // 紀錄失敗指標
		writeJSONError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(body, &reqData); err != nil {
		d.recordMetrics(nil, err, false) // 紀錄失敗指標
		writeJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var modelName string
	if m, ok := reqData["model"].(string); ok {
		modelName = m
	} else {
		modelName = "mooncake-default"
	}

	d.mu.RLock()
	top := d.topology
	d.mu.RUnlock()

	maxRetries := 3

	// ==========================================
	// === Mode 1: 本地優先 & PD-Together 混和模式 ===
	// ==========================================
	if top.IsPDTogether || (len(top.PrefillBackends) == 0 && len(top.DecodeBackends) == 0) {
		reqBytes, _ := json.Marshal(reqData)

		// 步驟 1: 只有在本機 vLLM 就緒「且目前沒有其他請求正在使用本機」時才走本機直通。
		// 用 CompareAndSwap 搶佔 localBusy 這個名額：搶到才執行本機，執行完（不論成敗）立刻釋放。
		// 這讓單一請求仍走最快的本機路徑，但多筆併發請求會自動分流到 P2P 遠端節點，
		// 而不是全部排在同一張本機 GPU 的隊列裡（不再只看「本機是否健康」，而是看「本機是否有空」）。
		if d.vllmReady.Load() && d.localBusy.CompareAndSwap(false, true) {
			d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("本地 vLLM 處理 (模型: %s)", modelName))
			ok := d.proxyToLocalVLLMDirect(w, r, reqBytes)
			d.localBusy.Store(false)
			if ok {
				return // 成功：已直接 pipe 回應給客戶端
			}
			d.app.TUI.AddLog("[WARN]", "本地 vLLM 未回應，自動切換至 P2P 遠端節點備援...")
		} else if d.vllmReady.Load() {
			d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("本地 vLLM 忙碌中 (模型: %s)，分派至 P2P 遠端節點...", modelName))
		}
		knownPeers := d.app.TUI.GetPeers()
		var peerIDs []string
		for _, p := range knownPeers {
			if p.NodeID != "" && p.NodeID != d.host.ID().String() {
				peerIDs = append(peerIDs, p.NodeID)
			}
		}

		for attempt := 1; attempt <= maxRetries; attempt++ {
			if len(peerIDs) > 0 {
				d.mu.Lock()
				d.decodeIndex = (d.decodeIndex + 1) % len(peerIDs)
				targetPeerID := peerIDs[d.decodeIndex]
				d.mu.Unlock()

				// 檢查遠端位址，過濾私有不可達 IP
				if pid, pErr := peer.Decode(targetPeerID); pErr == nil {
					if len(d.host.Peerstore().Addrs(pid)) == 0 {
						continue
					}
				}

				d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("P2P 備援轉發 %s -> 遠端節點: %s (嘗試 %d/%d)", modelName, targetPeerID[:8], attempt, maxRetries))
				respBytes, err := d.streamToPeer(r.Context(), targetPeerID, r.URL.Path, reqBytes)
				if err == nil {
					d.recordMetrics(respBytes, nil, false)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write(respBytes)
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
		}

		// 步驟 3: 若本地與遠端備援皆失敗，紀錄失敗指標並回傳 HTTP 502
		d.recordMetrics(nil, err, false)
		writeJSONError(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
		return
	}

	// ==========================================
	// === Mode 2: P/D 獨立分離模式 ===
	// ==========================================
	rawReqBytes, _ := json.Marshal(reqData)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		d.mu.Lock()
		if len(top.PrefillBackends) == 0 || len(top.DecodeBackends) == 0 {
			d.mu.Unlock()
			// 降級由本地 vLLM 執行
			respBytes, err := d.streamToLocalVLLM(r.Context(), r.URL.Path, rawReqBytes)
			if err == nil && len(respBytes) > 0 {
				d.recordMetrics(respBytes, nil, false)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write(respBytes)
				return
			}
			d.recordMetrics(nil, fmt.Errorf("no backends"), false) // 紀錄失敗指標
			writeJSONError(w, "No P/D backends available", http.StatusServiceUnavailable)
			return
		}

		prefillBackend := top.PrefillBackends[0]
		d.decodeIndex = (d.decodeIndex + 1) % len(top.DecodeBackends)
		decodeBackend := top.DecodeBackends[d.decodeIndex]
		d.mu.Unlock()

		d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("P/D 分離請求 %s -> Prefill: %s, Decode: %s", modelName, prefillBackend.PeerID[:8], decodeBackend.PeerID[:8])) // 至 tui.go 記錄 P/D 日誌

		// 階段 1: 發送 Prefill (指定生成 1 個 Token 並要求將 KV 傳送至 Decode 節點)
		prefillReq := make(map[string]interface{})
		for k, v := range reqData {
			prefillReq[k] = v
		}
		prefillReq["max_tokens"] = 1
		prefillReq["ignore_eos"] = true
		prefillReq["temperature"] = 0.0
		prefillReq["mooncake_peer"] = decodeBackend.BootstrapAddr
		prefillReq["mooncake_engine"] = decodeBackend.EngineID
		prefillBytes, _ := json.Marshal(prefillReq)

		var prefillResp []byte
		var err error
		if prefillBackend.PeerID == d.host.ID().String() {
			prefillResp, err = d.streamToLocalVLLM(r.Context(), r.URL.Path, prefillBytes) // Prefill 本地執行
		} else {
			prefillResp, err = d.streamToPeer(r.Context(), prefillBackend.PeerID, r.URL.Path, prefillBytes) // Prefill 遠端轉發
		}

		if err != nil {
			d.app.TUI.AddLog("[WARN]", fmt.Sprintf("Prefill 階段失敗: %v，嘗試重試 (%d/%d)", err, attempt, maxRetries)) // 至 tui.go 記錄警告
			time.Sleep(200 * time.Millisecond)
			continue
		}

		var prefillRespObj map[string]interface{}
		if err := json.Unmarshal(prefillResp, &prefillRespObj); err == nil {
			if _, hasError := prefillRespObj["error"]; hasError {
				d.app.TUI.AddLog("[WARN]", fmt.Sprintf("Prefill 階段 API 回傳錯誤: %s", string(prefillResp))) // 至 tui.go 記錄警告
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}

		// 階段 2: 發送 Decode (參考 Prefill 節點傳過來的 KV Cache)
		decodeReq := make(map[string]interface{})
		for k, v := range reqData {
			decodeReq[k] = v
		}
		decodeReq["mooncake_peer"] = prefillBackend.BootstrapAddr
		decodeReq["mooncake_engine"] = prefillBackend.EngineID
		decodeBytes, _ := json.Marshal(decodeReq)

		var decodeResp []byte
		if decodeBackend.PeerID == d.host.ID().String() {
			decodeResp, err = d.streamToLocalVLLM(r.Context(), r.URL.Path, decodeBytes) // Decode 本地執行
		} else {
			decodeResp, err = d.streamToPeer(r.Context(), decodeBackend.PeerID, r.URL.Path, decodeBytes) // Decode 遠端轉發
		}

		if err != nil {
			d.app.TUI.AddLog("[WARN]", fmt.Sprintf("Decode 階段失敗: %v，嘗試重試 (%d/%d)", err, attempt, maxRetries)) // 至 tui.go 記錄警告
			time.Sleep(200 * time.Millisecond)
			continue
		}

		d.recordMetrics(decodeResp, nil, false) // 紀錄成功指標
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(decodeResp)
		return
	}

	// 遠端 P/D 階段嘗試失敗，降級由本地 vLLM 執行
	d.app.TUI.AddLog("[WARN]", "P/D 階段嘗試失敗，降級由本地 vLLM 執行 API 請求...")
	respBytes, err := d.streamToLocalVLLM(r.Context(), r.URL.Path, rawReqBytes)
	if err == nil && len(respBytes) > 0 {
		d.recordMetrics(respBytes, nil, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBytes)
		return
	}

	d.recordMetrics(nil, fmt.Errorf("failed"), false) // 紀錄失敗指標
	writeJSONError(w, "All P/D execution attempts failed", http.StatusBadGateway)
}

// StartLocalDispatcher 建立並啟動本地的 OpenAI API 相容網關服務進入點。
// 【邏輯說明】
// 1. 建立 LocalDispatcher 實例並啟動 syncTopologyLoop() 背景同步。
// 2. 註冊 HTTP 端點 (/v1/completions, /v1/chat/completions, /mooncake_kv/, /v1/models, /health)。
// 3. 背景啟動 HTTP 伺服器，監聽 ProxyPort (預設 50006)。
func StartLocalDispatcher(app *App, h host.Host) {
	dispatcher := NewLocalDispatcher(app, h) // 建立分發器實例
	go dispatcher.syncTopologyLoop()         // 背景輪詢叢集拓樸

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/completions", dispatcher.handleProxyRequest)
	mux.HandleFunc("/v1/completions/", dispatcher.handleProxyRequest)
	mux.HandleFunc("/v1/chat/completions", dispatcher.handleProxyRequest)
	mux.HandleFunc("/v1/chat/completions/", dispatcher.handleProxyRequest)
	mux.HandleFunc("/mooncake_kv/", dispatcher.handleKVTunnel)

	// 模型清單 Handler (相容 OpenAI GET /v1/models)
	modelsHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"id": "Qwen3-4B-AWQ", "object": "model", "created": time.Now().Unix(), "owned_by": "mooncake"},
				{"id": "mooncake-default", "object": "model", "created": time.Now().Unix(), "owned_by": "mooncake"},
			},
		})
	}
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/v1/models/", modelsHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := app.Config.ProxyPort
	if port <= 0 {
		port = 50006
	}

	app.TUI.AddLog("[INFO]", fmt.Sprintf("OpenAI API Dispatcher (Server-Guided P/D) listening on :%d", port)) // 至 tui.go 記錄監聽資訊
	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(port), mux); err != nil {
			app.TUI.AddLog("[ERROR]", fmt.Sprintf("Dispatcher 啟動失敗: %v", err)) // 至 tui.go 記錄錯誤日誌
		}
	}()
}
