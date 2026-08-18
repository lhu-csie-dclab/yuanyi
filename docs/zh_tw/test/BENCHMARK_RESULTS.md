# Mooncake 2.0 Client - 壓測與效能測試結果報告 (繁體中文)

本文件呈現使用 **[NVIDIA AIPerf Benchmark Suite](https://github.com/ai-dynamo/aiperf)** 於 Mooncake 2.0 Client 進行 10,000 次請求壓測的官方數據。

---

## 🖥️ 壓測硬體環境

- **硬體叢集**：**10 x NVIDIA RTX A2000 (8GB VRAM)** 分散式 Swarm 節點
- **測試模型**：`Qwen/Qwen3-4B-AWQ`
- **目標網關端點**：`http://10.0.2.201:50006/v1`

---

## ⚡ AIPerf 壓測指令

```bash
aiperf profile \
  --model "Qwen3-4B-AWQ" \
  --endpoint-type chat \
  --tokenizer "Qwen/Qwen3-4B-AWQ" \
  --url http://10.0.2.201:50006/v1 \
  --isl 512 --isl-stddev 0 \
  --osl 128 --osl-stddev 0 \
  --concurrency 100 \
  --request-count 10000
```

---

## 📊 壓測結果數據對照表

### 1. NVIDIA AIPerf | LLM Metrics

| 指標 (Metric) | 平均 (avg) | 最小 (min) | 最大 (max) | p99 | p90 | p50 | 標準差 (std) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Request Latency (ms)** | **4,159.46** | 1,511.92 | 11,639.09 | 8,529.24 | 6,287.76 | 3,901.27 | 1,491.46 |
| **E2E Output Token Throughput (tokens/sec/user)** | **34.74** | 11.00 | 84.66 | 67.47 | 51.68 | 32.81 | 12.07 |
| **Output Sequence Length (tokens)** | **128.00** | 125.00 | 132.00 | 128.00 | 128.00 | 128.00 | 0.16 |
| **Input Sequence Length (tokens)** | **512.00** | 512.00 | 513.00 | 512.00 | 512.00 | 512.00 | 0.01 |
| **Output Token Throughput (tokens/sec)** | **3,066.17** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Request Throughput (requests/sec)** | **23.96** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Request Count (requests)** | **10,000.00** | N/A | N/A | N/A | N/A | N/A | N/A |
