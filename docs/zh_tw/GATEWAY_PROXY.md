# OpenAI API 網關與 Local-First 代理分發器手冊

本文件詳細說明 [`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go) 的核心運作機制與 OpenAI 網關。

---

## 🔀 代理特點

1. **vLLM 健康預熱檢查 (`startVLLMHealthChecker`)**：每 5 秒輪詢 `http://127.0.0.1:8100/health`，使用 `atomic.Bool` 管理就緒狀態。
2. **透明零延遲 SSE 串流 (`proxyToLocalVLLMDirect`)**：透過 `http.Flusher` 直通本機 vLLM，具備 0ms 網路額外開銷。
3. **Mode 1 與 Mode 2 分離推理調度器**：本機未就緒**或忙碌中**時，自動 Round-Robin 分發至 P2P Swarm 遠端 Peer 執行。
4. **併發感知本機名額 (`localBusy`)**：用 `atomic.Bool` + `CompareAndSwap` 搶佔本機執行名額，單一請求走本機快速路徑，但併發的第二筆請求會直接分派給遠端 Peer，而不是排隊等本機——判斷依據是「本機當下有沒有空」，不是只看「本機健不健康」。
