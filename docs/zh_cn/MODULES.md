# Yuanyi Client Agent - 模块对照与调用矩阵 (简体中文)

本文档索引所有 **Yuanyi Client Agent** 的 Go 源代码文件、模块角色与技术手册对照。

---

## 🗂 文件与技术手册索引表

| 源代码文件 | 模块角色 | 主要 Struct | 技术手册链接 |
| :--- | :--- | :--- | :--- |
| **[`main.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/main.go)** | 入口点与信号监听 | `main` | [`docs/zh_cn/ARCHITECTURE.md`](ARCHITECTURE.md#11-maingo---应用程序启动器) |
| **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** | 主控制容器编排器 | `App` | [`docs/zh_cn/APP_CONTAINER.md`](APP_CONTAINER.md) |
| **[`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)** | 配置解析与网卡检测 | `ClientConfig`, `VLLMConfig` | [`docs/zh_cn/CONFIG.md`](CONFIG.md) |
| **[`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go)** | API 网关与代理分发器 | `LocalDispatcher`, `BackendInfo` | [`docs/zh_cn/GATEWAY_PROXY.md`](GATEWAY_PROXY.md) |
| **[`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go)** | libp2p 网络与 GossipSub | `NetworkNode`, `GPUInfo` | [`docs/zh_cn/P2P_NETWORK.md`](P2P_NETWORK.md) |
| **[`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go)** | Ray & vLLM 进程编排 | `Runner` | [`docs/zh_cn/RUNNER_DOCKER.md`](RUNNER_DOCKER.md) |
| **[`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go)** | 遥测爬虫与 NVML 监控 | `SysMonitor`, `VLLMMetrics` | [`docs/zh_cn/TELEMETRY_SYS.md`](TELEMETRY_SYS.md) |
| **[`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go)** | 终端 TUI 面板 | `TUI`, `Stats` | [`docs/zh_cn/DASHBOARD_UI.md`](DASHBOARD_UI.md) |
| **[`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go)** | Web 监控仪表板 | `embed.FS` | [`docs/zh_cn/DASHBOARD_UI.md`](DASHBOARD_UI.md) |
| **`server_*.go`**（可选） | Hub 模式：节点数据库、算分、派发器、仪表板 | `DBManager`, `RankManager`, `ProxyServer` | [`docs/zh_cn/HUB_MODE.md`](HUB_MODE.md) |
