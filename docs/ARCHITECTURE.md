# Mooncake 2.0 Client Agent - Layered Architecture & Module Specification

This document provides an exhaustive, multi-layered architectural specification for the Mooncake 2.0 Client Agent. It preserves the complete structural and algorithmic documentation originally embedded across the Go source files, organized into functional system layers.

---

## 📐 System Layer Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Layer 1: Entry Point & Master Orchestrator           │
│                    (main.go, app.go)                                    │
└────────────────────────────────────┬────────────────────────────────────┘
                                     │
    ┌────────────────────────────────┼────────────────────────────────┐
    ▼                                ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│  Layer 2: Config      │ │ Layer 3: API Gateway  │ │ Layer 4: P2P Network  │
│  (config.go)          │ │ (proxy.go)            │ │ (p2p.go)              │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
    │                                │                                │
    ├────────────────────────────────┼────────────────────────────────┘
    ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│ Layer 5: Process Mgt  │ │ Layer 6: Telemetry    │ │ Layer 7: UI & Web     │
│ (runner.go)           │ │ (sys.go)              │ │ (tui.go, web.go)      │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
                                     │
                                     ▼ (only when server_mode.enabled)
                    ┌────────────────────────────────────┐
                    │ Layer 8: Hub Mode                   │
                    │ (server_db.go, server_rank.go,      │
                    │  server_p2p.go, server_proxy.go,    │
                    │  server_web.go)                     │
                    └────────────────────────────────────┘
```

---

## 🏛 Layer 1: Entry Point & Master Orchestrator

### 1.1 `main.go` - Application Bootstrapper
- **Module Name**: Application Main Entry Point
- **System Role**:
  Acts as the bootstrapper for the client agent. It handles configuration loading, master app instantiation, OS shutdown signal listening (`SIGINT`, `SIGTERM`), and gracefully triggers container/process cleanup before termination.
- **Core Workflow**:
  1. Calls `LoadOrCreateConfig("./config.json")` to load runtime options.
  2. Instantiates master application container `NewApp(cfg)`.
  3. Registers signal notification channel (`os/signal`) for `SIGINT` and `SIGTERM`.
  4. Spawns background signal listener goroutine to trigger `app.Stop()`.
  5. Executes `app.Start(ctx)` and enters main event loop.

### 1.2 `app.go` - Master Engine Container
- **Detailed Specification**: **[📖 `app.go` Technical Manual (`docs/APP_CONTAINER.md`)](APP_CONTAINER.md)**
- **Module Name**: App Central Application Container
- **System Role**:
  The central dependency injection container holding pointers to all 4 core subsystems: Configuration, TUI, Hardware Telemetry, P2P Network, and Process Runner.
- **Subsystem Handles**:
  - `Config (*ClientConfig)`: Global options parsed from `config.json`.
  - `TUI (*TUI)`: Terminal user interface & ring-buffer logger (`tui.go`).
  - `Sys (*SysMonitor)`: Hardware & vLLM Prometheus metrics scraper (`sys.go`).
  - `P2P (*NetworkNode)`: libp2p host, DHT, & GossipSub network (`p2p.go`).
  - `Runner (*Runner)`: Ray Head & vLLM process/container orchestrator (`runner.go`).
- **Lifecycle Methods**:
  - `NewApp(cfg)`: Instantiates master engine and builds sub-modules.
  - `Start(ctx)`: Sequentially boots `Sys`, `P2P`, `Runner`, `WebDashboard`, and starts `TUI`.
  - `Stop()`: Teardown sequence terminating Ray/vLLM processes and closing P2P handles.

---

## ⚙️ Layer 2: Configuration & Auto-Detection

### 2.1 `config.go` - Configuration Parser & Network Detector
- **Module Name**: Configuration & System Auto-Detection
- **System Role**:
  Provides structural definitions for `config.json`, strips inline `//` comments, auto-detects active host NICs, and applies defensive defaults.
- **Struct Definitions**:
  - `DockerConfig`: Container name, image tag, network mode (`host`), shared memory (`16gb`), and NCCL network interface.
  - `PathsConfig`: Host-to-container volume mount mapping (`config_path`, `model_path`, `mooncake_path`).
  - `VLLMConfig`: vLLM engine arguments (`model_name`, `max_model_len`, `gpu_memory_utilization`, `port`, `kv_role`, `mooncake_bootstrap_port`).
  - `P2PConfig`: Multiaddress connection string for Tracker/Bootstrap node.
  - `ClientConfig`: Master container struct mapping full `config.json`.
- **Core Functions**:
  - `detectActiveNetworkInterface()`: Scans network interfaces (`net.Interfaces`) to auto-select non-loopback active physical NIC (`eth0`, `enp*`, `wlan*`).
  - `removeCommentLines()`: Strips `//` single-line comments from JSON string before parsing.
  - `LoadOrCreateConfig(filename)`: Reads/creates `config.json` with safety fallback values.

---

## 🔀 Layer 3: OpenAI API Gateway & Local-First Proxy

### 3.1 `proxy.go` - Local-First Gateway & P/D Scheduler
- **Module Name**: OpenAI API Gateway & Disaggregated Scheduler
- **System Role**:
  Serves an OpenAI-compatible API Gateway on port `50006` (`/v1/chat/completions`, `/v1/models`, `/health`). Implements Local-First routing with zero-buffer SSE streaming and vLLM readiness checking.
- **Key Mechanisms**:
  1. **vLLM Readiness Checking (`startVLLMHealthChecker`)**:
     Background polling checks `http://127.0.0.1:8100/health` with `atomic.Bool` state, preventing warmup errors during initial model loading.
  2. **Local-First Passthrough (`proxyToLocalVLLMDirect`)**:
     Pipes HTTP POST requests directly to local GPU vLLM (`8100`) using zero-buffer `http.Flusher` streaming. Delivers 0ms network overhead and full Server-Sent Events (SSE) compatibility.
  3. **Mode 1: Local-First & PD-Together Hybrid Mode**:
     Tries local GPU execution first. If local vLLM is unready/failing, automatically falls back to remote P2P peers.
  4. **Mode 2: Disaggregated Prefill/Decode Mode**:
     Executes two-stage chained inference across specialized Prefill and Decode nodes, passing Mooncake KV Cache transfer parameters.
  5. **Mooncake KV Tunnel Proxy (`handleKVTunnel`)**:
     Proxies `/mooncake_kv/` HTTP transfer streams across libp2p nodes on port `8998`.

---

## 🌐 Layer 4: P2P Mesh Network & Peer Discovery

### 4.1 `p2p.go` - libp2p Node, DHT & GossipSub Swarm
- **Module Name**: P2P Mesh Network Agent
- **System Role**:
  Manages encrypted P2P mesh connectivity, Kademlia DHT bootstrapping, mDNS LAN discovery, GossipSub status broadcasting, and local TCP VIP proxying.
- **Key Components**:
  - **Private Network (PSK)**: Enforces private swarm access via `swarm.key`.
  - **Badger Peerstore**: Persists peer info on disk (`./my-peerstore`).
  - **Custom Protocols**:
    - `/gpu-service/1.0.0`: Health check ping/pong protocol.
    - `/mooncake-proxy/1.0.0`: HTTP-over-libp2p API & KV transfer tunnel protocol.
  - **Local VIP Proxy (`generateVIP` & `startLocalProxyForPeer`)**:
    Computes a deterministic loopback IP (`127.0.0.X:80Y`) from SHA-256 of PeerID, creating a local TCP proxy to transparently forward traffic to remote peers.
  - **GossipSub Broadcasting (`gossipPublisher` & `gossipSubscriber`)**:
    Periodically (every 3s) broadcasts `GPUInfo` JSON payload containing GPU temp, VRAM, utilization, throughput, and token stats.

---

## ⚡ Layer 5: Process & Container Orchestration

### 5.1 `runner.go` - Ray Cluster & vLLM Direct Process Manager
- **Module Name**: Process & Container Orchestrator
- **System Role**:
  Manages the lifecycle of Ray Head node and vLLM inference engine processes inside the container (Direct Mode) or via Docker CLI (Container Mode).
- **Core Operations**:
  - `isDirectExecution()`: Detects whether running inside the All-in-One container (`ALL_IN_ONE=true` or `/opt/dynamo/venv/bin/vllm` present).
  - `startVLLMDirectly(ctx)`:
    1. Direct execution of `/opt/dynamo/venv/bin/ray start --head --dashboard-port 8275 --port 6389`.
    2. Constructs `vllm serve` arguments (`--gpu-memory-utilization 0.75`, `--max-model-len 8192`, `--kv-transfer-config MooncakeConnector`).
    3. Pipes stdout and stderr in real-time to TUI log buffers.
  - `Stop()`: Sends termination signals (`Kill()`) to Ray and vLLM processes on shutdown.

---

## 📊 Layer 6: Hardware & Performance Telemetry

### 6.1 `sys.go` - Metrics Scraper & NVML GPU Monitor
- **Module Name**: System & Hardware Telemetry Agent
- **System Role**:
  Scrapes vLLM Prometheus metrics endpoint (`http://localhost:8100/metrics`) every 2 seconds and queries GPU hardware state via `nvidia-smi`.
- **Telemetry Indicators**:
  - **vLLM Inference Metrics**: Prefill speed (tokens/s), Generation speed (tokens/s), TTFT (s), KV Cache memory usage (%), Queue depth (Active Requests).
  - **Host System Metrics**: CPU utilization (%) and Process RSS memory (MB/GB) using `gopsutil`.
  - **GPU Hardware Telemetry**: GPU core temp (℃), GPU utilization (%), VRAM usage (MB/Total MB), power draw (W), fan speed (%), and driver version.

---

## 🖥 Layer 7: User Interface & Web Dashboard

### 7.1 `tui.go` - Interactive Terminal UI & Stats Persistence
- **Module Name**: Terminal UI & Stats Persistence Agent
- **System Role**:
  Renders a 4-tab interactive terminal interface (`tview`/`tcell`) with thread-safe ring-buffer logging, auto-scroll, and disk persistence (`stats.json`).
- **Features**:
  - **Tab 1: Dashboard**: Live Node statistics (CPU, RAM, GPU, Requests, Tokens) + Connected Peers table.
  - **Tabs 2-4: Logs**: System logs, vLLM console output, and Docker logs.
  - **Headless Fallback**: Automatically falls back to background headless mode if running without TTY.
  - **Persistence**: Periodically saves metrics to `./stats.json` every 5 seconds.

### 7.2 `web.go` - Web Dashboard HTTP Server
- **Module Name**: Web Dashboard HTTP Server
- **System Role**:
  Hosts a Web Monitoring Dashboard on port `50007`. The dashboard itself is a Vue 3 + Vite +
  Tailwind CSS single-page app in [`web-ui/`](../web-ui) (hash-based `vue-router`, so the
  server needs no SPA-fallback logic); its `npm run build` output (`web-ui/dist/`) is embedded
  directly into the binary via Go 1.16+ `embed.FS`. See
  [`docs/DASHBOARD_UI.md`](DASHBOARD_UI.md) for the frontend architecture.
- **RESTful Endpoints**:
  - `GET /`: Renders static dashboard console.
  - `GET /api/peers`: Online P2P peers status.
  - `GET /api/node_info`: Local PeerID & server addresses.
  - `GET /api/stats`: Cluster-wide throughput, TTFT, & KV cache metrics.
  - `GET /api/logs`: Aggregated system, vLLM, and Docker logs.
  - `GET /POST /api/config`: Reads, updates, and backups `config.json`.

---

## 🛰 Layer 8: Hub Mode (Optional Central Server Merge)

### 8.1 `server_db.go` / `server_rank.go` - Peer Database & Contribution Scoring
- **Module Name**: Hub Peer Database & GPU Scoring Engine
- **System Role**:
  Owns a local SQLite database (`peers.db`) tracking every peer this node has observed, plus a
  fuzzy GPU-model-to-VRAM matcher used to score contribution and publish `top.json` every 10s.

### 8.2 `server_p2p.go` - Connection Tracking & Health Checks
- **Module Name**: Hub Connectivity Agent
- **System Role**:
  Registers a `libp2p.Notifee` (`ConnNotifee`) that mirrors connect/disconnect events into
  `peers.db`, and runs `startServerPingLoop` to periodically ping every known peer and update
  its failure count / penalty points.

### 8.3 `server_proxy.go` - Central Prefill/Decode Dispatcher
- **Module Name**: Hub Dispatch Service
- **System Role**:
  Mirrors `proxy.go`'s dispatcher, but scoped to this hub's own view of the mesh (built from
  its local `peers.db`) rather than the local vLLM instance. Serves `/api/cluster_topology` and
  the same OpenAI-compatible endpoints on `server_mode.proxy_port`.

### 8.4 `server_web.go` - Hub Dashboard API
- **Module Name**: Hub Dashboard JSON API
- **System Role**:
  `RegisterHubRoutes` mounts the leaderboard/peer-list/audit-event JSON endpoints under
  `/hub/api/*` on the client's own `web.go` HTTP server (`web_port`), rather than listening on
  a separate port. There is no separate hub HTML page any more: the hub views
  (`src/views/hub/*.vue`) are part of the same Vue SPA `web.go` embeds, reached client-side via
  `vue-router`'s hash routes (`/#/hub`, `/#/hub/history`, `/#/hub/leaderboard`) -- `/hub/` is
  only ever a real server-side path for the `/hub/api/*` JSON endpoints, never for a page.
  `web.go`'s `/api/node_info` reports `hub_mode_enabled`, which the dashboard uses to reveal
  the "Cluster (Hub Mode)" nav section.

### 8.5 Multi-Hub Consistency Model
Unlike the standalone Central Server this replaces, hub mode has no single fixed instance: any
number of nodes can enable `server_mode.enabled` simultaneously. Each independently persists
what it observes on the existing network-wide GossipSub topic into its own `peers.db`, so
multiple hubs converge to the same eventually-consistent view without any hub-to-hub
replication protocol and without a single point of failure. See
[`HUB_MODE.md`](HUB_MODE.md) for the full design rationale and trade-offs.
