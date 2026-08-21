# 多節點全新 Clone 部署與併發多卡測試（繁體中文）

本文件記錄一次端到端驗證：模擬一個完全陌生的開源使用者，從 GitHub clone 這個 repo、部署到多台
獨立機器上，並確認 (a) 推論請求真的是由 Swarm 裡不同的實體 GPU 各自處理，以及 (b) 單一入口節點
在收到併發請求時，真的會自動分派給其他節點，而不是全部排在自己的 GPU 隊列裡。

> [!NOTE]
> 以下結果是在 **2026-08-21** 重新跑的，commit 為 `6b1b4e2`（`main`）——比上一版文件記錄的
> `bdfc4d7` 多合併了 3 個 PR：Vue 3 + Vite + Tailwind 儀表板重寫（#7）、把 Go 工具鏈釘死在
> `1.26.6` 以修復 CVE-2026-39822/CVE-2026-42505（#8），以及堵住 mooncake-proxy 隧道與 vLLM
> 啟動指令的兩個遠端程式碼執行漏洞、外加 docker-compose 容器加固（#9）。這次重新完整跑一遍跟上次
> 一樣的全新 clone 與多 GPU 分派測試方法，而不是假設舊結果在新程式碼上依然成立。

（完整英文版：[`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md)）

單節點在持續高負載下的吞吐量/延遲數據請見 [`BENCHMARK_RESULTS.md`](BENCHMARK_RESULTS.md)。本文件
著重的是**多節點、多 GPU 的正確性**：全新 `git clone` 是否真的能跑起來、Swarm 是否真的用到了它
宣稱擁有的每一張實體 GPU，以及打單一節點的 gateway 是否真的會把併發負載分散出去，而不是全部堆在
一張卡上。

---

## 🖥️ 測試環境

| 項目 | 內容 |
| :--- | :--- |
| 測試日期 | 2026-08-21 |
| 節點數量 | 2 台獨立實體主機，共 10 個獨立容器（每台主機 5 個） |
| 每節點 GPU | 1x NVIDIA **Quadro RTX 4000（8GB VRAM）**，PCIe passthrough 直通給每個容器 |
| 容器執行環境 | LXC（非特權容器），Docker **29.6.1**，Docker Compose **v5.3.1** |
| 推論基礎映像 | `nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13`（CUDA 13） |
| Go 工具鏈（容器內建置） | `go1.26.6`（透過 `go.mod` 的 `toolchain` 指令釘死版本，PR #8） |
| 前端建置（容器內） | Node 22 + Vite，建置 Vue 3 + Tailwind 儀表板（PR #7） |
| 測試模型 | `Qwen3-4B-AWQ`（4-bit AWQ 量化） |
| Repository commit | `6b1b4e2`（`main`） |
| 網路模式 | Docker `network_mode: host`，10 個節點全部加入同一個私有 libp2p Swarm（共用 `swarm.key`） |

> [!NOTE]
> 每個節點都使用這個 repo `main` 分支上**完全未修改**的原始 `Dockerfile` / `docker-compose.yml`，
> `server_mode.enabled` 維持預設值（`false`）——也就是純 client 模式，沒有動任何程式碼。

---

## 🧪 測試方法

1. 在 10 個節點上分別把這個 repo 全新 clone 到乾淨目錄（`/root/0821`，以今天測試日期命名，
   跟上一輪測試的 `/root/0820-2` 部署分開，不共用同一份目錄）。
2. 只填入一個真實新加入者會需要的東西（`swarm.key` 沿用既有網路、`.env` 只填
   `ABS_MODEL_PATH`/`SERVER_ADDRESS`/`IFACE`/`CLIENT_WEB_PORT`），`identity.key`/`stats.json`/
   `peers.db` 全部留給程式自己在第一次啟動時產生（確認：10 個節點全部拿到全新的 PeerID，跟
   上一輪部署的不一樣）。
3. 用原始的 `docker-compose.yml` 建置並啟動。這是 PR #7/#8 新增的 3 階段 `Dockerfile`
   （Node/Vite 儀表板建置 → Go 1.26.6 建置 → CUDA runtime）第一次在這批真實 GPU 硬體上
   完整跑過，不只是在 CI 上跑。
4. 先把上一輪部署（`/root/0820-2` 內的 `docker compose down`）關掉，再啟動新的部署，確保同一
   時間只有一個容器占用每個節點的 GPU。
5. 透過 `/api/peers`、`/api/node_info` 驗證 P2P 網格成形，`/health`（8100 埠）確認 vLLM 就緒。
6. 執行三種請求模式，同時比對各節點自己的 `vllm:request_success_total`（vLLM 自己的
   Prometheus 執行計數器，8100 埠 `/metrics`）：
   - **逐一測試**：依序對每個節點自己的 gateway 各送一次請求。
   - **10 台全部併發**：10 個節點的 gateway 同時被打。
   - **單一入口併發**：多筆併發請求只送給**其中一個**節點的 gateway，驗證它會不會自己
     把負載分出去。

### 請求參數

```json
{
  "model": "Qwen3-4B-AWQ",
  "messages": [{"role": "user", "content": "Say hello in one short sentence."}],
  "max_tokens": 150,
  "temperature": 0.7
}
```
併發測試把 `max_tokens` 拉高到 180，方便量測有更長的生成視窗。

---

## 📊 測試結果

### 1. 部署與網格成形（全部 10 個節點）

| 檢查項目 | 結果 |
| :--- | :--- |
| `git clone` 到 commit `6b1b4e2` | ✅ 10/10 |
| `docker compose up -d --build` 成功（新的 3 階段 Dockerfile，冷啟動建置） | ✅ 10/10 |
| Gateway `/health` → `200` | ✅ 10/10 |
| vLLM `/health`（8100 埠）暖機後 → `200` | ✅ 10/10 |
| P2P Peer 探索 | ✅ 10/10，每個節點都看到其餘全部 9 個節點（全連通網格） |
| 產生全新 PeerID（新的 `identity.key`，不是沿用上一輪的） | ✅ 10/10 |

### 2. 逐一測試 + 10 台全部併發（每台各自處理自己的請求）

| 模式 | 請求數 | 成功 | 回應時間 |
| :--- | :---: | :---: | :---: |
| 逐一測試（一次一台） | 10 | 10/10（`HTTP 200`） | 2.17s – 2.33s |
| 10 台全部併發 | 10 | 10/10（`HTTP 200`） | 1.89s – 1.97s |

10 張不同的實體 GPU 各自獨立服務了自己被直接打的請求，確認是 10 個節點在跑，不是同一張卡默默
吃下全部流量。

### 3. 單一入口併發分派測試

9 筆併發請求**只送給一個節點的 gateway**——完全沒有直接打其他任何節點的端點。這正是在驗證
PR #4 的併發感知 dispatcher（判斷「要不要走本機」看的是本機**當下有沒有空**——`localBusy`，
`atomic.Bool` + `CompareAndSwap`——而不是只看本機健不健康），這次是在 PR #7–#9 之後的程式碼上
重新驗證一次。

**從目標節點自己的 `/api/logs` 抓到的即時 dispatcher 決策日誌：**
```
[PROXY] 本地 vLLM 處理 (模型: Qwen3-4B-AWQ)                                x1
[PROXY] 本地 vLLM 忙碌中 (模型: Qwen3-4B-AWQ)，分派至 P2P 遠端節點...          x8
[PROXY] P2P 備援轉發 Qwen3-4B-AWQ -> 遠端節點: 12D3KooW... (嘗試 1/3)         x8
```

| 結果 | 數值 |
| :--- | :--- |
| 送出的請求數（全部打同一個節點） | 9 |
| 成功回應（`HTTP 200`） | 9/9 |
| 回應時間範圍 | 1.91s – 1.98s（9 筆請求彼此只差約 70 毫秒，不是單一請求延遲的 9 倍） |
| 留在本機（依 dispatcher 日誌） | 1 |
| 分派到 P2P 遠端節點（依 dispatcher 日誌） | 8 |

**用各節點自己的 vLLM 執行計數器（`vllm:request_success_total`）獨立佐證**（此計數器是容器啟動
以來的累計值；第 1、2 節測完後，全部 10 個節點的基準值都是 **2**，所以這一節任何超過基準值的
增量都只會來自這 9 筆單一入口請求）：

| 節點 | 測試前計數 | 測試後計數 | 差值 |
| :--- | :---: | :---: | :---: |
| 目標節點（101） | 2 | 3 | +1（本機執行） |
| 其餘 8 個節點 | 各 2 | 各 3 | 各 +1 |
| 其餘 1 個節點 | 2 | 2 | +0（這輪沒被選中） |

**9 筆請求全部對得上**——1 筆本機執行，加上 8 個遠端節點各自剛好執行 1 筆被分派的請求，沒有任何
一筆排在同一張 GPU 上重複執行，也沒有任何一筆對不上帳。這次的結果比上一輪更乾淨（上一輪有 2 筆
請求落在同一張遠端 GPU 上）：這次 9 筆請求分散到 9 張不同的實體 GPU（目標節點 + 8 個遠端），
每張卡剛好各執行 1 筆，完全均勻分佈。

如果請求是排在同一張 GPU 後面依序執行，每多排一個位置大約要多等一次完整生成的時間（約 2 秒）；
9 筆請求能在彼此相差不到 70 毫秒的時間內全部完成，只有真的分散到多張 GPU 平行執行才做得到，不可能
是在同一張卡上排隊處理。

---

## ✅ 結論

一個在 `6b1b4e2` 完全全新 `git clone` 下來的 repo——現在已經包含 PR #7–#9 的 Vue 3 儀表板重寫、
釘死版本的 Go 1.26.6 工具鏈、以及 mooncake-proxy/docker-compose 安全加固——只填入真實新參與者
會拿到的資訊（Bootstrap 位址 + 共用私網金鑰），透過新的 3 階段 Dockerfile 就能正確建置、用原始
`docker-compose.yml` 正確運行、加入既有的 P2P 網格，並用自己本機的 GPU 提供推論。在 2 台獨立
實體主機上分別部署的 10 個節點中，每個節點的 GPU 都被獨立確認為真的在運作，並正確服務真實的
對話請求。單一節點的 gateway 依然能正確辨識「本機 GPU 目前是否已經在忙」，並自動把併發超載的
部分分派給 Swarm 裡的其他節點——這一點同時透過 dispatcher 自己的決策日誌，以及接收節點 vLLM 的
獨立執行計數器得到驗證，這次的 9 筆併發請求呈現完全均勻的「每張 GPU 各執行 1 筆」分佈。
