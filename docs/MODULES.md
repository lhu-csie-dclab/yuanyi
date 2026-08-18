# Mooncake 2.0 Client Agent - Module Reference Guide

This document indexes all source code files in the **Mooncake 2.0 Client Agent**, detailing their module roles, contained data structures, and primary functions.

---

## 🗂 File & Module Index

| Source File | Module Role | Key Structs | Primary Functions |
| :--- | :--- | :--- | :--- |
| **[`main.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/main.go)** | Entry Point & Signal Listener | `main` | `main()` |
| **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** | Master Application Container | `App` | `NewApp()`, `Start()`, `Stop()` |
| **[`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)** | Config Parser & NIC Auto-Detector | `ClientConfig`, `VLLMConfig`, `DockerConfig`, `PathsConfig`, `P2PConfig` | `LoadOrCreateConfig()`, `detectActiveNetworkInterface()`, `removeCommentLines()` |
| **[`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go)** | API Gateway & Local-First Proxy | `LocalDispatcher`, `BackendInfo`, `ClusterTopologyResponse` | `StartLocalDispatcher()`, `proxyToLocalVLLMDirect()`, `handleProxyRequest()`, `handleKVTunnel()`, `startVLLMHealthChecker()` |
| **[`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go)** | libp2p Mesh & GossipSub Network | `NetworkNode`, `GPUInfo`, `discoveryNotifee` | `Start()`, `setupStreams()`, `bootstrapNode()`, `keepAlive()`, `generateVIP()`, `startLocalProxyForPeer()`, `gossipPublisher()`, `gossipSubscriber()` |
| **[`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go)** | Ray & vLLM Process Orchestrator | `Runner` | `Start()`, `startVLLMDirectly()`, `startVLLMContainer()`, `isDirectExecution()`, `Stop()` |
| **[`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go)** | Metrics Scraper & NVML Telemetry | `SysMonitor`, `VLLMMetrics`, `GPUTelemetry` | `Start()`, `GetMetrics()`, `GetGPUModelSummary()`, `GetGPUTelemetry()`, `metricScraper()` |
| **[`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go)** | Terminal UI & Stats Persistence | `TUI`, `Stats`, `PersistentStats`, `PeerRecord` | `NewTUI()`, `Run()`, `AddLog()`, `AddVLLMLog()`, `AddDockerLog()`, `RecordPeerInfo()`, `UpdateStats()`, `loadStatsDisk()`, `saveStatsDisk()` |
| **[`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go)** | Web Monitoring Dashboard | `embed.FS` | `StartClientWebDashboard()`, API Handlers (`/api/peers`, `/api/stats`, `/api/logs`, `/api/config`) |

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
