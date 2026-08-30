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
	// A relay-only node has no local vLLM to wait for, so polling would log a failure
	// every few seconds forever. Leaving vllmReady false is exactly right: the dispatcher
	// then always routes to peers that do have GPUs.
	if !app.Config.ServerMode.RelayOnly {
		go d.startVLLMHealthChecker() // 背景輪詢等待本機 vLLM 就緒
	}
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
// 透過 libp2p 向 Bootstrap/Hub 節點索取最新的 P/D 叢集拓樸視圖。
//
// 走 HubAPIProtocolID 而不是原始 HTTP，所以 Hub 不需要對外開放任何埠，也能穿透 NAT 與 relay；
// 而且 swarm.key 的 PSK 本身就限制了誰能開起這條串流，等於免費附帶身分驗證。
//
// 每個失敗分支都必須留下日誌：這裡原本全部是裸 return，於是「拓樸永遠同步不到」會完全無聲——
// d.topology 一直停在建構時的 PD-Together 預設值，所有請求退回本機優先派工，表面上看起來只是
// 「P/D 沒有生效」而查不到任何線索。實測曾因此讓整個叢集的 P/D 分離長期失效而無人察覺。
func (d *LocalDispatcher) syncTopologyLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	syncOnce := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if d.app.P2P == nil {
			return
		}
		body, err := d.app.P2P.FetchHubAPI(ctx, "/hub/api/cluster_topology")
		if err != nil {
			d.app.TUI.AddLog("[SYNC]", fmt.Sprintf("無法取得 Hub 拓樸: %v — P/D 分離不會生效（PD-Together 模式不受影響）", err))
			return
		}

		var top ClusterTopologyResponse
		if err := json.Unmarshal(body, &top); err != nil {
			d.app.TUI.AddLog("[SYNC]", fmt.Sprintf("拓樸回應解析失敗: %v", err))
			return
		}
		d.mu.Lock()
		d.topology = top
		d.mu.Unlock()
		d.app.TUI.AddLog("[SYNC]", fmt.Sprintf("同步 Server P/D 拓樸成功 (IsPDTogether: %v, P: %d, D: %d)", top.IsPDTogether, len(top.PrefillBackends), len(top.DecodeBackends))) // 至 tui.go 記錄同步日誌
	}

	// 靜態呼叫首次同步，隨後定時器輪詢
	syncOnce()
	for range ticker.C {
		syncOnce()
	}
}

// peerVLLMPort 回傳目標節點自己廣播的 vLLM 埠。過去這裡一律誤用「本機自己」的
// config.VLLM.Port 當成隧道目標埠，兩者只是碰巧預設值都是 8100 才長期沒被發現——
// 一旦某節點的 vllm.port 被改成非預設值（例如 relay-only 節點為了避免埠衝突而
// 自訂），接收端 setupStreams 的白名單檢查（只允許自己本機設定的埠）就會直接拒絕，
// 回傳 403 "target port is not allowed"，導致該節點永遠無法把請求轉發給任何一個
// vllm.port 與自己不同的對象。改為查詢該節點透過 GossipSub 廣播的 VLLMPort；
// 尚未升級到含此欄位版本的舊節點回傳 0，退回官方文件慣用的預設埠 8100。
func (d *LocalDispatcher) peerVLLMPort(peerIDStr string) int {
	if info, ok := d.app.TUI.GetPeers()[peerIDStr]; ok && info.VLLMPort > 0 {
		return info.VLLMPort
	}
	return 8100
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

	vllmPort := d.peerVLLMPort(peerIDStr)

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

// streamToPeerDirect 與 streamToPeer 走相同的 libp2p tunnel，但不把回應整包緩衝
// 成 []byte 再重新包裝成 application/json —— 那樣做會讓串流請求 (stream: true) 收到
// 的 SSE `data: {...}\n\n` 內容被誤標成 Content-Type: application/json，導致嚴格檢查
// Content-Type 的 SSE 客戶端 (例如 aiperf) 解析失敗。這裡改為原封不動複製遠端節點的
// Response Headers（包含真正的 text/event-stream）並即時 pipe body，做法對齊本機直通
// 的 proxyToLocalVLLMDirect。僅用於 PD-Together 模式的 P2P 備援路徑；P/D 分離模式的
// Prefill/Decode 階段仍需要讀取完整回應做判斷，繼續使用會緩衝的 streamToPeer。
func (d *LocalDispatcher) streamToPeerDirect(ctx context.Context, w http.ResponseWriter, peerIDStr string, path string, reqBytes []byte) bool {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. peer.Decode 失敗: %v", peerIDStr[:8], err))
		return false
	}

	stream, err := d.host.NewStream(ctx, pid, ProxyProtocolID)
	if err != nil {
		addrs := d.host.Peerstore().Addrs(pid)
		addrStrs := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrs[i] = a.String()
		}
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. NewStream 失敗 (已知位址: %v): %+v", peerIDStr[:8], addrStrs, err))
		return false
	}
	defer stream.Close()

	vllmPort := d.peerVLLMPort(peerIDStr)

	if err := binary.Write(stream, binary.BigEndian, uint16(vllmPort)); err != nil {
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. 寫入目標埠失敗: %v", peerIDStr[:8], err))
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(reqBytes))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = fmt.Sprintf("127.0.0.1:%d", vllmPort)

	if err := req.Write(stream); err != nil {
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. 寫入請求失敗: %v", peerIDStr[:8], err))
		return false
	}

	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. 讀取回應失敗: %v", peerIDStr[:8], err))
		return false
	}
	defer resp.Body.Close()

	// 遠端節點回 5xx 代表「這台不能服務」（最常見的是它自己的 vLLM 掛了，proxy handler
	// 回 502 Failed to reach local port 8100），而不是這個請求本身有問題。此時必須當成
	// 這次嘗試失敗、讓外層換下一個節點重試——絕不能把它 pipe 給客戶端。
	//
	// 少了這道判斷，「傳輸成功」就被當成「請求成功」：一台 vLLM 掛掉但 client 還活著的
	// 節點會持續留在 round-robin 名單中，穩定污染約 1/N 的請求，而既有的重試機制完全使
	// 不上力，因為 libp2p 這層從頭到尾都是成功的。實測（6 個推理節點、其中 1 台 vLLM
	// 掛掉）錯誤率 55/300 = 18.3%，恰好接近 1/6。
	//
	// 只對 5xx 換節點。4xx 是請求本身的問題（例如模型名稱錯誤），換哪台都一樣會失敗，
	// 原樣回給客戶端才是正確的。
	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		d.app.TUI.AddLog("[WARN]", fmt.Sprintf("P2P 遠端節點 %s.. 回傳 %d，改試其他節點: %s",
			peerIDStr[:8], resp.StatusCode, strings.TrimSpace(string(body))))
		return false
	}

	// 原封不動地複製所有 Response Headers (包含 Content-Type: text/event-stream 等)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)

	// 透明 Pipe：直接將遠端節點的回應串流 pipe 給客戶端，零緩衝
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}

	d.app.TUI.UpdateStats(func(st *Stats) {
		st.requests++
		st.decode++
		st.successCount++
	})
	return true
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
		modelName = "yuanyi-default"
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

		// 先算出目前可用的 P2P 遠端節點清單：下面「本機忙碌時該不該分流出去」需要先知道
		// 到底有沒有地方可以分流。
		knownPeers := d.app.TUI.GetPeers()
		var peerIDs []string
		for _, p := range knownPeers {
			if p.NodeID == "" || p.NodeID == d.host.ID().String() {
				continue
			}
			// Relay-only peers contribute network capacity, not GPU capacity -- they run
			// no vLLM, so dispatching inference to them would always fail. Any other value
			// (including empty, sent by older builds) means the peer does serve inference.
			// Also check Summary: it's been a required (non-omitempty) field since before
			// Role existed, so a peer running a pre-Role build that never sends "role":
			// "relay" at all still gets caught here by its "No GPU Detected" summary --
			// without this, every receiver (regardless of its own build freshness) treats
			// that peer's empty Role as "usable" and repeatedly dispatches to it, always
			// failing (observed as NewStream failures / dial backoff loops in the field).
			if p.Role == RoleRelay || p.Summary == "No GPU Detected" {
				continue
			}
			peerIDs = append(peerIDs, p.NodeID)
		}

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
			if len(peerIDs) == 0 {
				// 單機模式（Swarm 裡目前沒有其他節點，例如 Windows 本機單獨執行）：
				// 本機忙碌但沒有任何遠端節點可以分流。此時在本機排隊處理，遠比直接回
				// 502 好——vLLM 自己就會把併發請求做 continuous batching，排隊是有效的。
				d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("本地 vLLM 忙碌中 (模型: %s)，但目前無可用 P2P 遠端節點，改為本機排隊處理...", modelName))
				if d.proxyToLocalVLLMDirect(w, r, reqBytes) {
					return // 成功：本機排隊處理完成
				}
				d.app.TUI.AddLog("[WARN]", "本機排隊處理失敗，將嘗試 P2P 備援...")
			} else {
				d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("本地 vLLM 忙碌中 (模型: %s)，分派至 P2P 遠端節點...", modelName))
			}
		}

		// 重試次數必須至少覆蓋「每個已知節點各試一次」。原本寫死 3 次：當 Swarm 裡的節點數
		// 多於 3，而 round-robin 前幾次剛好選到當下不可達的節點時，就會在還有其他健康節點
		// 沒被嘗試過的情況下直接放棄並回 502。以節點數為下限，確保輪完一圈才判定失敗。
		peerAttempts := maxRetries
		if len(peerIDs) > peerAttempts {
			peerAttempts = len(peerIDs)
		}

		for attempt := 1; attempt <= peerAttempts; attempt++ {
			if len(peerIDs) > 0 {
				d.mu.Lock()
				d.decodeIndex = (d.decodeIndex + 1) % len(peerIDs)
				targetPeerID := peerIDs[d.decodeIndex]
				d.mu.Unlock()

				// 注意：這裡刻意不再預先檢查 d.host.Peerstore().Addrs(pid)。透過 GossipSub
				// 得知（而非直接 libp2p 連線）的節點，其位址不會被寫進 Peerstore，用它來
				// 篩選會把所有「只透過中繼/gossip 認識」的跨 NAT 節點都誤判成不可達，
				// 導致遠端派發永遠選不到任何目標。可達性交給 NewStream 自己判斷即可——
				// 它本來就會透過 Kademlia DHT／Circuit Relay／打洞去找路徑，找不到才是
				// 真正的失敗，由下面 streamToPeerDirect 的回傳值與重試機制處理。
				d.app.TUI.AddLog("[PROXY]", fmt.Sprintf("P2P 備援轉發 %s -> 遠端節點: %s (嘗試 %d/%d)", modelName, targetPeerID[:8], attempt, peerAttempts))
				if d.streamToPeerDirect(r.Context(), w, targetPeerID, r.URL.Path, reqBytes) {
					return // 成功：已直接 pipe 遠端節點的回應給客戶端
				}
				time.Sleep(200 * time.Millisecond)
			}
		}

		// 步驟 3: 所有遠端節點都失敗時的最終保險——只要本機 vLLM 還就緒，就排隊由本機處理。
		// 走到這裡通常是「本機忙碌 → 嘗試分流 → 遠端全部不可達」，但本機忙碌不代表本機不能
		// 處理：vLLM 自己會做 continuous batching，多等一下遠比直接回 502 好。少了這層，
		// 一旦遠端暫時全部連不上，明明本機有能力服務的請求也會失敗。
		if d.vllmReady.Load() {
			d.app.TUI.AddLog("[WARN]", fmt.Sprintf("所有 %d 個遠端節點皆不可達，改由本機排隊處理...", peerAttempts))
			if d.proxyToLocalVLLMDirect(w, r, reqBytes) {
				return // 成功：本機排隊處理完成
			}
		}

		// 步驟 4: 本地與遠端備援皆失敗，紀錄失敗指標並回傳 HTTP 502
		proxyErr := fmt.Errorf("local vLLM unavailable and all %d remote peer attempts failed", peerAttempts)
		d.recordMetrics(nil, proxyErr, false)
		writeJSONError(w, fmt.Sprintf("Proxy error: %v", proxyErr), http.StatusBadGateway)
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
		decodeIsLocal := decodeBackend.PeerID == d.host.ID().String()
		if decodeIsLocal {
			decodeResp, err = d.streamToLocalVLLM(r.Context(), r.URL.Path, decodeBytes) // Decode 本地執行
		} else {
			decodeResp, err = d.streamToPeer(r.Context(), decodeBackend.PeerID, r.URL.Path, decodeBytes) // Decode 遠端轉發
		}

		if err != nil {
			d.app.TUI.AddLog("[WARN]", fmt.Sprintf("Decode 階段失敗: %v，嘗試重試 (%d/%d)", err, attempt, maxRetries)) // 至 tui.go 記錄警告
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Only count it when this node's own vLLM produced the answer. When decode ran on a
		// peer, that peer already counts it in its ProxyProtocolID handler (see p2p.go), so
		// recording it here too would credit one request to two nodes and inflate exactly the
		// dispatcher nodes that did the least work.
		if decodeIsLocal {
			d.recordMetrics(decodeResp, nil, false) // 紀錄成功指標
		}
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
				{"id": "Qwen/Qwen3-4B-AWQ", "object": "model", "created": time.Now().Unix(), "owned_by": "yuanyi"},
				{"id": "yuanyi-default", "object": "model", "created": time.Now().Unix(), "owned_by": "yuanyi"},
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
