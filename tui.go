// Package main implements the terminal UI (tview/tcell) and stats persistence.
package main

import (
	"encoding/json" // JSON 序列化與反序列化 (持久化至 stats.json)
	"fmt"           // 格式化字串與字串拼接
	"os"            // 檔案讀寫 (os.ReadFile / os.WriteFile)
	"sort"          // 切片排序 (sort.Slice)
	"strconv"       // 數字與字串互相轉換
	"strings"       // 字串建構器 (strings.Builder) 與分割
	"sync"          // 互斥鎖 (sync.Mutex) 保護併發資料結構
	"time"          // 時間計算與定時器 (Ticker)

	"github.com/gdamore/tcell/v2" // 終端機底层 Event 與 Color 事件庫
	"github.com/rivo/tview"       // 高階 Terminal UI 視窗與組件框架
)

// Stats 儲存客戶端執行期間的即時累計統計數據。
type Stats struct {
	mu           sync.Mutex // 保護以下所有欄位讀寫的互斥鎖
	startTime    time.Time  // 程式開始運行的啟動時間點
	peers        int        // 目前已連線的 P2P 鄰居節點數量
	gpuSummary   string     // 本機 GPU 型號與數量摘要
	requests     int64      // 累計接收到的 HTTP API 請求總次數
	queue        int64      // 當前排隊等待處理的請求深度
	inTokens     int64      // 累計處理的 Prompt Token 總數量
	outTokens    int64      // 累計產生的 Completion Token 總數量
	prefill      int64      // 累計執行的 Prefill 階段請求次數
	decode       int64      // 累計執行的 Decode 階段請求次數
	errorCount   int64      // 累計處理失敗的請求次數
	successCount int64      // 累計成功處理的請求次數
}

// PersistentStats 寫入 stats.json 的持久化資料結構體。
type PersistentStats struct {
	Requests     int64 `json:"requests"`      // 累計總請求數
	SuccessCount int64 `json:"success_count"` // 累計成功數
	ErrorCount   int64 `json:"error_count"`   // 累計失敗數
	InTokens     int64 `json:"in_tokens"`     // 累計 Prompt Token 數
	OutTokens    int64 `json:"out_tokens"`    // 累計 Completion Token 數
	Prefill      int64 `json:"prefill"`       // 累計 Prefill 數
	Decode       int64 `json:"decode"`        // 累計 Decode 數
}

// loadStatsDisk 副程式：從 ./stats.json 檔案讀取歷史統計數據。
// 【邏輯說明】
// 1. 建立預設以當前時間為 startTime 的 Stats 結構體。
// 2. 嘗試讀取 ./stats.json，若檔案存在且 JSON 解碼成功，將歷史累計數填回 Stats 中。
func loadStatsDisk() *Stats {
	st := &Stats{startTime: time.Now()}
	if data, err := os.ReadFile("./stats.json"); err == nil {
		var p PersistentStats
		if json.Unmarshal(data, &p) == nil {
			st.requests = p.Requests
			st.successCount = p.SuccessCount
			st.errorCount = p.ErrorCount
			st.inTokens = p.InTokens
			st.outTokens = p.OutTokens
			st.prefill = p.Prefill
			st.decode = p.Decode
		}
	}
	return st
}

// saveStatsDisk 方法：將目前 Stats 記憶體數據寫入本機 ./stats.json 檔案。
// 避免因關機或崩潰導致統計數據丟失。
func (t *TUI) saveStatsDisk() {
	t.stats.mu.Lock()
	p := PersistentStats{
		Requests:     t.stats.requests,
		SuccessCount: t.stats.successCount,
		ErrorCount:   t.stats.errorCount,
		InTokens:     t.stats.inTokens,
		OutTokens:    t.stats.outTokens,
		Prefill:      t.stats.prefill,
		Decode:       t.stats.decode,
	}
	t.stats.mu.Unlock()
	data, _ := json.MarshalIndent(p, "", "  ")
	_ = os.WriteFile("./stats.json", data, 0644)
}

// PeerRecord 保存單一 P2P 鄰居節點的 GPUInfo 狀態與最後收到回報的時間點。
type PeerRecord struct {
	Info     GPUInfo   // 最新 GPU 狀態快照 (定義於 p2p.go)
	LastSeen time.Time // 上次收到該節點 Gossip 廣播的時間 (用於 90 秒超時離線剔除)
}

// TUI 終端文字介面主控結構體。
type TUI struct {
	app   *App   // 指向根容器 App 的指標
	stats *Stats // 累計統計物件

	logMu      sync.Mutex // 保護 logLines 的互斥鎖
	logLines   []string   // 系統通用日誌切片
	maxLogLine int        // 記憶體日誌最大留存行數 (預設 300 行)

	vllmLogMu    sync.Mutex // 保護 vllmLogLines 的互斥鎖
	vllmLogLines []string   // vLLM 控制台日誌切片

	dockerLogMu    sync.Mutex // 保護 dockerLogLines 的互斥鎖
	dockerLogLines []string   // Docker 容器日誌切片

	peerMu      sync.Mutex            // 保護 peerRecords 的互斥鎖
	peerRecords map[string]PeerRecord // P2P 鄰居動態記錄表 (PeerID -> PeerRecord)

	tviewApp      *tview.Application // tview 核心應用程式物件
	logView       *tview.TextView    // 系統日誌文字視圖
	vllmLogView   *tview.TextView    // vLLM 控制台文字視圖
	dockerLogView *tview.TextView    // Docker 容器文字視圖
	statsView     *tview.TextView    // Dashboard 左側統計面板視圖
	peersView     *tview.TextView    // Dashboard 右側鄰居列表視圖

	autoUpdate bool // 控制日誌頁面是否自動滾動至底部 (A 鍵切換)
}

// NewTUI 建構函式：初始化 TUI 實例並啟動背景 5 秒自動備份 Goroutine。
func NewTUI(app *App) *TUI {
	t := &TUI{
		app:         app,
		stats:       loadStatsDisk(), // 載入歷史統計
		maxLogLine:  300,
		peerRecords: make(map[string]PeerRecord),
		autoUpdate:  true,
	}
	// 背景 Goroutine: 每 5 秒將統計資料備份至硬碟 stats.json
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			t.saveStatsDisk()
		}
	}()
	return t
}

// AddDockerLog 寫入 Docker 容器日誌。
// 【邏輯說明】
// 1. 加鎖將日誌寫入 dockerLogLines 切片。
// 2. 超過 maxLogLine (300 行) 時自動截斷舊日誌。
// 3. 呼叫 QueueUpdateDraw 在 UI 主線程刷新畫面，並根據 autoUpdate 決定是否 ScrollToEnd()。
func (t *TUI) AddDockerLog(line string) {
	t.dockerLogMu.Lock()
	t.dockerLogLines = append(t.dockerLogLines, line)
	if len(t.dockerLogLines) > t.maxLogLine {
		t.dockerLogLines = t.dockerLogLines[len(t.dockerLogLines)-t.maxLogLine:]
	}
	t.dockerLogMu.Unlock()

	if t.dockerLogView != nil {
		t.tviewApp.QueueUpdateDraw(func() {
			t.dockerLogView.SetText(strings.Join(t.dockerLogLines, "\n"))
			if t.autoUpdate {
				t.dockerLogView.ScrollToEnd()
			}
		})
	}
}

// AddVLLMLog 寫入 vLLM 控制台日誌。
func (t *TUI) AddVLLMLog(line string) {
	t.vllmLogMu.Lock()
	t.vllmLogLines = append(t.vllmLogLines, line)
	if len(t.vllmLogLines) > t.maxLogLine {
		t.vllmLogLines = t.vllmLogLines[len(t.vllmLogLines)-t.maxLogLine:]
	}
	t.vllmLogMu.Unlock()

	if t.vllmLogView != nil {
		t.tviewApp.QueueUpdateDraw(func() {
			t.vllmLogView.SetText(strings.Join(t.vllmLogLines, "\n"))
			if t.autoUpdate {
				t.vllmLogView.ScrollToEnd()
			}
		})
	}
}

// AddLog 寫入系統通用日誌 (自動加上 [HH:MM:SS] 時間戳記與等級標頭)。
func (t *TUI) AddLog(level string, message string) {
	t.logMu.Lock()
	formatted := fmt.Sprintf("[%s] %s %s", time.Now().Format("15:04:05"), level, message)
	t.logLines = append(t.logLines, formatted)
	if len(t.logLines) > t.maxLogLine {
		t.logLines = t.logLines[len(t.logLines)-t.maxLogLine:]
	}
	t.logMu.Unlock()

	if t.logView != nil {
		t.tviewApp.QueueUpdateDraw(func() {
			t.logView.SetText(strings.Join(t.logLines, "\n"))
			if t.autoUpdate {
				t.logView.ScrollToEnd()
			}
		})
	}
}

// RecordPeerInfo 記錄收到廣播的鄰居節點狀態，並更新其 LastSeen 時間。
func (t *TUI) RecordPeerInfo(info GPUInfo) {
	t.peerMu.Lock()
	t.peerRecords[info.NodeID] = PeerRecord{
		Info:     info,
		LastSeen: time.Now(),
	}
	t.peerMu.Unlock()
}

// GetPeers 取得目前在線 (LastSeen 90 秒內) 的鄰居節點清單 Map (提供給 Web API 使用)。
func (t *TUI) GetPeers() map[string]GPUInfo {
	t.peerMu.Lock()
	defer t.peerMu.Unlock()
	res := make(map[string]GPUInfo)
	now := time.Now()
	for id, rec := range t.peerRecords {
		if now.Sub(rec.LastSeen) <= 90*time.Second {
			res[id] = rec.Info
		}
	}
	return res
}

// GetLogs 防禦性複製現有三類日誌切片的副本 (提供給 Web API 使用)。
func (t *TUI) GetLogs() ([]string, []string, []string) {
	t.logMu.Lock()
	sys := make([]string, len(t.logLines))
	copy(sys, t.logLines)
	t.logMu.Unlock()

	t.vllmLogMu.Lock()
	vllm := make([]string, len(t.vllmLogLines))
	copy(vllm, t.vllmLogLines)
	t.vllmLogMu.Unlock()

	t.dockerLogMu.Lock()
	docker := make([]string, len(t.dockerLogLines))
	copy(docker, t.dockerLogLines)
	t.dockerLogMu.Unlock()

	return sys, vllm, docker
}

// UpdateStats 在鎖保護下接受閉包修飾函式，安全地更新即時累計統計。
func (t *TUI) UpdateStats(fn func(*Stats)) {
	t.stats.mu.Lock()
	defer t.stats.mu.Unlock()
	fn(t.stats)
}

// GetLocalStats 匯出本地統計數據為 Map 格式 (提供給 Web API 或 P2P 廣播使用)。
func (t *TUI) GetLocalStats() map[string]interface{} {
	t.stats.mu.Lock()
	defer t.stats.mu.Unlock()
	return map[string]interface{}{
		"requests":       t.stats.requests,
		"success_count":  t.stats.successCount,
		"error_count":    t.stats.errorCount,
		"in_tokens":      t.stats.inTokens,
		"out_tokens":     t.stats.outTokens,
		"total_tokens":   t.stats.inTokens + t.stats.outTokens,
		"total_requests": t.stats.requests,
		"prefill":        t.stats.prefill,
		"decode":         t.stats.decode,
	}
}

// formatNum 數字格式化副程式：為整數加上千分位逗號 (例如 1234567 -> "1,234,567")。
func formatNum(n int64) string {
	in := strconv.FormatInt(n, 10)
	numOfDigits := len(in)
	if numOfDigits <= 3 {
		return in
	}
	var b strings.Builder
	for i, c := range in {
		if i > 0 && (numOfDigits-i)%3 == 0 {
			b.WriteRune(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// formatDuration 時間格式化副程式：將 time.Duration 轉換為 "HH:MM:SS" 字串。
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// buildStatsText 組合 TUI Dashboard 左側 Node Statistics 面板的格式化字串。
// 顯示 Uptime, CPU, Memory, Peers, GPU, Requests, Tokens, Prefill/Decode, Success/Error 等數據。
func (t *TUI) buildStatsText() string {
	t.stats.mu.Lock()
	defer t.stats.mu.Unlock()

	uptime := formatDuration(time.Since(t.stats.startTime)) // 格式化運行時間
	gpu := t.stats.gpuSummary
	if gpu == "" {
		gpu = "N/A"
	}

	return fmt.Sprintf(
		"[::b]Uptime[::-]      %-14s [::b]CPU[::-]        %-10s [::b]Memory[::-]     %s\n"+
			"[::b]Peer[::-]        %-14d [::b]GPU[::-]        %s\n"+
			"[::b]Requests[::-]    %-14d [::b]Queue[::-]      %d\n"+
			"[::b]In Token[::-]    %-14s [::b]Out Token[::-]  %s\n"+
			"[::b]Prefill[::-]     %-14d [::b]Decode[::-]     %d\n"+
			"[::b]Error[::-]       [red]%-14d[-] [::b]Success[::-]    [green]%d[-]",
		uptime,
		t.app.Sys.cpuPercentStr(), t.app.Sys.memUsageStr(), // 至 sys.go 查詢 CPU 與記憶體用量
		t.stats.peers, gpu,
		t.stats.requests, t.stats.queue,
		formatNum(t.stats.inTokens), formatNum(t.stats.outTokens), // 數字加千分位逗號
		t.stats.prefill, t.stats.decode,
		t.stats.errorCount, t.stats.successCount,
	)
}

// listOnlinePeers 清理超過 90 秒未回報的離線節點，並將剩餘線上鄰居依 NodeID 進行排序。
func (t *TUI) listOnlinePeers() []PeerRecord {
	t.peerMu.Lock()
	defer t.peerMu.Unlock()

	now := time.Now()
	var out []PeerRecord
	for id, rec := range t.peerRecords {
		if now.Sub(rec.LastSeen) > 90*time.Second {
			delete(t.peerRecords, id) // 剔除超時離線節點
			continue
		}
		out = append(out, rec)
	}
	// 依 PeerID 字串進行排序
	sort.Slice(out, func(i, j int) bool { return out[i].Info.NodeID < out[j].Info.NodeID })
	return out
}

// buildPeersText 組合 TUI Dashboard 右側 Connected Peers 面板的格式化表格字串。
func (t *TUI) buildPeersText() string {
	records := t.listOnlinePeers() // 取得線上鄰居記錄
	if len(records) == 0 {
		return "[::d](目前沒有收到其他節點的廣播)[::-]"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[::b]%-12s %-8s %-12s %s[::-]\n", "NodeID", "狀態", "最後回報", "GPU")
	for _, rec := range records {
		shortID := rec.Info.NodeID
		if len(shortID) > 10 {
			shortID = "..." + shortID[len(shortID)-8:]
		}
		ago := time.Since(rec.LastSeen).Round(time.Second)
		statusColor := "[green]"
		if rec.Info.Status != "idle" {
			statusColor = "[yellow]"
		}
		fmt.Fprintf(&b, "%-12s %s%-8s[-] %-12s %s\n", shortID, statusColor, rec.Info.Status, ago.String()+"前", rec.Info.Summary)
	}
	return b.String()
}

// Run 建立 tview 終端視窗、配置分頁 Layout、綁定熱鍵事件並啟動 GUI 渲染主迴圈。
// 【步驟說明】
// 1. 初始化 tview.NewApplication()。
// 2. 建立各個 TextView 與 Flex 彈性佈局容器。
// 3. 建立 4 個分頁：Dashboard (統計與鄰居), System Logs, vLLM Console, Docker Logs。
// 4. 配置頂端 Tab Bar 頁籤狀態列。
// 5. 綁定按鍵監聽 (SetInputCapture)：
//   - Q/q: 保存統計並退出程式。
//   - A/a: 切換日誌 AutoScroll 自動滾動狀態。
//   - 1-4 / Tab / Shift+Tab: 切換檢視分頁。
//
// 6. 背景開啟 1 秒 Ticker 定期更新 Dashboard 統計與鄰居畫面。
// 7. 呼叫 SetRoot 並 Run() 阻塞進入事件迴圈。
func (t *TUI) Run() error {
	tviewApp := tview.NewApplication()
	t.tviewApp = tviewApp

	// 建立統計與鄰居 View
	t.statsView = tview.NewTextView().SetDynamicColors(true)
	t.statsView.SetBorder(true).SetTitle(" Node Statistics ")

	t.peersView = tview.NewTextView().SetDynamicColors(true)
	t.peersView.SetBorder(true).SetTitle(" Connected Peers ")

	dashFlex := tview.NewFlex().
		AddItem(t.statsView, 0, 1, false).
		AddItem(t.peersView, 0, 1, false)

	// 建立三類日誌 View
	t.logView = tview.NewTextView().SetDynamicColors(true).SetRegions(true).SetScrollable(true)
	t.logView.SetBorder(true).SetTitle(" System Logs ")

	t.vllmLogView = tview.NewTextView().SetDynamicColors(true).SetRegions(true).SetScrollable(true)
	t.vllmLogView.SetBorder(true).SetTitle(" vLLM Console ")

	t.dockerLogView = tview.NewTextView().SetDynamicColors(true).SetRegions(true).SetScrollable(true)
	t.dockerLogView.SetBorder(true).SetTitle(" Docker Logs ")

	// 建立多頁頁面容器
	pages := tview.NewPages().
		AddPage("Dashboard", dashFlex, true, true).
		AddPage("System Logs", t.logView, true, false).
		AddPage("vLLM Console", t.vllmLogView, true, false).
		AddPage("Docker Logs", t.dockerLogView, true, false)

	tabs := []string{"Dashboard", "System Logs", "vLLM Console", "Docker Logs"}
	currentTab := 0

	// 建立頂端快捷頁籤條
	tabBar := tview.NewTextView().SetDynamicColors(true)
	updateTabBar := func() {
		var items []string
		for i, name := range tabs {
			if i == currentTab {
				items = append(items, fmt.Sprintf("[black:white:b] [%d] %s [-:-:-]", i+1, name))
			} else {
				items = append(items, fmt.Sprintf(" [%d] %s ", i+1, name))
			}
		}
		status := " [A] AutoScroll: ON"
		if !t.autoUpdate {
			status = " [A] AutoScroll: OFF"
		}
		tabBar.SetText(strings.Join(items, " | ") + "  " + status + "  (Press [Q] to Quit, [Tab]/[1-4] to Switch)")
	}
	updateTabBar()

	// 主版面置頂放置 Tab Bar，下方放置內容頁面
	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabBar, 1, 0, false).
		AddItem(pages, 0, 1, true)

	// 綁定全域熱鍵事件
	tviewApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'q', 'Q':
				t.saveStatsDisk() // 關機前保存統計數據
				tviewApp.Stop()
				return nil
			case 'a', 'A':
				t.autoUpdate = !t.autoUpdate // 切換自動滾動開關
				updateTabBar()
				return nil
			case '1', '2', '3', '4':
				idx := int(event.Rune() - '1')
				if idx >= 0 && idx < len(tabs) {
					currentTab = idx
					pages.SwitchToPage(tabs[currentTab])
					updateTabBar()
				}
				return nil
			}
		case tcell.KeyTab:
			currentTab = (currentTab + 1) % len(tabs)
			pages.SwitchToPage(tabs[currentTab])
			updateTabBar()
			return nil
		case tcell.KeyBacktab:
			currentTab = (currentTab - 1 + len(tabs)) % len(tabs)
			pages.SwitchToPage(tabs[currentTab])
			updateTabBar()
			return nil
		}
		return event
	})

	// 背景 1 秒 Ticker Goroutine：定時安全刷新 Dashboard 面板內容
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			tviewApp.QueueUpdateDraw(func() {
				t.statsView.SetText(t.buildStatsText()) // 刷新統計面板
				t.peersView.SetText(t.buildPeersText()) // 刷新鄰居面板
			})
		}
	}()

	if err := tviewApp.SetRoot(mainLayout, true).Run(); err != nil {
		t.AddLog("[INFO]", fmt.Sprintf("[System] TUI 介面未能初始化 (%v)，自動切換為背景無頭模式 (Headless Mode)...", err))
		fmt.Printf("[System] TUI 無法初始化 (%v)，切換為背景無頭模式 (Headless Mode)\n", err)

		fmt.Println("[System] Web 儀表板與 Gateway 控制台持續運作中。按 Ctrl+C 結束。")
		// 在無頭模式下阻塞主執行緒，避免程序退出
		select {}
	}
	return nil
}
