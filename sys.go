// Package main implements hardware metric scraping and NVML GPU telemetry.
package main

import (
	"fmt"            // 格式化字串與數字輸出
	"io"             // 基礎流讀取 (io.ReadAll)
	"net/http"       // HTTP Get 請求 (發送給 vLLM /metrics 端點)
	"os"             // 作業系統程序 PID 查詢 (os.Getpid)
	"os/exec"        // 執行 nvidia-smi 外部系統命令
	"strconv"        // 字串轉浮點數與整數
	"strings"        // 字串切割與前綴匹配
	"sync"           // 互斥鎖 (sync.Mutex) 保護併發存取
	"time"           // 時間計算與定時器 (Ticker)

	"github.com/shirou/gopsutil/v3/process" // 系統程序資源監控庫 (查詢 CPU 與 RSS 記憶體)
)

// VLLMMetrics 儲存從 vLLM HTTP /metrics 端點解析與計算後的推論效能快照。
type VLLMMetrics struct {
	KVCacheUsage   float64 `json:"kv_cache_usage"`   // GPU 顯示記憶體 KV Cache 當前使用率百分比 (0~100%)
	ActiveRequests int     `json:"active_requests"`  // 當前處理中與排隊中的請求數量
	PrefillSpeed   float64 `json:"prefill_speed"`    // Prefill 階段即時吞吐速率 (tokens/sec)
	GenSpeed       float64 `json:"gen_speed"`        // Generation 階段解碼即時生成速率 (tokens/sec)
	AvgTTFT        float64 `json:"avg_ttft"`         // 平均首字產生延遲 Time-To-First-Token (秒)
}

// SysMonitor 系統資源 (CPU/記憶體) 與 GPU/vLLM 指標監控組件。
type SysMonitor struct {
	app        *App             // 指向根容器 App 的指標
	procHandle *process.Process // gopsutil 程序 Handle (用於計算本程式的 CPU 與記憶體)

	metricsMu        sync.Mutex  // 保護以下增量計算與 metrics 欄位的互斥鎖
	lastMetricsTime  time.Time   // 上一次成功爬取 Prometheus 指標的時間
	lastPromptTokens float64     // 上一次爬取的 Prompt Tokens 累計值
	lastGenTokens    float64     // 上一次爬取的 Generation Tokens 累計值
	lastTTFTSum      float64     // 上一次爬取的 TTFT 總延遲時間和
	lastTTFTCount    float64     // 上一次爬取的 TTFT 總次數
	currentMetrics   VLLMMetrics // 最新計算好的效能指標快照
}

// NewSysMonitor 建構函式：建立 SysMonitor 實例。
func NewSysMonitor(app *App) *SysMonitor {
	return &SysMonitor{app: app}
}

// Start 啟動監控組件。
// 【邏輯說明】
// 1. 呼叫 process.NewProcess 傳入本程序 PID (os.Getpid) 取得 gopsutil 程序物件。
// 2. 預熱呼叫 procHandle.Percent(0) 初始化 CPU 基準線。
// 3. 背景啟動 s.metricScraper() 爬蟲 Goroutine。
func (s *SysMonitor) Start() {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err == nil {
		s.procHandle = p
		_, _ = s.procHandle.Percent(0)
	}
	go s.metricScraper() // 背景執行 metrics 爬取
}

// GetMetrics 安全取得最新的 VLLMMetrics 結構體副本 (執行緒安全)。
func (s *SysMonitor) GetMetrics() VLLMMetrics {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return s.currentMetrics
}

// metricScraper 永續監控 Goroutine：
// 每 2 秒透過 HTTP GET 爬取 http://localhost:8100/metrics (vLLM Prometheus 端點)。
// 【計算邏輯說明】
// 1. 解析以 `#` 開頭以外的行，提取 vllm:gpu_cache_usage_perc, vllm:num_requests_running 等 key/value。
// 2. 增量速率計算：
//    - dt = 當前時間 - 上次時間 (秒)
//    - PrefillSpeed = (當前 Prompt Tokens - 上次 Prompt Tokens) / dt
//    - GenSpeed = (當前 Gen Tokens - 上次 Gen Tokens) / dt
//    - AvgTTFT = (當前 TTFT Sum - 上次 TTFT Sum) / (當前 TTFT Count - 上次 TTFT Count)
// 3. 將累計 Token 數同步給 tui.go 的 Stats 結構。
func (s *SysMonitor) metricScraper() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		resp, err := http.Get("http://localhost:8100/metrics")
		if err != nil {
			continue // 若 vLLM 尚未啟動或端點連不上，無視錯誤等待下次輪詢
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		lines := strings.Split(string(body), "\n")
		var kvCache, requests, promptTokens, genTokens, ttftSum, ttftCount float64

		// 解析 Prometheus 多行文字格式
		for _, line := range lines {
			if strings.HasPrefix(line, "#") {
				continue // 忽略 Prometheus 註解行
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				continue
			}

			keyPart := parts[0]
			valStr := strings.TrimSpace(parts[1])
			val, _ := strconv.ParseFloat(valStr, 64)

			// 匹配各項指標鍵名
			if strings.HasPrefix(keyPart, "vllm:gpu_cache_usage_perc") {
				if val > kvCache {
					kvCache = val // 取多卡中最高的 KV Cache 使用率
				}
			} else if strings.HasPrefix(keyPart, "vllm:num_requests_running") {
				requests += val
			} else if strings.HasPrefix(keyPart, "vllm:request_prompt_tokens_total") || strings.HasPrefix(keyPart, "vllm:prompt_tokens_total") || strings.HasPrefix(keyPart, "vllm:num_prompt_tokens") {
				promptTokens += val
			} else if strings.HasPrefix(keyPart, "vllm:request_generation_tokens_total") || strings.HasPrefix(keyPart, "vllm:generation_tokens_total") || strings.HasPrefix(keyPart, "vllm:num_generation_tokens") {
				genTokens += val
			} else if strings.HasPrefix(keyPart, "vllm:time_to_first_token_seconds_sum") {
				ttftSum += val
			} else if strings.HasPrefix(keyPart, "vllm:time_to_first_token_seconds_count") {
				ttftCount += val
			}
		}

		// 增量計算與鎖保護
		s.metricsMu.Lock()
		now := time.Now()
		if !s.lastMetricsTime.IsZero() {
			dt := now.Sub(s.lastMetricsTime).Seconds()
			if dt > 0 {
				// 計算 Prefill 速率 (Tokens/s)
				s.currentMetrics.PrefillSpeed = (promptTokens - s.lastPromptTokens) / dt
				if s.currentMetrics.PrefillSpeed < 0 {
					s.currentMetrics.PrefillSpeed = 0
				}
				// 計算 Generation 速率 (Tokens/s)
				s.currentMetrics.GenSpeed = (genTokens - s.lastGenTokens) / dt
				if s.currentMetrics.GenSpeed < 0 {
					s.currentMetrics.GenSpeed = 0
				}

				// 計算平均首字延遲 (TTFT)
				diffSum := ttftSum - s.lastTTFTSum
				diffCount := ttftCount - s.lastTTFTCount
				if diffCount > 0 {
					s.currentMetrics.AvgTTFT = diffSum / diffCount
				} else {
					s.currentMetrics.AvgTTFT = 0
				}
			}
		}

		// 保存本次歷史基準值
		s.lastPromptTokens = promptTokens
		s.lastGenTokens = genTokens
		s.lastTTFTSum = ttftSum
		s.lastTTFTCount = ttftCount
		s.lastMetricsTime = now

		s.currentMetrics.KVCacheUsage = kvCache * 100 // 轉為百分比
		s.currentMetrics.ActiveRequests = int(requests)
		s.metricsMu.Unlock()

		// 同步最新 Tokens 至 TUI 控制台
		if s.app != nil && s.app.TUI != nil {
			s.app.TUI.UpdateStats(func(st *Stats) { // 至 tui.go 同步 Token 數據
				if int64(promptTokens) > st.inTokens {
					st.inTokens = int64(promptTokens)
				}
				if int64(genTokens) > st.outTokens {
					st.outTokens = int64(genTokens)
				}
			})
		}
	}
}

// cpuPercentStr 回傳目前程序的 CPU 使用率字串 (格式如 "12.5%")。
func (s *SysMonitor) cpuPercentStr() string {
	if s.procHandle == nil {
		return "N/A"
	}
	pct, err := s.procHandle.Percent(0)
	if err != nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", pct)
}

// memUsageStr 回傳目前程序的記憶體 RSS 用量字串 (格式如 "256.0MB" 或 "1.2GB")。
func (s *SysMonitor) memUsageStr() string {
	if s.procHandle == nil {
		return "N/A"
	}
	mi, err := s.procHandle.MemoryInfo()
	if err != nil || mi == nil {
		return "N/A"
	}
	gb := float64(mi.RSS) / (1024 * 1024 * 1024)
	if gb < 1 {
		mb := float64(mi.RSS) / (1024 * 1024)
		return fmt.Sprintf("%.1fMB", mb)
	}
	return fmt.Sprintf("%.1fGB", gb)
}

// GetGPUModelSummary 回傳本機 GPU 型號與數量摘要字串切片 (例如 ["NVIDIA RTX 4090(24576MB) x1"])。
func (s *SysMonitor) GetGPUModelSummary() []string {
	tele := s.GetGPUTelemetry() // 獲取 GPU 遙測資料
	if tele.Summary != "" {
		return []string{tele.Summary}
	}
	return []string{"無法讀取GPU"}
}

// GPUTelemetry 儲存透過 nvidia-smi 查詢到的顯卡硬體遙測資料。
type GPUTelemetry struct {
	Summary       string  `json:"summary"`        // GPU 型號與數量摘要 (例如 "NVIDIA RTX 4090 x1")
	GPUTemp        int     `json:"gpu_temp"`       // 多張 GPU 中的最高核心溫度 (℃)
	GPUUtil        int     `json:"gpu_util"`       // 多張 GPU 中的最高計算單元使用率 (%)
	MemUtil        int     `json:"mem_util"`       // 多張 GPU 中的最高記憶體控制器使用率 (%)
	VRAMUsed       int     `json:"vram_used"`      // 所有 GPU 已使用顯示記憶體總和 (MB)
	VRAMTotal      int     `json:"vram_total"`     // 所有 GPU 總顯示記憶體容量 (MB)
	PowerDraw      float64 `json:"power_draw"`     // 多張 GPU 中的最高即時功耗 (W)
	PowerLimit     float64 `json:"power_limit"`    // GPU 功耗上限設定值 (W)
	FanSpeed       int     `json:"fan_speed"`      // 多張 GPU 中的最高風扇轉速百分比 (%)
	DriverVersion  string  `json:"driver_version"` // NVIDIA 顯示卡驅動程式版本號
}

// GetGPUTelemetry 呼叫外部命令 `nvidia-smi` 查詢本機 GPU 狀態並解析 CSV。
// 【解析邏輯說明】
// 1. 執行 `nvidia-smi --query-gpu=... --format=csv,noheader,nounits`。
// 2. 若無 NVIDIA 顯卡或命令失敗，防禦性回傳 "No GPU Detected" 結構體。
// 3. 逐行解析 CSV 10 個欄位：name, memory.total, memory.used, utilization.gpu, utilization.memory, temperature.gpu, power.draw, power.limit, fan.speed, driver_version。
// 4. 統計多卡情況下的總 VRAM 與已用 VRAM，取最高溫度/使用率/功耗/風扇轉速，並以 map 統計相同型號顯示卡數量。
func (s *SysMonitor) GetGPUTelemetry() GPUTelemetry {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,memory.total,memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw,power.limit,fan.speed,driver_version", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		// 分支 1: 無 NVIDIA GPU 或未安裝 nvidia-smi 指令
		return GPUTelemetry{Summary: "No GPU Detected"}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		// 分支 2: 無輸出內容
		return GPUTelemetry{Summary: "No GPU Detected"}
	}

	var totalVRAM, usedVRAM, maxGPUUtil, maxMemUtil, maxTemp, maxFan int
	var maxPower, maxPowerLimit float64
	var driverVer string
	statsMap := make(map[string]int)

	// 逐行解析各張 GPU 顯卡 CSV
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) < 10 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		vramTot, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		vramU, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		gUtil, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
		mUtil, _ := strconv.Atoi(strings.TrimSpace(parts[4]))
		temp, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
		pwr, _ := strconv.ParseFloat(strings.TrimSpace(parts[6]), 64)
		pwrLim, _ := strconv.ParseFloat(strings.TrimSpace(parts[7]), 64)
		fan, _ := strconv.Atoi(strings.TrimSpace(parts[8]))
		driverVer = strings.TrimSpace(parts[9])

		// 累加顯存用量
		totalVRAM += vramTot
		usedVRAM += vramU

		// 取各顯卡中的最大指標極值
		if gUtil > maxGPUUtil {
			maxGPUUtil = gUtil
		}
		if mUtil > maxMemUtil {
			maxMemUtil = mUtil
		}
		if temp > maxTemp {
			maxTemp = temp
		}
		if pwr > maxPower {
			maxPower = pwr
		}
		if pwrLim > maxPowerLimit {
			maxPowerLimit = pwrLim
		}
		if fan > maxFan {
			maxFan = fan
		}

		key := fmt.Sprintf("%s(%dMB)", name, vramTot)
		statsMap[key]++
	}

	// 組裝型號摘要字串
	var summaryParts []string
	for key, count := range statsMap {
		summaryParts = append(summaryParts, fmt.Sprintf("%s x%d", key, count))
	}
	summaryStr := strings.Join(summaryParts, ", ")

	return GPUTelemetry{
		Summary:       summaryStr,
		GPUTemp:        maxTemp,
		GPUUtil:        maxGPUUtil,
		MemUtil:        maxMemUtil,
		VRAMUsed:       usedVRAM,
		VRAMTotal:      totalVRAM,
		PowerDraw:      maxPower,
		PowerLimit:     maxPowerLimit,
		FanSpeed:       maxFan,
		DriverVersion:  driverVer,
	}
}
