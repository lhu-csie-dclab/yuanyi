# Mooncake 2.0 Client Agent - 多层次架构与模块规格说明书 (简体中文)

本文档为 Mooncake 2.0 Client Agent 提供完整的 7 大层级架构说明。

---

## 📐 系统分层架构概览 (System Layer Overview)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Layer 1: 入口点与主控编排器                           │
│                    (main.go, app.go)                                    │
└────────────────────────────────────┬────────────────────────────────────┘
                                     │
    ┌────────────────────────────────┼────────────────────────────────┐
    ▼                                ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│  Layer 2: 配置管理    │ │ Layer 3: API 网关     │ │ Layer 4: P2P 网络     │
│  (config.go)          │ │ (proxy.go)            │ │ (p2p.go)              │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
    │                                │                                │
    ├────────────────────────────────┼────────────────────────────────┘
    ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│ Layer 5: 进程管理     │ │ Layer 6: 系统遥测     │ │ Layer 7: UI 与 Web    │
│ (runner.go)           │ │ (sys.go)              │ │ (tui.go, web.go)      │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
```

---

## 🏛 Layer 1: 入口点与主控编排器 (Entry Point & Master Orchestrator)

### 1.1 `main.go` - 应用程序启动器
- **模块名称**：Application Main Entry Point
- **系统角色**：负责 Agent 的引导与启动、监听 OS 关机信号（`SIGINT`, `SIGTERM`），并触发优雅关机程序。

### 1.2 `app.go` - 主控制引擎容器
- **详细技术手册**：**[📖 `app.go` 技术手册 (`docs/zh_cn/APP_CONTAINER.md`)](APP_CONTAINER.md)**
- **模块名称**：App Central Application Container
- **系统角色**：核心依赖注入容器，持有 Configuration、TUI、SysMonitor、P2P Network 与 Runner 5 大子系统。

---

## ⚙️ Layer 2: 配置管理与网卡自动检测 (Configuration & Auto-Detection)

### 2.1 `config.go` - 配置解析器
- **详细技术手册**：**[⚙️ 配置管理与参数手册 (`docs/zh_cn/CONFIG.md`)](CONFIG.md)**
- **模块名称**：Configuration & System Auto-Detection
- **系统角色**：解析 `config.json`，自动剥离单行 `//` 注释，动态检测实体网卡。

---

## 🔀 Layer 3: OpenAI API 网关与本地代理 (Gateway & Proxy Dispatcher)

### 3.1 `proxy.go` - 本地优先代理分发器
- **详细技术手册**：**[🔀 Gateway Proxy 手册 (`docs/zh_cn/GATEWAY_PROXY.md`)](GATEWAY_PROXY.md)**
- **模块名称**：OpenAI API Gateway & Disaggregated Scheduler
- **系统角色**：于 `50006` 端口提供兼容 OpenAI 格式的 API 网关。支持 `atomic.Bool` vLLM 健康预热检查、透明 0ms SSE 流式代理。
