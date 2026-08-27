// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main implements the terminal UI (Bubble Tea/Lip Gloss) and stats persistence.
//
// This was previously built on tview/tcell. That renderer parses ANY "[...]" substring in
// a string handed to a dynamic-colors view as a color/style tag -- but the rest of this
// codebase logs plenty of genuinely literal bracketed text ("[ERROR]", "[System]", tab
// hints like "[1-4]"), which got silently swallowed or misparsed instead of displayed,
// corrupting the screen (see git history around 2026-08-27 for the reports and the two
// partial fixes that preceded this rewrite). Bubble Tea/Lip Gloss apply styling via Go
// values (lipgloss.Style), never by parsing markup out of the string content, so plain log
// text containing brackets is never at risk of being misinterpreted as a control sequence.
package main

import (
	"encoding/json" // JSON 序列化與反序列化 (持久化至 stats.json)
	"fmt"           // 格式化字串與字串拼接
	"io"            // list.ItemDelegate.Render 的輸出介面
	"os"            // 檔案讀寫 (os.ReadFile / os.WriteFile)
	"sort"          // 切片排序 (sort.Slice)
	"strconv"       // 數字與字串互相轉換
	"strings"       // 字串建構器 (strings.Builder) 與分割
	"sync"          // 互斥鎖 (sync.Mutex) 保護併發資料結構
	"time"          // 時間計算與定時器 (Ticker)

	"github.com/charmbracelet/bubbles/list"     // 主從式左側可搜尋/篩選列表元件
	"github.com/charmbracelet/bubbles/viewport" // 可捲動內容元件 (用於三個日誌分頁)
	tea "github.com/charmbracelet/bubbletea"    // 終端機事件迴圈與渲染引擎
	"github.com/charmbracelet/lipgloss"         // 樣式化文字排版
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

// TUI 終端文字介面主控結構體。這個型別本身純粹是執行緒安全的資料容器（日誌、統計、鄰居
// 記錄）；實際畫面渲染邏輯在 dashboardModel（Bubble Tea tea.Model 實作，見下方）中，兩者
// 透過 TUI 的公開方法（AddLog 等）解耦，dashboardModel 只在每次 Update/View 時讀取快照。
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
}

// NewTUI 建構函式：初始化 TUI 實例並啟動背景 5 秒自動備份 Goroutine。
func NewTUI(app *App) *TUI {
	t := &TUI{
		app:         app,
		stats:       loadStatsDisk(), // 載入歷史統計
		maxLogLine:  300,
		peerRecords: make(map[string]PeerRecord),
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

// AddDockerLog 寫入 Docker 容器日誌 (加鎖寫入切片，超過 maxLogLine 時自動截斷舊日誌)。
// 畫面刷新由 dashboardModel 的週期性 tick 自行從這個切片讀取快照，這裡不需要主動推播。
func (t *TUI) AddDockerLog(line string) {
	t.dockerLogMu.Lock()
	t.dockerLogLines = append(t.dockerLogLines, line)
	if len(t.dockerLogLines) > t.maxLogLine {
		t.dockerLogLines = t.dockerLogLines[len(t.dockerLogLines)-t.maxLogLine:]
	}
	t.dockerLogMu.Unlock()
}

// AddVLLMLog 寫入 vLLM 控制台日誌。
//
// stderr 那行行首/行尾維持沿用舊有的 "[red]"..."[-]" 慣例（呼叫端 runner.go 未變動）；
// 与舊版 tview 會把任何 "[...]" 都當成標籤解析不同，這裡只精準比對這一組固定前後綴字面
// 值，不會誤吃訊息內容裡其他無關的中括號。
func (t *TUI) AddVLLMLog(line string) {
	t.vllmLogMu.Lock()
	t.vllmLogLines = append(t.vllmLogLines, line)
	if len(t.vllmLogLines) > t.maxLogLine {
		t.vllmLogLines = t.vllmLogLines[len(t.vllmLogLines)-t.maxLogLine:]
	}
	t.vllmLogMu.Unlock()
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

// padRight 依「顯示字元數」（非位元組數）補齊字串至固定寬度；不足補空格，超過則原樣輸出
// (交由外層 lipgloss 容器截斷/換行處理，而不是在這裡硬切斷可能破壞多位元組字元)。
func padRight(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// ---------------------------------------------------------------------------
// Bubble Tea 畫面渲染 (dashboardModel)
//
// 視覺語言：極客復古 (amber-on-black) —— 純黑背景、琥珀黃 (amber) 高亮標籤/數值、復古
// 白色本文、天藍色點綴次要資訊。所有分割框一律用純 ASCII 字元 (-, |, +)，不用任何
// Unicode box-drawing 字符：這個專案在 Windows 主控台上踩過太多字元寬度/渲染的坑，純
// ASCII 是唯一保證每一種終端機都能正確對齊的畫法。
// ---------------------------------------------------------------------------

var (
	colorAmber  = lipgloss.Color("214") // 標籤、選取列、強調數值 (琥珀黃)
	colorFg     = lipgloss.Color("252") // 復古白本文
	colorSkyBlu = lipgloss.Color("75")  // 次要資訊/GPU 型號等點綴色
	colorGreen  = lipgloss.Color("42")  // 成功/idle 狀態
	colorYellow = lipgloss.Color("220") // 忙碌狀態
	colorRed    = lipgloss.Color("203") // 錯誤/stderr
	colorDim    = lipgloss.Color("240") // 次要說明文字
	colorBlack  = lipgloss.Color("0")   // 選取列文字 (疊在琥珀黃底色上)

	labelStyle    = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	fgStyle       = lipgloss.NewStyle().Foreground(colorFg)
	skyStyle      = lipgloss.NewStyle().Foreground(colorSkyBlu)
	dimStyle      = lipgloss.NewStyle().Foreground(colorDim)
	greenStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	yellowStyle   = lipgloss.NewStyle().Foreground(colorYellow)
	redStyle      = lipgloss.NewStyle().Foreground(colorRed)
	selectedStyle = lipgloss.NewStyle().Background(colorAmber).Foreground(colorBlack).Bold(true)

	// asciiBorder：純 ASCII 分割框，取代任何 Unicode 框線字元。
	asciiBorder = lipgloss.Border{
		Top: "-", Bottom: "-", Left: "|", Right: "|",
		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	}
	panelStyle = lipgloss.NewStyle().Border(asciiBorder).BorderForeground(colorAmber).Padding(0, 1)

	tabActiveStyle   = selectedStyle.Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(colorDim).Padding(0, 1)
	statusBarStyle   = lipgloss.NewStyle().Foreground(colorFg)
	hintKeyStyle     = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	hintStyle        = lipgloss.NewStyle().Foreground(colorDim)
)

const dashboardTickInterval = 1 * time.Second

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(dashboardTickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// peerItem 包裝一筆 PeerRecord 以實作 bubbles/list.Item 介面 (供左側主列表使用)。
type peerItem struct{ rec PeerRecord }

func (p peerItem) FilterValue() string {
	return p.rec.Info.NodeID + " " + p.rec.Info.Summary + " " + p.rec.Info.Status
}

// peerDelegate 是 bubbles/list 的自訂單行渲染器：取代預設的「標題+說明」雙行卡片樣式，
// 改成一行一筆的高密度表格列 (NodeID / Status / LastSeen / GPU)，符合「一屏可預覽數十
// 筆資料」的密集表格需求。
type peerDelegate struct{ width int }

func (d peerDelegate) Height() int                             { return 1 }
func (d peerDelegate) Spacing() int                             { return 0 }
func (d peerDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d peerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	pi, ok := item.(peerItem)
	if !ok {
		return
	}
	shortID := pi.rec.Info.NodeID
	if len(shortID) > 12 {
		shortID = "..." + shortID[len(shortID)-9:]
	}
	ago := time.Since(pi.rec.LastSeen).Round(time.Second)
	status := pi.rec.Info.Status
	if status == "" {
		status = "idle"
	}

	row := fmt.Sprintf("%s %s %s %s",
		padRight(shortID, 13), padRight(status, 8), padRight(ago.String()+" ago", 10), pi.rec.Info.Summary)

	if index == m.Index() {
		fmt.Fprint(w, selectedStyle.Render(padRight("> "+row, d.width)))
	} else {
		statusColor := greenStyle
		if status != "idle" {
			statusColor = yellowStyle
		}
		line := fmt.Sprintf("  %s %s %s %s",
			padRight(shortID, 13), statusColor.Render(padRight(status, 8)), padRight(ago.String()+" ago", 10), skyStyle.Render(pi.rec.Info.Summary))
		fmt.Fprint(w, line)
	}
}

// dashboardModel 是 tea.Model 實作：只持有畫面狀態（目前分頁、視窗尺寸、自動捲動開關、
// 主從式列表與三個日誌 viewport），實際資料一律即時從 *TUI 讀取快照，絕不快取成會漂移
// 的另一份副本。
type dashboardModel struct {
	t *TUI

	width, height int
	ready         bool

	tabs       []string
	currentTab int
	autoScroll bool

	peerList list.Model

	logVP    viewport.Model
	vllmVP   viewport.Model
	dockerVP viewport.Model

	quitting bool
}

func newDashboardModel(t *TUI) dashboardModel {
	l := list.New(nil, peerDelegate{}, 0, 0)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Styles.NoItems = dimStyle.Copy().Padding(1, 2)
	l.KeyMap.Quit.SetEnabled(false)         // "q" is our own global quit key, not the list's
	l.KeyMap.CursorUp.SetEnabled(true)
	l.KeyMap.CursorDown.SetEnabled(true)

	return dashboardModel{
		t:          t,
		tabs:       []string{"Peers", "System Logs", "vLLM Console", "Docker Logs"},
		autoScroll: true,
		peerList:   l,
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tickCmd()
}

// filtering 回傳左側 peer 列表目前是否正在接受搜尋文字輸入 (按 "/" 觸發)。輸入期間，全域
// 熱鍵 (q/a/1-4/tab) 必須讓路給列表自己的文字輸入處理，否則使用者連打字都打不出 "q"。
func (m dashboardModel) filtering() bool {
	return m.currentTab == 0 && m.peerList.FilterState() == list.Filtering
}

// contentHeight 回傳扣掉頂端 Tab Bar 與底部快捷鍵列 (各 1 行) 後，內容區可用的高度。
func (m dashboardModel) contentHeight() int {
	h := m.height - 2
	if h < 3 {
		return 3
	}
	return h
}

func (m *dashboardModel) syncSizes() {
	innerW := m.width - 4 // panelStyle border(2) + padding(2)
	if innerW < 10 {
		innerW = 10
	}
	innerH := m.contentHeight() - 2 // border top+bottom
	if innerH < 1 {
		innerH = 1
	}

	listW := (m.width*3)/5 - 4 // ~60% 寬度給主列表
	if listW < 10 {
		listW = 10
	}
	detailW := m.width - 4 - listW - 4
	if detailW < 10 {
		detailW = 10
	}

	if !m.ready {
		m.logVP = viewport.New(innerW, innerH)
		m.vllmVP = viewport.New(innerW, innerH)
		m.dockerVP = viewport.New(innerW, innerH)
		m.ready = true
	} else {
		m.logVP.Width, m.logVP.Height = innerW, innerH
		m.vllmVP.Width, m.vllmVP.Height = innerW, innerH
		m.dockerVP.Width, m.dockerVP.Height = innerW, innerH
	}
	m.peerList.SetSize(listW, innerH)
}

// syncPeerList 從 *TUI 重新讀取線上鄰居快照並灌入左側列表；bubbles/list 依索引保留游標
// 位置，由於 listOnlinePeers 固定依 NodeID 排序，只要該節點仍在線，索引通常穩定不跳動。
func (m *dashboardModel) syncPeerList() {
	records := m.t.listOnlinePeers()
	items := make([]list.Item, len(records))
	for i, rec := range records {
		items[i] = peerItem{rec: rec}
	}
	m.peerList.SetItems(items)
}

func (m *dashboardModel) syncLogs() {
	sys, vllm, docker := m.t.GetLogs()
	m.logVP.SetContent(strings.Join(sys, "\n"))
	m.vllmVP.SetContent(renderVLLMLog(vllm))
	m.dockerVP.SetContent(strings.Join(docker, "\n"))
	if m.autoScroll {
		m.logVP.GotoBottom()
		m.vllmVP.GotoBottom()
		m.dockerVP.GotoBottom()
	}
}

// renderVLLMLog applies the "[red]"..."[-]" stderr-highlight convention (see AddVLLMLog)
// line by line, stripping the literal markers and re-styling with lipgloss instead of
// leaving markup text for a parser to (mis)interpret.
func renderVLLMLog(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.HasPrefix(l, "[red]") && strings.HasSuffix(l, "[-]") {
			out[i] = redStyle.Render(l[len("[red]") : len(l)-len("[-]")])
		} else {
			out[i] = l
		}
	}
	return strings.Join(out, "\n")
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncSizes()
		m.syncPeerList()
		m.syncLogs()
		return m, nil

	case tickMsg:
		if m.ready {
			m.syncPeerList()
			m.syncLogs()
		}
		return m, tickCmd()

	case tea.KeyMsg:
		if !m.filtering() {
			switch msg.String() {
			case "q", "Q", "ctrl+c":
				m.t.saveStatsDisk() // 關機前保存統計數據
				m.quitting = true
				return m, tea.Quit
			case "a", "A":
				m.autoScroll = !m.autoScroll
				return m, nil
			case "r", "R":
				m.syncPeerList()
				m.syncLogs()
				return m, nil
			case "1", "2", "3", "4":
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(m.tabs) {
					m.currentTab = idx
				}
				return m, nil
			case "tab":
				m.currentTab = (m.currentTab + 1) % len(m.tabs)
				return m, nil
			case "shift+tab":
				m.currentTab = (m.currentTab - 1 + len(m.tabs)) % len(m.tabs)
				return m, nil
			}
		}
	}

	// 未被上面全域熱鍵吃掉的按鍵（搜尋輸入中的任意字元、列表游標上下、日誌捲動…）轉發給
	// 目前分頁自己的元件處理。
	var cmd tea.Cmd
	switch m.currentTab {
	case 0:
		m.peerList, cmd = m.peerList.Update(msg)
	case 1:
		m.logVP, cmd = m.logVP.Update(msg)
	case 2:
		m.vllmVP, cmd = m.vllmVP.Update(msg)
	case 3:
		m.dockerVP, cmd = m.dockerVP.Update(msg)
	}
	return m, cmd
}

func (m dashboardModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Initializing..."
	}

	var b strings.Builder
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	switch m.currentTab {
	case 0:
		b.WriteString(m.renderPeersMasterDetail())
	case 1:
		b.WriteString(panelStyle.Width(m.width - 2).Height(m.contentHeight() - 2).MaxHeight(m.contentHeight() - 2).Render(m.logVP.View()))
	case 2:
		b.WriteString(panelStyle.Width(m.width - 2).Height(m.contentHeight() - 2).MaxHeight(m.contentHeight() - 2).Render(m.vllmVP.View()))
	case 3:
		b.WriteString(panelStyle.Width(m.width - 2).Height(m.contentHeight() - 2).MaxHeight(m.contentHeight() - 2).Render(m.dockerVP.View()))
	}

	b.WriteString("\n")
	b.WriteString(m.renderHintBar())
	return b.String()
}

// renderStatusBar 頂端狀態錨點：分頁籤 + 目前上下文摘要 (角色/在線節點數/運行時間/請求數)。
func (m dashboardModel) renderStatusBar() string {
	var tabs []string
	for i, name := range m.tabs {
		label := fmt.Sprintf(" %d:%s ", i+1, name)
		if i == m.currentTab {
			tabs = append(tabs, tabActiveStyle.Render(label))
		} else {
			tabs = append(tabs, tabInactiveStyle.Render(label))
		}
	}

	t := m.t
	t.stats.mu.Lock()
	uptime := formatDuration(time.Since(t.stats.startTime))
	peers, requests, successCount, errCount := t.stats.peers, t.stats.requests, t.stats.successCount, t.stats.errorCount
	t.stats.mu.Unlock()

	role := "inference"
	if t.app.Config.ServerMode.RelayOnly {
		role = "relay-only"
	}

	ctx := fmt.Sprintf(" %s | %s peers | up %s | req %s (ok %s / err %s)",
		skyStyle.Render(role),
		hintKeyStyle.Render(strconv.Itoa(peers)),
		fgStyle.Render(uptime),
		fgStyle.Render(strconv.FormatInt(requests, 10)),
		greenStyle.Render(strconv.FormatInt(successCount, 10)),
		redStyle.Render(strconv.FormatInt(errCount, 10)),
	)

	return strings.Join(tabs, "") + statusBarStyle.Render(ctx)
}

// renderHintBar 底部固定快捷鍵導覽列，全鍵盤操作，不需要滑鼠。
func (m dashboardModel) renderHintBar() string {
	key := func(k, desc string) string { return hintKeyStyle.Render(k) + hintStyle.Render(" "+desc) }

	if m.currentTab == 0 {
		return "  " + strings.Join([]string{
			key("/", "search"), key("up/down", "select"), key("a", fmt.Sprintf("autoscroll:%s", onOff(m.autoScroll))),
			key("r", "refresh"), key("1-4", "switch"), key("q", "quit"),
		}, "   ")
	}
	return "  " + strings.Join([]string{
		key("up/down/pgup/pgdn", "scroll"), key("a", fmt.Sprintf("autoscroll:%s", onOff(m.autoScroll))),
		key("1-4", "switch"), key("q", "quit"),
	}, "   ")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// renderPeersMasterDetail 組合左側 (~60%) 高密度 peer 列表與右側 (~40%) 選中節點的完整
// 元資料詳情面板 (master-detail 佈局)。
func (m dashboardModel) renderPeersMasterDetail() string {
	listW := (m.width*3)/5 - 4
	if listW < 10 {
		listW = 10
	}
	detailW := m.width - 4 - listW - 4
	if detailW < 10 {
		detailW = 10
	}
	innerH := m.contentHeight() - 2

	listBox := panelStyle.Width(listW).Height(innerH).MaxHeight(innerH).Render(m.peerList.View())
	// buildPeerDetail() renders a fixed ~20-line block regardless of the actual terminal
	// height. lipgloss's Height() only pads content that's SHORTER than requested; it does
	// not clip content that's taller, so on a short terminal this panel silently overflowed
	// the frame -- the whole rendered string ended up taller than the real window, and the
	// terminal auto-scrolled to show the bottom, clipping the status bar off the top (only
	// visible on the Peers tab, since the log tabs' bubbles/viewport panels genuinely
	// truncate to their height already). MaxHeight makes this panel clip the same way.
	detailBox := panelStyle.Width(detailW).Height(innerH).MaxHeight(innerH).Render(m.buildPeerDetail())
	return lipgloss.JoinHorizontal(lipgloss.Top, listBox, detailBox)
}

// buildPeerDetail 渲染目前於左側列表選取節點的完整 GPUInfo 元資料；若尚無任何節點在線，
// 改顯示本機的 Node Statistics 摘要，讓右側面板永遠有內容可看。
func (m dashboardModel) buildPeerDetail() string {
	item, ok := m.peerList.SelectedItem().(peerItem)
	if !ok {
		return m.buildLocalStatsText()
	}
	info := item.rec.Info
	ago := time.Since(item.rec.LastSeen).Round(time.Second)

	field := func(label, val string) string {
		return fmt.Sprintf("%s%s", labelStyle.Render(padRight(label, 12)), val)
	}
	role := info.Role
	if role == "" {
		role = "inference"
	}

	lines := []string{
		labelStyle.Render("Node Detail"),
		"",
		field("NodeID", skyStyle.Render(info.NodeID)),
		field("Role", role),
		field("Status", info.Status),
		field("LastSeen", ago.String()+" ago"),
		field("GPU", info.Summary),
		field("Addr", dimStyle.Render(info.Addr)),
		"",
		field("GPU Temp", fmt.Sprintf("%d C", info.GPUTemp)),
		field("GPU Util", fmt.Sprintf("%d%%", info.GPUUtil)),
		field("Mem Util", fmt.Sprintf("%d%%", info.MemUtil)),
		field("VRAM", fmt.Sprintf("%d / %d MB", info.VRAMUsed, info.VRAMTotal)),
		field("Power", fmt.Sprintf("%.1f / %.1f W", info.PowerDraw, info.PowerLimit)),
		field("Fan", fmt.Sprintf("%d%%", info.FanSpeed)),
		field("Driver", info.DriverVersion),
		"",
		field("KV Cache", fmt.Sprintf("%.1f%%", info.KVCacheUsage*100)),
		field("Active Req", strconv.Itoa(info.ActiveRequests)),
		field("Gen Speed", fmt.Sprintf("%.1f t/s", info.GenSpeed)),
		field("Prefill Spd", fmt.Sprintf("%.1f t/s", info.PrefillSpeed)),
		field("Avg TTFT", fmt.Sprintf("%.2f s", info.AvgTTFT)),
		field("Total Req", strconv.FormatInt(info.TotalRequests, 10)),
		field("Tokens", fmt.Sprintf("%s in / %s out", formatNum(info.InTokens), formatNum(info.OutTokens))),
	}
	return strings.Join(lines, "\n")
}

// buildLocalStatsText 在還沒有任何遠端節點上線時，右側詳情面板改顯示本機統計，避免空白。
func (m dashboardModel) buildLocalStatsText() string {
	t := m.t
	t.stats.mu.Lock()
	uptime := formatDuration(time.Since(t.stats.startTime))
	gpu := t.stats.gpuSummary
	if gpu == "" {
		gpu = "N/A"
	}
	requests, queue := t.stats.requests, t.stats.queue
	inTok, outTok := t.stats.inTokens, t.stats.outTokens
	prefill, decode := t.stats.prefill, t.stats.decode
	t.stats.mu.Unlock()

	field := func(label, val string) string {
		return fmt.Sprintf("%s%s", labelStyle.Render(padRight(label, 12)), val)
	}
	lines := []string{
		labelStyle.Render("This Node"),
		dimStyle.Render("(no peer selected -- showing local stats)"),
		"",
		field("Uptime", uptime),
		field("CPU", t.app.Sys.cpuPercentStr()),
		field("Memory", t.app.Sys.memUsageStr()),
		field("GPU", gpu),
		field("Requests", strconv.FormatInt(requests, 10)),
		field("Queue", strconv.FormatInt(queue, 10)),
		field("In Token", formatNum(inTok)),
		field("Out Token", formatNum(outTok)),
		field("Prefill", strconv.FormatInt(prefill, 10)),
		field("Decode", strconv.FormatInt(decode, 10)),
	}
	return strings.Join(lines, "\n")
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
	sort.Slice(out, func(i, j int) bool { return out[i].Info.NodeID < out[j].Info.NodeID })
	return out
}

// Run 建立 Bubble Tea 全螢幕應用程式、啟動事件迴圈並阻塞直到使用者按 Q 離開或發生錯誤。
// 熱鍵：Q 離開、A 切換自動捲動、R 立即刷新、1-4/Tab/Shift+Tab 切換分頁、"/" 在 Peers 分頁
// 搜尋 (bubbles/list 內建)；捲動鍵在日誌分頁時交給該分頁自己的 viewport 處理。
func (t *TUI) Run() error {
	p := tea.NewProgram(newDashboardModel(t), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		t.AddLog("[INFO]", fmt.Sprintf("[System] TUI 介面未能初始化 (%v)，自動切換為背景無頭模式 (Headless Mode)...", err))
		fmt.Printf("[System] TUI 無法初始化 (%v)，切換為背景無頭模式 (Headless Mode)\n", err)
		fmt.Println("[System] Web 儀表板與 Gateway 控制台持續運作中。按 Ctrl+C 結束。")
		select {} // 在無頭模式下阻塞主執行緒，避免程序退出
	}
	return nil
}
