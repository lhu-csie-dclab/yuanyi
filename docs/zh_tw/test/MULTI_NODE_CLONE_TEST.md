# 多節點全新 Clone 部署與併發多卡測試（繁體中文）

本文件記錄一次端到端驗證：模擬一個完全陌生的開源使用者，從 GitHub clone 這個 repo、部署到多台
獨立機器上，並確認推論請求真的是由 Swarm 裡不同的實體 GPU 各自處理——而不是同一張卡假裝成多個
節點。

單節點在持續高負載下的吞吐量/延遲數據請見 [`BENCHMARK_RESULTS.md`](BENCHMARK_RESULTS.md)
（NVIDIA AIPerf，1 萬次請求）。本文件著重的是**多節點、多 GPU 的正確性**：全新 `git clone` 是否
真的能跑起來，以及 Swarm 是否真的用到了它宣稱擁有的每一張實體 GPU。

（完整英文版：[`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md)）

---

## 🖥️ 測試環境

| 項目 | 內容 |
| :--- | :--- |
| 節點數量 | 2 台獨立實體主機，共 10 個獨立容器（每台主機 5 個） |
| 每節點 GPU | 1x NVIDIA **Quadro RTX 4000（8GB VRAM）**，PCIe passthrough 直通給每個容器 |
| 容器執行環境 | LXC（非特權容器），Docker **29.6.1**，Docker Compose **v5.3.1** |
| 推論基礎映像 | `nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13`（CUDA 13） |
| Go 工具鏈（容器內建置） | `go1.26`（Alpine builder stage） |
| 測試模型 | `Qwen3-4B-AWQ`（4-bit AWQ 量化） |
| 網路模式 | Docker `network_mode: host`，10 個節點全部加入同一個私有 libp2p Swarm（共用 `swarm.key`） |

> [!NOTE]
> 每個節點都使用這個 repo `main` 分支上**完全未修改**的原始 `Dockerfile` / `docker-compose.yml`，
> `server_mode.enabled` 維持預設值（`false`）——也就是純 client 模式，沒有動任何程式碼。

---

## 🧪 測試方法

1. 在 10 個節點上分別把這個 repo 全新 clone 到乾淨目錄：
   ```bash
   git clone https://github.com/lhu-csie-dclab/yuanyi.git
   ```
2. 只填入一個真實新加入者會需要的東西，依照 [`P2P_NETWORK.md`](../P2P_NETWORK.md) 與
   `.env.example`：
   - `swarm.key`——沿用既有 Swarm 的私有網路金鑰（依文件說明，這是要私下分發給參與節點的，
     **不會**進版控）。
   - `.env`——`ABS_MODEL_PATH`（指向本機已有的模型權重）、`SERVER_ADDRESS`（既有的 Bootstrap
     multiaddress）、`IFACE`、`CLIENT_WEB_PORT`，對應 `.env.example` 的欄位。
   - `identity.key`、`stats.json`、`peers.db`——留空/不建立，完全交給程式自己在第一次啟動時
     產生，就跟真正第一次使用的人一樣。
3. 用原始的 compose 檔建置並啟動：
   ```bash
   docker compose up -d --build
   ```
4. 透過每個節點的 `/api/peers`、`/api/node_info` 驗證 P2P 網格是否成形，並用 `/health`（8100 埠）
   確認 vLLM 是否就緒。
5. 對每個節點自己的網關（50006 埠）發送真實的 `/v1/chat/completions` 請求——先逐一測試，再 10
   台同時測試——同時在每個節點上取樣 `nvidia-smi`。

### 請求參數

```json
{
  "model": "Qwen3-4B-AWQ",
  "messages": [{"role": "user", "content": "<每次測試提示詞不同>"}],
  "max_tokens": 150,
  "temperature": 0.7
}
```
使用的提示詞：*「Write a 100 word story about the ocean.」*（逐一測試）與
*「Explain photosynthesis in detail.」/「Write a detailed 200 word essay about deep learning.」*
（併發測試，`max_tokens` 拉高到 200-250 以延長生成視窗方便取樣）。

---

## 📊 測試結果

### 1. 部署與網格成形（全部 10 個節點）

| 檢查項目 | 結果 |
| :--- | :--- |
| `git clone` 到最新 `main` commit | ✅ 10/10 |
| `docker compose up -d --build` 成功 | ✅ 10/10 |
| Gateway `/health` → `200` | ✅ 10/10 |
| vLLM `/health`（8100 埠）暖機後 → `200` | ✅ 10/10（其中 2 台多花約 60 秒暖機才轉綠） |
| P2P Peer 探索 | ✅ 10/10，每個節點各自看到 9-10 個 Peer（全連通網格） |

### 2. 逐一節點請求 + GPU 使用率

依序對每個節點自己的網關送一次請求，在生成開始約 1.5 秒時取樣 `nvidia-smi`：

| 節點 | GPU 使用率（生成中） | VRAM 用量 | HTTP | 延遲 |
| :--- | :---: | :---: | :---: | :---: |
| 1-10 | **每一台皆為 100%** | 約 6,320 MiB / 8,192 MiB | 200 | 2.47s - 2.61s |

10 個節點都各自獨立跑到 100% GPU 使用率——確認是 10 張不同的實體 GPU 在服務，不是同一張卡在
應付全部流量。

### 3. 10 台併發請求測試

10 個節點的網關同時被打（`curl` 在兩台主機上平行發送），在重疊的生成視窗期間取樣 4 次
`nvidia-smi`（間隔約 0.8 秒）：

| 結果 | 數值 |
| :--- | :--- |
| 同時發出的請求數 | 10 |
| 成功回應（`HTTP 200`）| 10/10 |
| 回應時間範圍 | 2.09s – 2.71s |
| 同一瞬間觀察到多張卡同時 100% | 每台主機都有捕捉到 2 個以上節點在同一張快照裡同時 100%（取樣間隔受限於每次呼叫容器 exec 本身的開銷，不是 GPU 本身的限制） |

一個 4B 參數模型若退回 CPU 執行，單次請求通常需要數十秒到數分鐘；10 個並發請求都能穩定在
2.1-2.7 秒內完成，只有真正的 GPU 加速推論才可能做到。

---

## ✅ 結論

一個完全全新 `git clone` 下來的 repo，只填入真實新參與者會拿到的資訊（Bootstrap 位址 + 共用私網
金鑰），透過原始 `docker-compose.yml` 就能正確建置與運行、加入既有的 P2P 網格，並用自己本機的
GPU 提供推論。在 2 台獨立實體主機上分別部署的 10 個節點中，每個節點的 GPU 都被獨立確認為真的在
運作（100% 使用率），並正確服務真實的對話請求——不論是逐一測試還是 10 台同時併發測試皆是如此。
