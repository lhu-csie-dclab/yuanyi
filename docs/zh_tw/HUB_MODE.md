# Hub 模式手冊（選用的中央伺服器合併能力）

本文件說明**Hub 模式**——一項選用能力，把原本獨立的 Mooncake 2.0 Central Server 職責合併進
這個 client 執行檔本身。實作分散在
[`server_db.go`](../../server_db.go)、[`server_rank.go`](../../server_rank.go)、
[`server_p2p.go`](../../server_p2p.go)、[`server_proxy.go`](../../server_proxy.go)、
[`server_web.go`](../../server_web.go) + `web/hub/`、[`logger.go`](../../logger.go) 與
[`scanGPUlevel.go`](../../scanGPUlevel.go)。

（完整英文版：[`docs/HUB_MODE.md`](../HUB_MODE.md)）

---

> [!NOTE]
> Hub 模式**預設關閉**（`server_mode.enabled: false`）。除非你自己開啟，否則純 client 的行為、
> 設定檔結構與網路行為完全不受影響。

---

## 🧭 Hub 模式提供什麼

任何 client 節點都可以開啟 Hub 模式。開啟後，這個節點仍會做一般 client 該做的所有事情
（跑自己的 vLLM 推論、加入 P2P Swarm、提供自己的 OpenAI 網關），並額外承擔以前需要獨立
Central Server 程序才能做的事：

- 一份本機 SQLite 資料庫（`peers.db`），追蹤這個節點觀察到的每一個 Peer：位址、GPU 資訊、
  Ping 健康狀態、累計 Token/請求貢獻度。
- 基於 GPU 硬體的貢獻度算分，以及每 10 秒刷新一次的 `top.json` 排行榜。
- 一個中央 Prefill/Decode 派發器與 `/api/cluster_topology` 端點（只基於這個節點自己觀察到的
  Swarm 視圖），監聽在 `server_mode.proxy_port`。
- 一個 Hub 專屬的 Web 儀表板（排行榜、Peer 清單、稽核事件、拓撲），監聽在 `server_mode.web_port`，
  與既有的 client 儀表板（`web_port`）分開。
- Circuit Relay v2 中繼服務，並固定監聽在 `server_mode.p2p_port`——如果這個節點本身公網可達，
  NAT 後方的 Peer 就能透過它連進 Swarm。

## 🌐 多 Hub 設計：沒有單點故障

現在不再有單一、固定的「Central Server」。相反地，**任意數量的節點可以同時開啟 Hub 模式**。
這是刻意的設計，而且不需要任何 Hub 之間的資料複寫協定：

- 每個節點——不論是不是 Hub——本來就已經訂閱同一個全網廣播的 GossipSub topic
  （`/my-gpu-network/v1/updates`），會收到每個 Peer 定期發出的廣播。
- Hub 節點唯一多做的事，就是把收到的內容也寫進自己本機的 `peers.db`。因為每個 Hub 觀察到的
  都是同一個廣播串流，各自的資料庫會獨立收斂到相同視圖，通常一個 gossip 週期（約 3 秒）內就會
  一致。
- 若某個 Hub 離線，其餘 Hub 各自已經收斂好的視圖完全不受影響，照常對外服務。Client 只是去問
  自己設定要連的那個 Hub，沒有協調者需要故障轉移。

**取捨，講清楚**：這是**最終一致**，不是線性一致。兩個 Hub 可能短暫對某個 Peer 的
`fail_count`/`penalty_points` 有不同看法（因為各自獨立對外 Ping），一個全新的 Hub 在收到接下來
幾次 gossip 廣播前排行榜也會是空的。在節點數量很多的 Swarm 裡，這被判斷為可接受的取捨——換來的
是不需要為了本質上只是遙測與算分資料，去實作 Raft/CRDT 這類共識協定。

## 🌱 Bootstrap 種子 vs. 執行期依賴

`p2p.server_addresses`（複數）取代單一的 `p2p.server_address`，成為設定 Bootstrap 種子的建議
方式；單數欄位仍會被讀取作為向後相容 fallback。節點只需要成功連上清單中**任何一個**位址——
之後 Kademlia DHT 探索就會找到 Swarm 裡其餘所有節點，包含其他 Hub。這份種子清單只是給全新節點
的敲門磚，不是正在運作的 Swarm 之後需要依賴的東西。

Hub 節點也可以設定成空種子清單，這種情況下它就是其他節點要連的第一個/根種子。

## 🪪 穩定 PeerID（`identity.key`）

以前 client 建立 host 時沒有帶 `libp2p.Identity(...)`，所以每次重啟 PeerID 都會換掉。現在不論
是不是 Hub，每個節點都會讀取或生成一把持久化的 Ed25519 金鑰（`identity.key`），透過
`libp2p.Identity(...)` 傳入，讓 PeerID 跨重啟保持穩定。這對 Hub 節點特別重要，因為其他節點可能
會把自己的 `server_addresses` 長期指向某個特定的 Hub PeerID。

## ⚙️ 設定參考

```json
{
  "p2p": {
    "server_address": "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh",
    "server_addresses": []
  },
  "server_mode": {
    "enabled": false,
    "p2p_port": 50004,
    "web_port": 50005,
    "proxy_port": 50008,
    "database_path": "./peers.db",
    "max_fail_count": 3,
    "check_interval_sec": 30,
    "cluster": {
      "prefill_nodes": 0,
      "decode_nodes": 0
    }
  }
}
```

| 欄位 | 預設值 | 說明 |
| :--- | :--- | :--- |
| `p2p.server_addresses` | `[]` | Bootstrap/Hub 種子節點清單（建議寫法）。 |
| `server_mode.enabled` | `false` | 是否為這個節點開啟 Hub 模式。 |
| `server_mode.p2p_port` | `50004` | 固定 libp2p 監聽埠，供其他節點撥入。 |
| `server_mode.web_port` | `50005` | Hub 專屬儀表板 HTTP 埠。 |
| `server_mode.proxy_port` | `50008` | 中央 Prefill/Decode 派發器 HTTP 埠。 |
| `server_mode.database_path` | `./peers.db` | SQLite 資料庫檔案路徑。 |
| `server_mode.max_fail_count` | `3` | 連續 Ping 失敗幾次後標記該 Peer 離線。 |
| `server_mode.check_interval_sec` | `30` | 健康檢查 Ping 的輪詢間隔秒數。 |
| `server_mode.cluster.prefill_nodes` / `decode_nodes` | `0` / `0` | 專用 P/D 節點數量上限；兩者皆 0 代表 PD-Together 模式。 |

`LoadOrCreateConfig` 會防止 `server_mode.web_port`/`proxy_port`/`p2p_port` 跟這個節點自己的
`web_port`、`proxy_port`、`vllm.port`、`vllm.mooncake_bootstrap_port` 撞埠，衝突時會重設為上面
的預設值。

## 🚀 開啟 Hub 模式

在 `config.json` 把 `server_mode.enabled` 設成 `true`（若沒有這個區塊就照上面補上——
`LoadOrCreateConfig` 會自動幫其餘欄位填入預設值）。不需要其他改動；下次啟動時，節點會在
GPU 規格資料庫（`gpu_database.json`，跟以前 Central Server 用的是同一個來源）不存在時自動
下載，初始化 `peers.db`，並啟動上述的額外服務。
