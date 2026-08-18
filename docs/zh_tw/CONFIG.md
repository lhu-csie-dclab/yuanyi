# 設定管理與參數技術手冊

本文件為 Mooncake 2.0 Client Agent 的設定管理提供詳細說明，涵蓋 [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)、[`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json)、[`.env.example`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/.env.example) 與 [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json)。

---

> [!WARNING]
> **正式環境部署警語與未測試參數聲明**：
> 本軟體目前處於**實驗研究階段**，**不推薦在正式生產環境 (Production) 部署**。目前僅基準設定（`Qwen3-4B-AWQ`, `protocol: "tcp"`）經測試驗證，其餘未經測試的參數可能產生不可預期的行為。

---

## ⚙️ `config.json` 預設範例

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
  }
}
```

---

## 🍰 `mooncake.json` 傳輸協定設定 (`protocol: "tcp"`)

```json
{
  "metadata_server": "P2PHANDSHAKE",
  "global_segment_size": "0",
  "local_buffer_size": "17179869184",
  "protocol": "tcp",
  "device_name": ""
}
```

### 說明：
- **`"protocol": "tcp"`**：強制採用標準 TCP Socket 進行跨節點 KV Cache 傳輸（相容標準乙太網路）。
- **`"local_buffer_size": "17179869184"`**：分配 16 GB 本機 RAM 緩衝區作為 KV 快取暫存空間。
