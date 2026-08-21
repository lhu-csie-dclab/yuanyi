# 多节点全新 Clone 部署与并发多卡测试（简体中文）

本文档记录一次端到端验证：模拟一个完全陌生的开源用户，从 GitHub clone 这个 repo、部署到多台
独立机器上，并确认 (a) 推理请求真的是由 Swarm 中不同的物理 GPU 各自处理，以及 (b) 单一入口节点
在收到并发请求时，真的会自动分派给其他节点，而不是全部排在自己的 GPU 队列里。

> [!NOTE]
> 以下结果是在 **2026-08-21** 重新跑的，commit 为 `6b1b4e2`（`main`）——比上一版文档记录的
> `bdfc4d7` 多合并了 3 个 PR：Vue 3 + Vite + Tailwind 仪表板重写（#7）、把 Go 工具链钉死在
> `1.26.6` 以修复 CVE-2026-39822/CVE-2026-42505（#8），以及堵住 mooncake-proxy 隧道与 vLLM
> 启动命令的两个远程代码执行漏洞、外加 docker-compose 容器加固（#9）。这次重新完整跑一遍跟上次
> 一样的全新 clone 与多 GPU 分派测试方法，而不是假设旧结果在新代码上依然成立。

（完整英文版：[`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md)）

单节点在持续高负载下的吞吐量/延迟数据请见 [`BENCHMARK_RESULTS.md`](BENCHMARK_RESULTS.md)。本文档
着重的是**多节点、多 GPU 的正确性**：全新 `git clone` 是否真的能跑起来、Swarm 是否真的用到了它
宣称拥有的每一张物理 GPU，以及打单一节点的 gateway 是否真的会把并发负载分散出去，而不是全部堆在
一张卡上。

---

## 🖥️ 测试环境

| 项目 | 内容 |
| :--- | :--- |
| 测试日期 | 2026-08-21 |
| 节点数量 | 2 台独立物理主机，共 10 个独立容器（每台主机 5 个） |
| 每节点 GPU | 1x NVIDIA **Quadro RTX 4000（8GB VRAM）**，PCIe passthrough 直通给每个容器 |
| 容器运行环境 | LXC（非特权容器），Docker **29.6.1**，Docker Compose **v5.3.1** |
| 推理基础镜像 | `nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13`（CUDA 13） |
| Go 工具链（容器内构建） | `go1.26.6`（通过 `go.mod` 的 `toolchain` 指令钉死版本，PR #8） |
| 前端构建（容器内） | Node 22 + Vite，构建 Vue 3 + Tailwind 仪表板（PR #7） |
| 测试模型 | `Qwen3-4B-AWQ`（4-bit AWQ 量化） |
| Repository commit | `6b1b4e2`（`main`） |
| 网络模式 | Docker `network_mode: host`，10 个节点全部加入同一个私有 libp2p Swarm（共用 `swarm.key`） |

> [!NOTE]
> 每个节点都使用这个 repo `main` 分支上**完全未修改**的原始 `Dockerfile` / `docker-compose.yml`，
> `server_mode.enabled` 保持默认值（`false`）——也就是纯 client 模式，没有改动任何代码。

---

## 🧪 测试方法

1. 在 10 个节点上分别把这个 repo 全新 clone 到干净目录（`/root/0821`，以今天测试日期命名，
   跟上一轮测试的 `/root/0820-2` 部署分开，不共用同一份目录）。
2. 只填入一个真实新加入者会需要的东西（`swarm.key` 沿用既有网络、`.env` 只填
   `ABS_MODEL_PATH`/`SERVER_ADDRESS`/`IFACE`/`CLIENT_WEB_PORT`），`identity.key`/`stats.json`/
   `peers.db` 全部留给程序自己在第一次启动时生成（确认：10 个节点全部拿到全新的 PeerID，跟
   上一轮部署的不一样）。
3. 用原始的 `docker-compose.yml` 构建并启动。这是 PR #7/#8 新增的 3 阶段 `Dockerfile`
   （Node/Vite 仪表板构建 → Go 1.26.6 构建 → CUDA runtime）第一次在这批真实 GPU 硬件上
   完整跑过，不只是在 CI 上跑。
4. 先把上一轮部署（`/root/0820-2` 内的 `docker compose down`）关掉，再启动新的部署，确保同一
   时间只有一个容器占用每个节点的 GPU。
5. 通过 `/api/peers`、`/api/node_info` 验证 P2P 网格成形，`/health`（8100 端口）确认 vLLM 就绪。
6. 执行三种请求模式，同时比对各节点自己的 `vllm:request_success_total`（vLLM 自己的
   Prometheus 执行计数器，8100 端口 `/metrics`）：
   - **逐一测试**：依次对每个节点自己的 gateway 各发一次请求。
   - **10 台全部并发**：10 个节点的 gateway 同时被打。
   - **单一入口并发**：多笔并发请求只发给**其中一个**节点的 gateway，验证它会不会自己
     把负载分出去。

### 请求参数

```json
{
  "model": "Qwen3-4B-AWQ",
  "messages": [{"role": "user", "content": "Say hello in one short sentence."}],
  "max_tokens": 150,
  "temperature": 0.7
}
```
并发测试把 `max_tokens` 提高到 180，方便量测有更长的生成窗口。

---

## 📊 测试结果

### 1. 部署与网格成形（全部 10 个节点）

| 检查项目 | 结果 |
| :--- | :--- |
| `git clone` 到 commit `6b1b4e2` | ✅ 10/10 |
| `docker compose up -d --build` 成功（新的 3 阶段 Dockerfile，冷启动构建） | ✅ 10/10 |
| Gateway `/health` → `200` | ✅ 10/10 |
| vLLM `/health`（8100 端口）预热后 → `200` | ✅ 10/10 |
| P2P Peer 发现 | ✅ 10/10，每个节点都看到其余全部 9 个节点（全连通网格） |
| 生成全新 PeerID（新的 `identity.key`，不是沿用上一轮的） | ✅ 10/10 |

### 2. 逐一测试 + 10 台全部并发（每台各自处理自己的请求）

| 模式 | 请求数 | 成功 | 响应时间 |
| :--- | :---: | :---: | :---: |
| 逐一测试（一次一台） | 10 | 10/10（`HTTP 200`） | 2.17s – 2.33s |
| 10 台全部并发 | 10 | 10/10（`HTTP 200`） | 1.89s – 1.97s |

10 张不同的物理 GPU 各自独立服务了自己被直接打的请求，确认是 10 个节点在跑，不是同一张卡默默
吃下全部流量。

### 3. 单一入口并发分派测试

9 笔并发请求**只发给一个节点的 gateway**——完全没有直接打其他任何节点的端点。这正是在验证
PR #4 的并发感知 dispatcher（判断"要不要走本机"看的是本机**当下有没有空**——`localBusy`，
`atomic.Bool` + `CompareAndSwap`——而不是只看本机健不健康），这次是在 PR #7–#9 之后的代码上
重新验证一次。

**从目标节点自己的 `/api/logs` 抓到的实时 dispatcher 决策日志：**
```
[PROXY] 本地 vLLM 處理 (模型: Qwen3-4B-AWQ)                                x1
[PROXY] 本地 vLLM 忙碌中 (模型: Qwen3-4B-AWQ)，分派至 P2P 遠端節點...          x8
[PROXY] P2P 備援轉發 Qwen3-4B-AWQ -> 遠端節點: 12D3KooW... (嘗試 1/3)         x8
```

| 结果 | 数值 |
| :--- | :--- |
| 发出的请求数（全部打同一个节点） | 9 |
| 成功响应（`HTTP 200`） | 9/9 |
| 响应时间范围 | 1.91s – 1.98s（9 笔请求彼此只差约 70 毫秒，不是单一请求延迟的 9 倍） |
| 留在本机（依 dispatcher 日志） | 1 |
| 分派到 P2P 远端节点（依 dispatcher 日志） | 8 |

**用各节点自己的 vLLM 执行计数器（`vllm:request_success_total`）独立佐证**（此计数器是容器启动
以来的累计值；第 1、2 节测完后，全部 10 个节点的基准值都是 **2**，所以这一节任何超过基准值的
增量都只会来自这 9 笔单一入口请求）：

| 节点 | 测试前计数 | 测试后计数 | 差值 |
| :--- | :---: | :---: | :---: |
| 目标节点（101） | 2 | 3 | +1（本机执行） |
| 其余 8 个节点 | 各 2 | 各 3 | 各 +1 |
| 其余 1 个节点 | 2 | 2 | +0（这轮没被选中） |

**9 笔请求全部对得上**——1 笔本机执行，加上 8 个远端节点各自刚好执行 1 笔被分派的请求，没有任何
一笔排在同一张 GPU 上重复执行，也没有任何一笔对不上账。这次的结果比上一轮更干净（上一轮有 2 笔
请求落在同一张远端 GPU 上）：这次 9 笔请求分散到 9 张不同的物理 GPU（目标节点 + 8 个远端），
每张卡刚好各执行 1 笔，完全均匀分布。

如果请求是排在同一张 GPU 后面依次执行，每多排一个位置大约要多等一次完整生成的时间（约 2 秒）；
9 笔请求能在彼此相差不到 70 毫秒的时间内全部完成，只有真的分散到多张 GPU 并行执行才做得到，不可能
是在同一张卡上排队处理。

---

## ✅ 结论

一个在 `6b1b4e2` 完全全新 `git clone` 下来的 repo——现在已经包含 PR #7–#9 的 Vue 3 仪表板重写、
钉死版本的 Go 1.26.6 工具链、以及 mooncake-proxy/docker-compose 安全加固——只填入真实新参与者
会拿到的信息（Bootstrap 地址 + 共用私网密钥），通过新的 3 阶段 Dockerfile 就能正确构建、用原始
`docker-compose.yml` 正确运行、加入既有的 P2P 网格，并用自己本机的 GPU 提供推理。在 2 台独立
物理主机上分别部署的 10 个节点中，每个节点的 GPU 都被独立确认为真的在运作，并正确服务真实的
对话请求。单一节点的 gateway 依然能正确识别"本机 GPU 目前是否已经在忙"，并自动把并发超载的
部分分派给 Swarm 中的其他节点——这一点同时通过 dispatcher 自己的决策日志，以及接收节点 vLLM 的
独立执行计数器得到验证，这次的 9 笔并发请求呈现完全均匀的"每张 GPU 各执行 1 笔"分布。
