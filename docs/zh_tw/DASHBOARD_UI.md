# 終端 TUI 面板與 Web 監控儀表板使用手冊

本文件說明 [`tui.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/tui.go) 終端面板與 [`web.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/web.go) Web Dashboard。

---

## 🖥️ 終端 TUI 面板功能

- **4 分頁切換**：`Dashboard`、`System Logs`、`vLLM Console`、`Docker Logs`。
- **熱鍵**：`Q` 關機存檔, `A` 日誌自動捲動切換, `1-4` 分頁跳轉。
- **Headless 背景模式**：無 TTY 終端環境時自動切換至背景執行。

---

## 🌐 Web 儀表板 (`50007` 埠)

- **技術棧**：Vue 3 + Vite + Tailwind CSS，原始碼在 [`web-ui/`](../../web-ui)，用 hash 路由（`/#/...`）切 SPA 頁面，不需要伺服器端的 SPA fallback。
- **Go `embed.FS`**：`web.go` 用 `//go:embed web-ui/dist` 把 `npm run build` 的產出直接編譯進二進位檔；非 Docker 建置前必須先在 `web-ui/` 跑過一次 `npm ci && npm run build`。
- **API 端點**：`/api/peers`, `/api/stats`, `/api/logs`, `/api/config`；Hub 模式開啟時另外掛載 `/hub/api/*`。
