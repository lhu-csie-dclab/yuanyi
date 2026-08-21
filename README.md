[English](README.md) | [繁體中文](README_zh-TW.md) | [简体中文](README_zh-CN.md)

# Mooncake 2.0 P2P LLM Inference Client Agent

[![Go Build](https://github.com/lhu-csie-dclab/yuanyi/actions/workflows/go.yml/badge.svg)](https://github.com/lhu-csie-dclab/yuanyi/actions)
[![Docker Build](https://github.com/lhu-csie-dclab/yuanyi/actions/workflows/docker.yml/badge.svg)](https://github.com/lhu-csie-dclab/yuanyi/actions)
[![Code Quality](https://github.com/lhu-csie-dclab/yuanyi/actions/workflows/lint.yml/badge.svg)](https://github.com/lhu-csie-dclab/yuanyi/actions)
[![Security Scan](https://github.com/lhu-csie-dclab/yuanyi/actions/workflows/security.yml/badge.svg)](https://github.com/lhu-csie-dclab/yuanyi/actions)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://golang.org)
[![CUDA Version](https://img.shields.io/badge/CUDA-13.0+-76B900?style=flat&logo=nvidia)](https://developer.nvidia.com/cuda-toolkit)
[![vLLM Support](https://img.shields.io/badge/vLLM-v0.20.1+-FF6F00?style=flat)](https://github.com/vllm-project/vllm)
[![Mooncake Transfer Engine](https://img.shields.io/badge/Mooncake-v0.3.10.post2-red?style=flat)](https://github.com/kvcache-ai/Mooncake)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

An all-in-one, high-performance **P2P Large Language Model (LLM) Inference Client Agent** built with Go, libp2p, Ray, and vLLM. 

Mooncake 2.0 Client provides an OpenAI-compatible API Gateway (`/v1/chat/completions`) with **Local-First Proxy Routing**, **Zero-Buffer SSE Streaming**, **vLLM Readiness Health Checking**, and **Mooncake KV Cache Transfer Engine** (`mooncake-transfer-engine-cuda13==0.3.10.post2`) integration for distributed Prefill/Decode (P/D) inference swarms.

---

> [!WARNING]
> **Experimental Stage Disclaimer (實驗階段與正式環境部署警語)**
> - **Experimental Research Software**: This project is currently in an **experimental research phase** and is **NOT RECOMMENDED for production (Production) environments**.
> - **Untested Parameters Notice**: Only the explicitly documented baseline configuration (`Qwen3-4B-AWQ`, `protocol: "tcp"`, `concurrency: 100`) has been stress-tested. All other unverified parameters, alternative transport layers, or unlisted models remain **untested** and may produce unstable results.

---

## 📚 Documentation & Architecture Index

For deep-dive technical documentation, multi-layered architectural specifications, and module reference guides, see:

- **[📖 Layered Architecture Specification (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md)**: Full 8-layer functional breakdown (Orchestrator, Gateway, P2P Swarm, Process Manager, Telemetry, UI, optional Hub Mode) including original code design algorithms.
- **[📦 Master App Container Specification (`docs/APP_CONTAINER.md`)](docs/APP_CONTAINER.md)**: Detailed specification of `app.go`, covering struct handles, instantiation, execution flow, and teardown sequence.
- **[⚙️ Configuration Management Guide (`docs/CONFIG.md`)](docs/CONFIG.md)**: Comprehensive guide for `config.go`, `config.json`, `.env.example`, and `mooncake.json`.
- **[🌐 P2P Mesh Network & Swarm Key Guide (`docs/P2P_NETWORK.md`)](docs/P2P_NETWORK.md)**: Detailed guide for `p2p.go`, Badger DB peerstore, GossipSub, TCP VIP proxies, and `swarm.key` generation.
- **[🔀 OpenAI API Gateway & Proxy Guide (`docs/GATEWAY_PROXY.md`)](docs/GATEWAY_PROXY.md)**: Detailed guide for `proxy.go`, Local-First transparent SSE streaming, vLLM health check, and P/D scheduler.
- **[🏃 Process Management & Docker Stack Guide (`docs/RUNNER_DOCKER.md`)](docs/RUNNER_DOCKER.md)**: Detailed guide for `runner.go`, `Dockerfile`, `docker-compose.yml`, and Ray/vLLM orchestration.
- **[📊 System Telemetry & Metrics Guide (`docs/TELEMETRY_SYS.md`)](docs/TELEMETRY_SYS.md)**: Detailed guide for `sys.go`, vLLM Prometheus metrics scraping, NVML GPU stats, and `stats.json`.
- **[🖥️ User Interfaces & Web Dashboard Guide (`docs/DASHBOARD_UI.md`)](docs/DASHBOARD_UI.md)**: Detailed guide for `tui.go` (4-tab terminal console, headless mode) and the Vue 3 + Vite + Tailwind CSS web dashboard (`web-ui/`, embedded via `web.go` on port `50007`).
- **[📈 NVIDIA AIPerf Benchmark & Stress Test Results (`docs/test/BENCHMARK_RESULTS.md`)](docs/test/BENCHMARK_RESULTS.md)**: Official 10,000 requests stress test results evaluated on 10 x RTX A2000 8GB GPUs using NVIDIA AIPerf.
- **[🧬 Multi-Node Fresh-Clone & Concurrent Multi-GPU Test (`docs/test/MULTI_NODE_CLONE_TEST.md`)](docs/test/MULTI_NODE_CLONE_TEST.md)**: Validates a from-scratch `git clone` deployed across 10 independent nodes on 2 hosts, confirming 10 distinct physical GPUs each serve real inference, sequentially and concurrently.
- **[🖥️ Proxmox VE + LXC GPU Passthrough Guide (`docs/install/proxmox/README.md`)](docs/install/proxmox/README.md)**: Host driver, LXC creation, GPU device passthrough, nested Docker, and the `no-cgroups` fix — how the 10-node reference cluster is built.
- **[🐧 Ubuntu Installation Guide (`docs/install/ubuntu/README.md`)](docs/install/ubuntu/README.md)**: The primary, production-tested platform — Docker Engine, NVIDIA Container Toolkit, `swarm.key`, and both the Docker and native build paths.
- **[🪟 Windows Native Deployment Guide (`docs/install/windows/README.md`)](docs/install/windows/README.md)**: Step-by-step guide for native Windows deployment with `uv`, `.venv`, and `SystemPanic/vllm-windows`.
- **[🪟 Windows Native Deployment Test (`docs/test/WINDOWS_NATIVE_TEST.md`)](docs/test/WINDOWS_NATIVE_TEST.md)**: Verified end-to-end run of the native Windows path on an RTX 3080 Laptop — build, startup, single/sequential/concurrent/streaming inference, and the two bugs the run surfaced and fixed.
- **[🧪 Experimental Stage & Untested Parameters Manual (`docs/EXPERIMENTAL.md`)](docs/EXPERIMENTAL.md)**: Detailed experimental research scope, baseline parameters, untested options, and production disclaimers.
- **[🗂 Module & Function Reference Guide (`docs/MODULES.md`)](docs/MODULES.md)**: File-by-file index of data structures, struct definitions, and cross-module call matrices.
- **[🛰️ Hub Mode Guide (`docs/HUB_MODE.md`)](docs/HUB_MODE.md)**: Optional merged Central Server capability — peer database, GPU scoring, central dispatcher, hub dashboard, and the multi-hub consistency model.

---

## 🏛 System Architecture

```mermaid
flowchart TB
    subgraph ClientHost["Host Machine / Client Node"]
        direction TB
        APICaller["Client / Benchmark Tool\n(aiperf, OpenAI SDK)"]
        
        subgraph Container["Single All-in-One Docker Container"]
            direction TB
            GoAgent["PID 1: Go Client Orchestrator (app.go)"]
            
            subgraph GatewayLayer["OpenAI API Gateway (50006)"]
                Dispatcher["LocalDispatcher (proxy.go)\n- Local-First Strategy\n- Transparent SSE Passthrough\n- vLLM Readiness Check"]
            end
            
            subgraph EngineLayer["Local Inference Engine"]
                RayHead["Ray Head Cluster (6389 / 8275)"]
                VLLM["vLLM Engine (8100)\n- Qwen3 / Llama Models\n- MooncakeConnector (8998)"]
            end
            
            subgraph Dashboards["Monitoring Interfaces"]
                WebUI["Web Dashboard (50007)"]
                TUI["Terminal UI (tview/Headless)"]
            end
        end
    end
    
    subgraph Swarm["Mooncake 2.0 P2P Swarm"]
        HubNode["Hub Node(s) (50004/50007 #/hub, 50008)\n- Any peer with server_mode.enabled\n- Topology Sync, NAT Relay, Leaderboard"]
        RemotePeer["Remote P2P Peer Nodes"]
    end
    
    APICaller -->|HTTP POST :50006| Dispatcher
    Dispatcher -->|1st Priority: Passthrough 0ms| VLLM
    Dispatcher -.->|2nd Priority: Fallback| RemotePeer
    GoAgent -->|Direct Exec| RayHead
    RayHead -->|Orchestrates| VLLM
    GoAgent -->|Sync Topology| HubNode
    VLLM <-->|Mooncake KV Transfer :8998| RemotePeer
```

> Every node runs the same client binary. A node becomes part of `Swarm` above as a **hub**
> only if its own `config.json` sets `server_mode.enabled: true`; any number of nodes may do
> so at once, and any node can be `ClientHost` for its own inference regardless of whether it
> is also a hub. See [`docs/HUB_MODE.md`](docs/HUB_MODE.md).

---

## ✨ Key Features

- **🚀 Local-First Proxy & Zero-Buffer SSE Streaming**:
  Directly pipes incoming HTTP requests to the local GPU-accelerated vLLM engine (`http://127.0.0.1:8100`) using zero-buffer chunked streaming (`http.Flusher`). Delivers instant responses with 0ms network overhead and full Server-Sent Events (SSE) compatibility for benchmark tools like `aiperf`.

- **⚡ Atomic vLLM Readiness Health Check**:
  Background polling tracks vLLM startup and model weight allocation via `http://127.0.0.1:8100/health`. Automatically suppresses connection errors during the 15-30 second warmup phase and silently defers to P2P swarm backup when unready.

- **📦 Single-Container All-in-One Deployment**:
  Runs cleanly inside a single Multi-Stage Docker container. The Go agent operates as **PID 1**, natively starting and managing Ray Head and vLLM processes without requiring `/var/run/docker.sock` mounts or host shell scripts.

- **🖥️ Dual Interface Support (Interactive TUI & Headless Mode)**:
  Features an interactive terminal UI powered by `tview` and `tcell`. Automatically detects non-TTY environments (such as headless Docker containers) and seamlessly falls back to background headless mode while keeping API endpoints operational.

- **🎨 Vue 3 + Vite + Tailwind Web Dashboard**:
  The web console (`web-ui/`) is a hash-routed single-page app built with Vue 3, Vite, and Tailwind CSS v4, compiled to static assets and embedded straight into the Go binary via `embed.FS` — the running server still needs no external frontend files. The Dockerfile builds it in its own Node stage before the Go build.

- **🌐 P2P Swarm & Mooncake KV Cache Transfer**:
  Connects to the Mooncake 2.0 swarm over libp2p. Participates in disaggregated Prefill/Decode (P/D) inference topologies, exchanging KV Caches across GPU nodes via `MooncakeConnector` on port `8998`.

- **🛰️ Optional Hub Mode (Merged Central Server)**:
  Any node can opt into `server_mode.enabled` to additionally take on the standalone Central Server's role: a local SQLite peers/leaderboard database, GPU-based scoring, a central P/D dispatcher, and a hub-only dashboard. Multiple hubs can run at once — each independently converges to the same view over the existing GossipSub topic, so there is no single point of failure. See [`docs/HUB_MODE.md`](docs/HUB_MODE.md).

---

### 📁 Directory & File Documentation Map

| Source File / Component | Primary Responsibility | Technical Manual Link |
| :--- | :--- | :--- |
| **[`main.go`](main.go)** | Application entry point & OS signal listener | [📖 Architecture Manual (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md#11-maingo---application-bootstrapper) |
| **[`app.go`](app.go)** | Master application container & orchestrator | [📦 Master App Specification (`docs/APP_CONTAINER.md`)](docs/APP_CONTAINER.md) |
| **[`config.go`](config.go)** / **[`config.json`](config.json)** | Config parser & active NIC auto-detector | [⚙️ Config Guide (`docs/CONFIG.md`)](docs/CONFIG.md) |
| **[`proxy.go`](proxy.go)** | OpenAI API Gateway & Local-First proxy | [🔀 Gateway Proxy Guide (`docs/GATEWAY_PROXY.md`)](docs/GATEWAY_PROXY.md) |
| **[`p2p.go`](p2p.go)** / **[`swarm.key.example`](swarm.key.example)** | libp2p network mesh & GossipSub swarm | [🌐 P2P Network & Key Guide (`docs/P2P_NETWORK.md`)](docs/P2P_NETWORK.md) |
| **[`runner.go`](runner.go)** / **[`Dockerfile`](Dockerfile)** | Ray Head & vLLM process/container runner | [🏃 Process & Docker Guide (`docs/RUNNER_DOCKER.md`)](docs/RUNNER_DOCKER.md) |
| **[`sys.go`](sys.go)** / **[`stats.json`](stats.json)** | Hardware metrics & vLLM Prometheus scraper | [📊 Telemetry & Metrics Guide (`docs/TELEMETRY_SYS.md`)](docs/TELEMETRY_SYS.md) |
| **[`tui.go`](tui.go)** / **[`web.go`](web.go)** / **[`web-ui/`](web-ui)** | Interactive TUI console & Web Dashboard (Vue 3 + Vite + Tailwind) | [🖥️ User Interfaces Guide (`docs/DASHBOARD_UI.md`)](docs/DASHBOARD_UI.md) |
| **`server_*.go`** (optional) | Hub mode: peer database, scoring, dispatcher, dashboard | [🛰️ Hub Mode Guide (`docs/HUB_MODE.md`)](docs/HUB_MODE.md) |
| **`docs/test/`** | AIPerf 10k requests 10 x RTX A2000 test data | [📈 AIPerf Benchmark Results (`docs/test/BENCHMARK_RESULTS.md`)](docs/test/BENCHMARK_RESULTS.md) |
| **`docs/test/`** | 10-node fresh-clone & concurrent multi-GPU validation | [🧬 Multi-Node Clone Test (`docs/test/MULTI_NODE_CLONE_TEST.md`)](docs/test/MULTI_NODE_CLONE_TEST.md) |

---

## 🔌 Network Ports Allocation & Reference Matrix

Available port mapping referenced across the system (`config.json`, `.env`, Python Ray, and Docker container):

| Port | Protocol | Layer / Service | Source / Reference | Description |
| :--- | :--- | :--- | :--- | :--- |
| **`50006`** | HTTP | OpenAI API Gateway | `config.json` (`proxy_port`) | Client API entrypoint (`/v1/chat/completions`, `/v1/models`) |
| **`50007`** | HTTP | Web UI Dashboard | `config.json` / `.env` (`CLIENT_WEB_PORT`) | Vue SPA + stats API; reveals a Cluster/Hub section (hash routes `/#/hub/*`) when `server_mode.enabled` |
| **`8100`** | HTTP | vLLM Engine | `config.json` (`vllm.port`) | Local GPU vLLM inference server endpoint |
| **`8998`** | TCP/HTTP | Mooncake Engine | `config.json` (`mooncake_bootstrap_port`) | Mooncake KV Cache transfer control & negotiation port |
| **`6389`** | TCP | Python Ray Cluster | Ray Head (`--port`) | Ray distributed execution head node port |
| **`8275`** | HTTP | Python Ray Dashboard | Ray Head (`--dashboard-port`) | Ray cluster management dashboard |
| **`50004`** | TCP/libp2p | Bootstrap Seed | `config.json` (`p2p.server_address(es)`) | Bootstrap tracker & NAT relay multiaddress port (also `server_mode.p2p_port` when this node is a hub) |
| **`50008`** | HTTP | Hub Dispatcher (optional) | `config.json` (`server_mode.proxy_port`) | Hub central prefill/decode dispatch, only when `server_mode.enabled` (the dashboard's topology/leaderboard views live on `50007` instead, see above) |

---

## 🛠️ Prerequisites & Installation Guide

### 1. Git Installation & Repository Cloning
Install `git` on your system and clone the repository:

```bash
# Ubuntu / Debian
sudo apt-get update && sudo apt-get install -y git git-lfs

# Clone repository
git clone https://github.com/lhu-csie-dclab/yuanyi.git
cd yuanyi
```

### 2. Go Environment & Local Compilation
This project requires **Go version 1.26.0 or higher** for native compilation and agent development.
The web dashboard (`web-ui/`) is embedded into the binary at compile time via `//go:embed`, so its
built assets (`web-ui/dist/`) must exist *before* `go build` runs. The Docker build handles this
automatically (see below); building natively requires **Node.js 22+** to build the dashboard once first:

```bash
# Verify Go version (requires 1.26.0+)
go version

# Build the web dashboard once (only needed outside Docker; web-ui/dist/ is gitignored)
cd web-ui && npm ci && npm run build && cd ..

# Build local executable binary
go build -v .
```

### 3. Docker Installation on Ubuntu
Building the multi-stage container (`Dockerfile`) requires Docker Engine. Follow the official [Docker Engine Installation on Ubuntu](https://docs.docker.com/engine/install/ubuntu/) guide:

```bash
# Uninstall old versions
sudo apt-get remove docker docker-engine docker.io containerd runc

# Set up Docker repository
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch="$(dpkg --print-architecture)" signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  "$(. /etc/os-release && echo "$VERSION_CODENAME")" stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker Engine & Docker Compose
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Verify Docker installation
docker --version
```

> [!NOTE]
> **Built-in Package Version**: Pre-installed with official CUDA 13 Mooncake Transfer Engine version `mooncake-transfer-engine-cuda13==0.3.10.post2`.

### 4. NVIDIA GPU Container Toolkit
Ensure the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) is installed so Docker containers can access local GPU accelerators.

#### 🧪 Verified Test Environment (測試環境驗證規格)
The system has been fully benchmarked and verified under the following LXC container environment:

| Category | Specification |
| :--- | :--- |
| **Virtualization / Hypervisor** | **Proxmox VE 9.1** (LXC Container) |
| **LXC OS Template** | `ubuntu-26.04-standard_26.04-1_amd64.tar.zst` |
| **NVIDIA Host Driver Version** | `595.71.05` |
| **CUDA Toolkit Version** | `13.2` |
| **GPU Compute Capability** | **`7.5`** (Turing Architecture) |

### 5. Download Demonstration Model (`Qwen3-4B-AWQ`)
The recommended demonstration model is **[Qwen/Qwen3-4B-AWQ](https://huggingface.co/Qwen/Qwen3-4B-AWQ)** from Hugging Face:

```bash
# Install Git LFS
git lfs install

# Download Qwen3-4B-AWQ model to local directory
mkdir -p /home/user/models
cd /home/user/models
git clone https://huggingface.co/Qwen/Qwen3-4B-AWQ
```

---

## 🚀 Quick Start & Deployment

### 1. Environment Configuration

Copy the environment template file:

```bash
cp .env.example .env
```

Configure `.env` to point `ABS_MODEL_PATH` to your local `Qwen3-4B-AWQ` model path:

```env
# Absolute path to local HuggingFace / AWQ model weights
ABS_MODEL_PATH=/home/user/models/Qwen3-4B-AWQ
IFACE=eth0
CLIENT_WEB_PORT=50007
```

### 2. Generate the Private Network Key (`swarm.key`)

> [!IMPORTANT]
> **Do this before the first launch.** A node refuses to start without a valid `swarm.key`, and
> **every node in the same mesh must carry the byte-identical key** — it is the pre-shared key
> (PSK) that defines the private network.

**Starting a new mesh?** Generate a fresh key:

```bash
printf '/key/swarm/psk/1.0.0/\n/base16/\n%s\n' "$(openssl rand -hex 32)" > swarm.key
```

**Joining an existing mesh?** Do **not** generate one — obtain the exact `swarm.key` from
whoever operates that mesh and copy it in verbatim. A mismatched key fails with the misleading
error `failed to negotiate security protocol: incoming message was too large`, which looks like
a network fault rather than a key problem. Confirm it matches a working node with
`sha256sum swarm.key`.

> [!WARNING]
> Do **not** ship `swarm.key.example` as your real key. It is a public placeholder committed to
> this repository, so anyone could use it to join your mesh. Keep your real `swarm.key` secret
> and out of version control (it is already in `.gitignore`).

See [`docs/P2P_NETWORK.md`](docs/P2P_NETWORK.md) for the file format and alternative generators.

### 3. Build and Launch Container

Build the Dockerfile and start the All-in-One service via Docker Compose:

```bash
docker compose up -d --build
```

### 4. Verify System Health

Check the API Gateway health status (`50006`):

```bash
curl http://localhost:50006/health
# Output: OK
```

Query supported models:

```bash
curl http://localhost:50006/v1/models
```

### 5. Execute Chat Completion

Send an OpenAI-compatible request using `Qwen3-4B-AWQ`:

```bash
curl http://localhost:50006/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Qwen3-4B-AWQ",
    "messages": [{"role": "user", "content": "Hello! Explain quantum computing in 2 sentences."}],
    "temperature": 0.7
  }'
```

### 6. 🪟 Windows Native Quick Start

This project natively supports Windows 10/11 without requiring Docker:

#### Step 1: Create Virtual Environment with `uv` & Install Dependencies (One-time Setup)
```powershell
# 1. Create Python 3.12 virtual environment
uv venv .venv --python 3.12

# 2. Install PyTorch (CUDA 12.4 build)
uv pip install torch==2.6.0+cu124 torchvision==0.21.0+cu124 torchaudio==2.6.0+cu124 --extra-index-url https://download.pytorch.org/whl/cu124

# 3. Install precompiled Windows vLLM wheel and compatible Transformers
uv pip install vllm-0.9.2+cu124-cp312-cp312-win_amd64.whl
uv pip install "transformers>=4.48.0,<4.50.0"
```

#### Step 2: Launch Client Agent
```powershell
# Run the compiled binary (or build via `go build .`)
.\go-p2p.exe
```
* The application will **automatically detect Windows**, invoke `nvidia-smi` for hardware telemetry, mount the local `.venv`, and start vLLM + P2P networking seamlessly!
* For complete configuration and background daemon setup, see **[🪟 Windows Deployment Guide (`docs/install/windows/README.md`)](docs/install/windows/README.md)**.

---

## ⚙️ Configuration Reference

Default settings in `config.json`:

```json
{
  "web_port": 50007,
  "proxy_port": 50006,
  "vllm": {
    "port": 8100,
    "model_name": "Qwen3-4B-AWQ",
    "gpu_memory_utilization": 0.75,
    "max_model_len": 8192,
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": 8998
  },
  "server_mode": {
    "enabled": false
  }
}
```

Set `server_mode.enabled: true` to opt this node into hub mode (merged Central Server responsibilities) — see [`docs/HUB_MODE.md`](docs/HUB_MODE.md) for the full field reference.

Mooncake transport settings in `mooncake.json` (`"protocol": "tcp"`):

```json
{
  "metadata_server": "P2PHANDSHAKE",
  "global_segment_size": "0",
  "local_buffer_size": "17179869184",
  "protocol": "tcp",
  "device_name": ""
}
```

---

## 🙏 Acknowledgements & Credits

Mooncake 2.0 Client Agent is built upon and integrates with the following outstanding open-source projects and platforms:

- **[vLLM](https://github.com/vllm-project/vllm)** - A high-throughput and memory-efficient inference and serving engine for LLMs.
- **[vllm-windows](https://github.com/SystemPanic/vllm-windows)** (SystemPanic/vllm-windows) - High-performance precompiled vLLM Windows runtime builds and environment compatibility support.
- **[Mooncake](https://github.com/kvcache-ai/Mooncake)** - KVCache-centric Disaggregated Architecture for LLM Serving.
- **[go-libp2p](https://github.com/libp2p/go-libp2p)** - Modular P2P networking library powering the decentralized mesh network.
- **[Ray](https://github.com/ray-project/ray)** - Unified framework for scaling AI and Python applications.
- **[aiperf](https://github.com/ai-dynamo/aiperf)** (`nvcr.io/nvidia/ai-dynamo/aiperf`) - Generative AI benchmark suite for load testing LLM inference services.

---

## 📜 License

Distributed under the **Apache License 2.0**. See [`LICENSE`](LICENSE) for more information.
