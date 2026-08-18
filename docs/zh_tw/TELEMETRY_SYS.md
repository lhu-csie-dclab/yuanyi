# 系統遙測、顯卡指標與數據存檔手冊

本文件詳細說明 [`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go) 的硬體遙測與 [`stats.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/stats.json) 數據持久化機制。

---

## 📊 遙測指標內容

1. **vLLM Prometheus Metrics 爬蟲**：每 2 秒解析 `http://127.0.0.1:8100/metrics` 計算 Prefill 與 Generation Token 速率。
2. **NVML 顯卡遙測 (`nvidia-smi`)**：查詢 GPU 核心溫度 (℃)、使用率 (%)、VRAM 使用量與功耗 (W)。
3. **`stats.json` 持久化**：每 5 秒將累計請求數與成功率存檔至硬碟。
