# User Interfaces: Terminal TUI & Web Monitoring Dashboard

This document details the dual monitoring user interfaces implemented in [`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go) (Terminal Console) and [`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go) (Web UI Dashboard).

---

## 🖥️ Terminal UI (`tui.go`) Usage Guide

The Terminal User Interface (TUI) is built using `tview` and `tcell`. It provides a multi-tab terminal console with real-time statistics and ring-buffer log views.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Mooncake 2.0 P2P Client Agent                                              │
│ [1] Dashboard  │  [2] System Logs  │  [3] vLLM Console  │  [4] Docker Logs │
├───────────────────────────────┬─────────────────────────────────────────────┤
│ Node Statistics               │ Connected Peers (P2P Swarm Table)           │
│ - Uptime: 01:23:45            │ ┌──────────┬─────────────┬───────────┬────┐ │
│ - CPU Util: 12.4%             │ │ Node ID  │ GPU Model   │ KV Cache  │Req │ │
│ - RAM RSS: 854 MB             │ ├──────────┼─────────────┼───────────┼────┤ │
│ - GPU: RTX 4090 (42℃, 35%)   │ │ 12D3KooW │ RTX 4090 x1 │ 14.5%     │ 2  │ │
│ - Total Requests: 1,542       │ └──────────┴─────────────┴───────────┴────┘ │
│ - Total Tokens: 485,210       │                                             │
├───────────────────────────────┴─────────────────────────────────────────────┤
│ Hotkeys: [Q] Quit | [A] AutoScroll Toggle | [1-4 / Tab] Switch Views        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Tab Overview

- **Tab 1: Dashboard**: Split-screen displaying Node Statistics (left) and connected P2P Swarm Peer Table (right).
- **Tab 2: System Logs**: Application lifecycle logs with colorized log levels (`[INFO]`, `[WARN]`, `[ERROR]`, `[GOSSIP]`).
- **Tab 3: vLLM Console**: Real-time stdout/stderr output from the local vLLM engine process.
- **Tab 4: Docker Logs**: Container output logs from Docker CLI.

### Interactive Hotkeys

| Hotkey | Action |
| :--- | :--- |
| **`Q` / `q`** | Save statistics to `stats.json` and exit application cleanly. |
| **`A` / `a`** | Toggle auto-scrolling on log text views. |
| **`1` - `4`** | Jump directly to Tab 1, Tab 2, Tab 3, or Tab 4. |
| **`Tab` / `Shift+Tab`** | Switch to next / previous tab. |

### Headless Fallback Mode

If the application is launched in a headless Docker container or environment without a TTY terminal (`os.Stdout` is not a terminal), `TUI.Run()` automatically detects non-interactive mode and logs:

```text
[System] Headless environment detected (no TTY). Running in background mode...
```

The application continues running in the background without UI errors, maintaining API endpoints (`50006` and `50007`).

---

## 🌐 Web Monitoring Dashboard (`web.go`) Usage Guide

`web.go` hosts a Web Monitoring Dashboard on port `50007` (`web_port`).

### Single-Binary Static Embedding (`embed.FS`)

The frontend HTML/CSS/JS assets inside `web/index.html` are compiled directly into the Go binary using Go 1.16+ `embed.FS`, eliminating external static file dependencies.

```go
//go:embed web/*
var webFS embed.FS
```

### Accessing the Web Console

Open your web browser and navigate to:
```text
http://localhost:50007/
```

### RESTful API Reference

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| **`GET /`** | `GET` | Serves the embedded Web Dashboard console HTML interface. |
| **`GET /api/peers`** | `GET` | Returns JSON list of all active P2P swarm peers and their GPU telemetry. |
| **`GET /api/node_info`** | `GET` | Returns local PeerID and central server multiaddress. |
| **`GET /api/local_stats`** | `GET` | Returns local processed request count, token statistics, and leaderboard rank. |
| **`GET /api/stats`** | `GET` | Calculates cluster-wide aggregate throughput, average TTFT, and average KV cache usage. |
| **`GET /api/logs`** | `GET` | Returns recent system, vLLM, and Docker log lines. |
| **`GET /api/config`** | `GET` | Reads current `config.json` contents. |
| **`POST /api/config`** | `POST` | Updates `config.json` dynamically and creates timestamped backups in `backups/`. |
| **`GET /api/config/backups`** | `GET` | Lists all configuration backups stored in `backups/`. |
| **`POST /api/config/restore`** | `POST` | Restores a selected configuration backup file. |
