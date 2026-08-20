# Mooncake 2.0 Client Agent - 多層次架構與模組規格說明書

本文件為 Mooncake 2.0 Client Agent 提供完整的 7 大層級架構說明，保留原本散落於原始碼頭部的所有設計細節與演算法說明。

> [!NOTE]
> 選用的第 8 層「Hub 模式」（合併原本獨立中央伺服器的能力）請參閱
> [`HUB_MODE.md`](HUB_MODE.md)，或英文完整版 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md#-layer-8-hub-mode-optional-central-server-merge)。

---

## 📐 系統分層架構概覽 (System Layer Overview)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Layer 1: 進入點與主控編排器                           │
│                    (main.go, app.go)                                    │
└────────────────────────────────────┬────────────────────────────────────┘
                                     │
    ┌────────────────────────────────┼────────────────────────────────┐
    ▼                                ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│  Layer 2: 設定管理    │ │ Layer 3: API 網關     │ │ Layer 4: P2P 網路     │
│  (config.go)          │ │ (proxy.go)            │ │ (p2p.go)              │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
    │                                │                                │
    ├────────────────────────────────┼────────────────────────────────┘
    ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│ Layer 5: 進程管理     │ │ Layer 6: 系統遙測     │ │ Layer 7: UI 與 Web    │
│ (runner.go)           │ │ (sys.go)              │ │ (tui.go, web.go)      │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
```

---

## 🏛 Layer 1: 進入點與主控編排器 (Entry Point & Master Orchestrator)

### 1.1 `main.go` - 應用程式啟動器
- **模組名稱**：Application Main Entry Point
- **系統角色**：
  負責 Agent 的引導與啟動。包含載入設定檔、實例化主應用容器 `App`、監聽 OS 關機信號（`SIGINT`, `SIGTERM`），並在收到關機指示時啟動優雅關機程序。

### 1.2 `app.go` - 主控制引擎容器
- **詳細技術手冊**：**[📖 `app.go` 技術手冊 (`docs/zh_tw/APP_CONTAINER.md`)](APP_CONTAINER.md)**
- **模組名稱**：App Central Application Container
- **系統角色**：
  核心依賴注入容器，持有 4 大子系統指標：Configuration（設定）、TUI（終端面板）、SysMonitor（硬體遙測）、P2P Network（網路）與 Runner（進程編排器）。

---

## ⚙️ Layer 2: 設定管理與網卡自動偵測 (Configuration & Auto-Detection)

### 2.1 `config.go` - 設定解析器
- **詳細技術手冊**：**[⚙️ 設定管理與參數手冊 (`docs/zh_tw/CONFIG.md`)](CONFIG.md)**
- **模組名稱**：Configuration & System Auto-Detection
- **系統角色**：
  解析 `config.json`，自動剝離單行 `//` 註解，動態偵測實體網卡，並提供邊界 Port 衝突防禦機制。

---

## 🔀 Layer 3: OpenAI API 網關與本地代理 (Gateway & Proxy Dispatcher)

### 3.1 `proxy.go` - 本地優先代理分發器
- **詳細技術手冊**：**[🔀 Gateway Proxy 手冊 (`docs/zh_tw/GATEWAY_PROXY.md`)](GATEWAY_PROXY.md)**
- **模組名稱**：OpenAI API Gateway & Disaggregated Scheduler
- **系統角色**：
  於 `50006` 埠提供相容 OpenAI 格式的 API 網關。支援 `atomic.Bool` vLLM 健康預熱檢查、透明 0ms SSE 串流代理、Mode 1 混合與 Mode 2 分離推理調度器。

---

## 🌐 Layer 4: P2P 網路與 Swarm 拓撲 (P2P Mesh Network)

### 4.1 `p2p.go` - libp2p 網路節點
- **詳細技術手冊**：**[🌐 P2P Network 手冊 (`docs/zh_tw/P2P_NETWORK.md`)](P2P_NETWORK.md)**
- **模組名稱**：P2P Mesh Network Agent
- **系統角色**：
  管理 PSK 私有網路密鑰 (`swarm.key`)、Badger DB Peerstore 持久化、Kademlia DHT 引導、GossipSub 顯卡遙測廣播與 SHA-256 本地 TCP VIP 代理 (`127.0.0.X:80Y`)。

---

## ⚡ Layer 5: 進程與容器編排 (Process & Container Orchestration)

### 5.1 `runner.go` - Ray 叢集與 vLLM 進程管理器
- **詳細技術手冊**：**[🏃 Process & Docker 手冊 (`docs/zh_tw/RUNNER_DOCKER.md`)](RUNNER_DOCKER.md)**
- **模組名稱**：Process & Container Orchestrator
- **系統角色**：
  當 Go Agent 作為容器內 PID 1 時，原生執行並管理 Ray Head 與 vLLM 推理引擎進程。

---

## 📊 Layer 6: 硬體遙測與效能監控 (Telemetry & Metrics)

### 6.1 `sys.go` - 遙測爬蟲與 NVML 監控器
- **詳細技術手冊**：**[📊 Telemetry & Metrics 手冊 (`docs/zh_tw/TELEMETRY_SYS.md`)](TELEMETRY_SYS.md)**
- **模組名稱**：System & Hardware Telemetry Agent
- **系統角色**：
  每 2 秒爬取 vLLM Prometheus Metrics 端點與 NVML `nvidia-smi` 顯卡溫度、功耗與 VRAM 使用率。

---

## 🖥 Layer 7: 使用者介面與 Web 儀表板 (User Interfaces)

### 7.1 `tui.go` & `web.go` - 介面與數據存檔
- **詳細技術手冊**：**[🖥️ User Interfaces 手冊 (`docs/zh_tw/DASHBOARD_UI.md`)](DASHBOARD_UI.md)**
- **模組名稱**：Terminal UI & Web Monitoring Dashboard
- **系統角色**：
  提供 4 分頁 TUI 終端面板（支援無 TTY 時自動切換 Headless 背景模式），以及 `50007` 埠嵌入式 Web Console。
