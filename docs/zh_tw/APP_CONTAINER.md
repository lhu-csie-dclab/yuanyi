# `app.go` - 主應用容器與模組編排手冊

本文件提供對 **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** 程式碼的深度說明。

---

## 🏛 系統定位與架構角色

在 Mooncake 2.0 Client 架構中，`app.go` 扮演**主控制引擎容器 (Master Control Container / Dependency Injection Root)**。

`App` 結構體封裝了所有核心子系統的指標（Pointers），並管理嚴格的初始化、順序啟動、併發 Goroutines 與關機清理流程。

---

## 🧩 結構體定義：`App`

```go
type App struct {
	Config *ClientConfig // 來自 config.json 的全域設定 (config.go)
	TUI    *TUI          // 終端機面板與日誌緩衝區 (tui.go)
	Sys    *SysMonitor   // 硬體遙測與 vLLM Prometheus 爬蟲 (sys.go)
	P2P    *NetworkNode  // libp2p 私網、DHT、GossipSub 與 VIP 代理 (p2p.go)
	Runner *Runner       // Ray Head 與 vLLM 進程編排器 (runner.go)
}
```

---

## ⚙️ 核心 Function 與執行邏輯

1. **`NewApp(cfg *ClientConfig) *App`**：實例化 `App` 容器並將指標注入至各子系統（依賴注入模式）。
2. **`Start(ctx context.Context) error`**：順序啟動子系統（`Sys.Start()` ➔ `P2P.Start()` ➔ 背景啟動 `Runner` & `Web Console` ➔ 進入 `TUI.Run()` 阻塞迴圈）。
3. **`Stop()`**：優雅關機程序，停止 Ray/vLLM 進程並關閉 P2P 連線。
