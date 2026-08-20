# 多节点全新 Clone 部署与并发多卡测试（简体中文）

本文档记录一次端到端验证：模拟一个完全陌生的开源用户，从 GitHub clone 这个 repo、部署到多台
独立机器上，并确认推理请求真的是由 Swarm 中不同的物理 GPU 各自处理——而不是同一张卡假装成多个
节点。

单节点在持续高负载下的吞吐量/延迟数据请见 [`BENCHMARK_RESULTS.md`](BENCHMARK_RESULTS.md)
（NVIDIA AIPerf，1 万次请求）。本文档着重的是**多节点、多 GPU 的正确性**：全新 `git clone` 是否
真的能跑起来，以及 Swarm 是否真的用到了它宣称拥有的每一张物理 GPU。

（完整英文版：[`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md)）

---

## 🖥️ 测试环境

| 项目 | 内容 |
| :--- | :--- |
| 节点数量 | 2 台独立物理主机，共 10 个独立容器（每台主机 5 个） |
| 每节点 GPU | 1x NVIDIA **Quadro RTX 4000（8GB VRAM）**，PCIe passthrough 直通给每个容器 |
| 容器运行环境 | LXC（非特权容器），Docker **29.6.1**，Docker Compose **v5.3.1** |
| 推理基础镜像 | `nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13`（CUDA 13） |
| Go 工具链（容器内构建） | `go1.26`（Alpine builder stage） |
| 测试模型 | `Qwen3-4B-AWQ`（4-bit AWQ 量化） |
| 网络模式 | Docker `network_mode: host`，10 个节点全部加入同一个私有 libp2p Swarm（共用 `swarm.key`） |

> [!NOTE]
> 每个节点都使用这个 repo `main` 分支上**完全未修改**的原始 `Dockerfile` / `docker-compose.yml`，
> `server_mode.enabled` 保持默认值（`false`）——也就是纯 client 模式，没有改动任何代码。

---

## 🧪 测试方法

1. 在 10 个节点上分别把这个 repo 全新 clone 到干净目录：
   ```bash
   git clone https://github.com/lhu-csie-dclab/yuanyi.git
   ```
2. 只填入一个真实新加入者会需要的东西，依照 [`P2P_NETWORK.md`](../P2P_NETWORK.md) 与
   `.env.example`：
   - `swarm.key`——沿用既有 Swarm 的私有网络密钥（按文档说明，这是要私下分发给参与节点的，
     **不会**进入版本控制）。
   - `.env`——`ABS_MODEL_PATH`（指向本机已有的模型权重）、`SERVER_ADDRESS`（既有的 Bootstrap
     multiaddress）、`IFACE`、`CLIENT_WEB_PORT`，对应 `.env.example` 的字段。
   - `identity.key`、`stats.json`、`peers.db`——留空/不创建，完全交给程序自己在第一次启动时
     生成，就跟真正第一次使用的人一样。
3. 用原始的 compose 文件构建并启动：
   ```bash
   docker compose up -d --build
   ```
4. 通过每个节点的 `/api/peers`、`/api/node_info` 验证 P2P 网格是否成形，并用 `/health`（8100
   端口）确认 vLLM 是否就绪。
5. 对每个节点自己的网关（50006 端口）发送真实的 `/v1/chat/completions` 请求——先逐一测试，再
   10 台同时测试——同时在每个节点上采样 `nvidia-smi`。

### 请求参数

```json
{
  "model": "Qwen3-4B-AWQ",
  "messages": [{"role": "user", "content": "<每次测试提示词不同>"}],
  "max_tokens": 150,
  "temperature": 0.7
}
```
使用的提示词：*"Write a 100 word story about the ocean."*（逐一测试）与
*"Explain photosynthesis in detail." / "Write a detailed 200 word essay about deep learning."*
（并发测试，`max_tokens` 提高到 200-250 以延长生成窗口方便采样）。

---

## 📊 测试结果

### 1. 部署与网格成形（全部 10 个节点）

| 检查项目 | 结果 |
| :--- | :--- |
| `git clone` 到最新 `main` commit | ✅ 10/10 |
| `docker compose up -d --build` 成功 | ✅ 10/10 |
| Gateway `/health` → `200` | ✅ 10/10 |
| vLLM `/health`（8100 端口）预热后 → `200` | ✅ 10/10（其中 2 台多花约 60 秒预热才转绿） |
| P2P Peer 发现 | ✅ 10/10，每个节点各自看到 9-10 个 Peer（全连通网格） |

### 2. 逐一节点请求 + GPU 使用率

依次对每个节点自己的网关发一次请求，在生成开始约 1.5 秒时采样 `nvidia-smi`：

| 节点 | GPU 使用率（生成中） | VRAM 用量 | HTTP | 延迟 |
| :--- | :---: | :---: | :---: | :---: |
| 1-10 | **每一台均为 100%** | 约 6,320 MiB / 8,192 MiB | 200 | 2.47s - 2.61s |

10 个节点都各自独立跑到 100% GPU 使用率——确认是 10 张不同的物理 GPU 在服务，不是同一张卡在
应付全部流量。

### 3. 10 台并发请求测试

10 个节点的网关同时被打（`curl` 在两台主机上并行发送），在重叠的生成窗口期间采样 4 次
`nvidia-smi`（间隔约 0.8 秒）：

| 结果 | 数值 |
| :--- | :--- |
| 同时发出的请求数 | 10 |
| 成功响应（`HTTP 200`）| 10/10 |
| 响应时间范围 | 2.09s – 2.71s |
| 同一瞬间观察到多张卡同时 100% | 每台主机都捕捉到 2 个以上节点在同一张快照里同时 100%（采样粒度受限于每次调用容器 exec 本身的开销，不是 GPU 本身的限制） |

一个 4B 参数模型若退回 CPU 执行，单次请求通常需要数十秒到数分钟；10 个并发请求都能稳定在
2.1-2.7 秒内完成，只有真正的 GPU 加速推理才可能做到。

---

## ✅ 结论

一个完全全新 `git clone` 下来的 repo，只填入真实新参与者会拿到的信息（Bootstrap 地址 + 共用私网
密钥），通过原始 `docker-compose.yml` 就能正确构建与运行、加入既有的 P2P 网格，并用自己本机的
GPU 提供推理。在 2 台独立物理主机上分别部署的 10 个节点中，每个节点的 GPU 都被独立确认为真的在
运作（100% 使用率），并正确服务真实的对话请求——不论是逐一测试还是 10 台同时并发测试皆是如此。
