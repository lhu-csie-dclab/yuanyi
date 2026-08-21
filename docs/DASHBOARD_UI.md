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

## 🌐 Web Monitoring Dashboard (`web.go` + `web-ui/`) Usage Guide

`web.go` hosts a Web Monitoring Dashboard on port `50007` (`web_port`). The frontend itself is
a **Vue 3 + Vite + Tailwind CSS single-page application** living in [`web-ui/`](../web-ui),
separate from the Go source tree.

### Frontend Stack & Build

| Piece | Choice |
| :--- | :--- |
| Framework | Vue 3 (`<script setup>` SFCs) |
| Build tool | Vite |
| Styling | Tailwind CSS v4 (CSS-first `@theme` tokens, no `tailwind.config.js`) |
| Routing | `vue-router`, **hash mode** (`/#/...`) |

Hash-based routing is a deliberate choice: every route lives entirely in the URL fragment,
which the browser never sends to the server, so the Go side needs **no SPA-fallback routing
logic** — it just serves one static bundle at `/` exactly like any other embedded asset, and
`/api/*` / `/hub/api/*` stay ordinary JSON endpoints untouched by client-side routing.

```
web-ui/
├── src/
│   ├── App.vue              # sidebar shell + <router-view>
│   ├── router.js             # hash-mode routes (client pages + /hub/* pages)
│   ├── api.js                 # fetch wrappers, one per REST endpoint below
│   ├── composables/           # useNodeInfo (hub-mode flag), usePolling, useToast
│   ├── components/            # StatCard, StatusPill, TelemetryBadges, PageHeader, Toast
│   └── views/
│       ├── client/            # Topology, Logs, Settings, API info
│       └── hub/                # Active Topology, History, Leaderboard (hub mode only)
└── dist/                       # npm run build output -- embedded by web.go, not committed
```

### Single-Binary Static Embedding (`embed.FS`)

`npm run build` compiles `web-ui/` into `web-ui/dist/`, which Go embeds directly into the
binary with `embed.FS` — the running server still needs no external static file directory.
`web-ui/dist/` is gitignored and rebuilt fresh by the Dockerfile's Node build stage (or by CI,
or manually via `npm run build` before a non-Docker `go build`); it is never committed.

```go
//go:embed web-ui/dist
var webFS embed.FS
```

> [!NOTE]
> Building **outside Docker** (`go build .` directly) requires `web-ui/dist/` to already exist,
> since the `//go:embed` directive is resolved at compile time. Run `npm ci && npm run build`
> inside `web-ui/` once before `go build` if you're not using the Dockerfile.

### Accessing the Web Console

Open your web browser and navigate to:
```text
http://localhost:50007/
```
The "Cluster (Hub Mode)" navigation section (Active Topology / History / Leaderboard) appears
automatically once `/api/node_info` reports `hub_mode_enabled: true` -- see
[`HUB_MODE.md`](HUB_MODE.md).

### RESTful API Reference

Client-side endpoints (`web.go`, always available):

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| **`GET /`** | `GET` | Serves the embedded Vue SPA (`index.html` + hashed JS/CSS bundles). |
| **`GET /api/peers`** | `GET` | Returns JSON list of all active P2P swarm peers and their GPU telemetry. |
| **`GET /api/node_info`** | `GET` | Returns local PeerID, bootstrap host, and `hub_mode_enabled`. |
| **`GET /api/local_stats`** | `GET` | Returns local processed request count, token statistics, and leaderboard rank. |
| **`GET /api/stats`** | `GET` | Calculates cluster-wide aggregate throughput, average TTFT, and average KV cache usage. |
| **`GET /api/logs`** | `GET` | Returns recent system, vLLM, and Docker log lines. |
| **`GET /api/config`** | `GET` | Reads current `config.json` contents. |
| **`POST /api/config`** | `POST` | Updates `config.json` dynamically and creates timestamped backups in `backups/`. |
| **`GET /api/config/backups`** | `GET` | Lists all configuration backups stored in `backups/`. |
| **`POST /api/config/restore`** | `POST` | Restores a selected configuration backup file. |

Hub endpoints (`server_web.go`'s `RegisterHubRoutes`, only registered when
`server_mode.enabled`) — see [`HUB_MODE.md`](HUB_MODE.md) for the full reference.
