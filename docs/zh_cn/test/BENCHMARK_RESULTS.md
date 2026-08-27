# Yuanyi Client - 压测与性能测试结果报告 (简体中文)

本文档呈现使用 **[NVIDIA AIPerf Benchmark Suite](https://github.com/ai-dynamo/aiperf)** 于 Yuanyi Client 进行 10,000 次请求压测的官方数据。

---

## 🖥️ 压测硬件环境

- **硬件集群**：**10 x NVIDIA RTX A2000 (8GB VRAM)** 分布式 Swarm 节点
- **测试模型**：`Qwen/Qwen3-4B-AWQ`
- **目标网关端点**：`http://10.0.2.201:50006/v1`
