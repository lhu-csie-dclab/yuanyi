# Mooncake 2.0 Client Agent - 模組對照與呼叫矩陣 (繁體中文)

本文件索引所有 **Mooncake 2.0 Client Agent** 的 Go 原始碼檔案、模組角色與技術手冊對照。

---

## 🗂 檔案與技術手冊索引表

| 原始碼檔案 | 模組角色 | 主要 Struct | 技術手冊連結 |
| :--- | :--- | :--- | :--- |
| **[`main.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/main.go)** | 進入點與信號監聽 | `main` | [`docs/zh_tw/ARCHITECTURE.md`](ARCHITECTURE.md#11-maingo---應用程式啟動器) |
| **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** | 主控制容器編排器 | `App` | [`docs/zh_tw/APP_CONTAINER.md`](APP_CONTAINER.md) |
| **[`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)** | 設定解析與網卡偵測 | `ClientConfig`, `VLLMConfig` | [`docs/zh_tw/CONFIG.md`](CONFIG.md) |
| **[`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go)** | API 網關與代理分發器 | `LocalDispatcher`, `BackendInfo` | [`docs/zh_tw/GATEWAY_PROXY.md`](GATEWAY_PROXY.md) |
| **[`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go)** | libp2p 網路與 GossipSub | `NetworkNode`, `GPUInfo` | [`docs/zh_tw/P2P_NETWORK.md`](P2P_NETWORK.md) |
| **[`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go)** | Ray & vLLM 進程編排 | `Runner` | [`docs/zh_tw/RUNNER_DOCKER.md`](RUNNER_DOCKER.md) |
| **[`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go)** | 遙測爬蟲與 NVML 監控 | `SysMonitor`, `VLLMMetrics` | [`docs/zh_tw/TELEMETRY_SYS.md`](TELEMETRY_SYS.md) |
| **[`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go)** | 終端 TUI 面板 | `TUI`, `Stats` | [`docs/zh_tw/DASHBOARD_UI.md`](DASHBOARD_UI.md) |
| **[`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go)** | Web 監控儀表板 | `embed.FS` | [`docs/zh_tw/DASHBOARD_UI.md`](DASHBOARD_UI.md) |
| **`server_*.go`**（選用） | Hub 模式：節點資料庫、算分、派發器、儀表板 | `DBManager`, `RankManager`, `ProxyServer` | [`docs/zh_tw/HUB_MODE.md`](HUB_MODE.md) |
