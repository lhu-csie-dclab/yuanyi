# 設定管理與參數技術手冊

本文件為 Yuanyi Client Agent 的設定管理提供詳細說明，涵蓋 [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)、[`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json)、[`.env.example`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/.env.example) 與 [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json)。

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
    "model_name": "cyankiwi/Qwen3-VL-4B-Instruct-AWQ-4bit",
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

`server_mode.enabled` 預設為 `false`，開啟後這個節點會兼任 Hub（合併原本獨立中央伺服器的能力：
節點/排行榜資料庫、GPU 算分、中央派發器、Hub 專屬儀表板）。完整欄位說明見
[`HUB_MODE.md`](HUB_MODE.md)。

---

## 📡 對外可達性（`announce_addr` / `behind_nat`）

這兩個選項是用來告訴節點「外界能不能連到我」。**一般節點兩個都不用設**，留空即可自動偵測。
只有在節點對自己網路環境的判斷會出錯時才需要——在 Docker 裡這是常態。

### 自架中繼節點：`announce_addr` 是必填

> [!IMPORTANT]
> 跑在 Docker（或經過 port forwarding）的中繼節點，**沒設這個就會安靜地失去中繼功能**，
> 但其他地方看起來都完全正常，非常難察覺。

libp2p 只有在「認為自己對外可達」時才會啟動 Circuit Relay 服務。容器裡的中繼節點只看得到
容器內部位址（`172.17.x`、`172.18.x`），自我探測必然失敗，於是判定自己是私有位址而默默不提供
中繼服務——即使它其實從外網完全連得到。依賴它的節點就會收到：

```
error opening hop stream to relay: protocols not supported: [/libp2p/circuit/relay/0.2.0/hop]
```

把 `announce_addr` 設成該節點**實際**可達的位址即可同時解決兩件事：這個位址會被廣播給其他
節點，而且可達性被明確宣告，中繼服務才會真正啟動：

```json
"p2p": {
  "server_address": "/dns4/relay.example.com/tcp/50004/p2p/12D3KooW...",
  "announce_addr": "/dns4/relay.example.com/tcp/50004"
}
```

注意 announce 位址**不帶** `/p2p/<peerID>` 後綴——它是一個位址，不是完整的節點參照。

### `behind_nat`：讓重啟後更快重新加入

這純粹是啟動速度的優化，不改變任何原本做得到或做不到的事。libp2p 必須先確定自己的可達性才會
去跟中繼要 reservation，而自行判斷需要數分鐘的探測——這段期間重啟過的節點在自己區網外是連不到
的。事先宣告可以把這段縮短到數秒。

建議留空。自動偵測會檢查本機介面是否有公網 IP；沒有就視為在 NAT 後面，這對幾乎所有家用機器都是
正確的。只有在要覆寫這個判斷時才明確設定。它與 `announce_addr` 互斥（兩者的主張相反），同時設定
會在啟動時直接報錯。

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
