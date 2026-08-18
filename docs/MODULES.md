# Mooncake 2.0 Client Agent - Module Reference Guide

This document indexes all source code files in the **Mooncake 2.0 Client Agent**, detailing their module roles, contained data structures, and primary functions.

---

## 🗂 File & Module Index

| Source File | Module Role | Key Structs | Documentation Manual |
| :--- | :--- | :--- | :--- |
| **[`main.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/main.go)** | Entry Point & Signal Listener | `main` | [`docs/ARCHITECTURE.md`](ARCHITECTURE.md#11-maingo---application-bootstrapper) |
| **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** | Master Application Container | `App` | [`docs/APP_CONTAINER.md`](APP_CONTAINER.md) |
| **[`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)** | Config Parser & NIC Auto-Detector | `ClientConfig`, `VLLMConfig` | [`docs/CONFIG.md`](CONFIG.md) |
| **[`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go)** | API Gateway & Local-First Proxy | `LocalDispatcher`, `BackendInfo` | [`docs/GATEWAY_PROXY.md`](GATEWAY_PROXY.md) |
| **[`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go)** | libp2p Mesh & GossipSub Network | `NetworkNode`, `GPUInfo` | [`docs/P2P_NETWORK.md`](P2P_NETWORK.md) |
| **[`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go)** | Ray & vLLM Process Orchestrator | `Runner` | [`docs/RUNNER_DOCKER.md`](RUNNER_DOCKER.md) |
| **[`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go)** | Metrics Scraper & NVML Telemetry | `SysMonitor`, `VLLMMetrics` | [`docs/TELEMETRY_SYS.md`](TELEMETRY_SYS.md) |
| **[`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go)** | Terminal UI & Stats Persistence | `TUI`, `Stats`, `PersistentStats` | [`docs/DASHBOARD_UI.md`](DASHBOARD_UI.md) |
| **[`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go)** | Web Monitoring Dashboard | `embed.FS` | [`docs/DASHBOARD_UI.md`](DASHBOARD_UI.md#web-monitoring-dashboard-webgo-usage-guide) |

---

## 🔄 Cross-Module Call Matrix

```
main.go
  └── LoadOrCreateConfig() [config.go]
  └── NewApp()             [app.go]
  └── App.Start()          [app.go]
        ├── SysMonitor.Start()           [sys.go]
        ├── NetworkNode.Start()          [p2p.go]
        │     └── StartLocalDispatcher() [proxy.go]
        ├── Runner.Start()               [runner.go]
        ├── StartClientWebDashboard()    [web.go]
        └── TUI.Run()                    [tui.go]
```
