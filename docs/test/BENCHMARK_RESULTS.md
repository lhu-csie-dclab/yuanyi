# Mooncake 2.0 Client - Benchmark & Stress Test Results

This document presents the official benchmark performance metrics for Mooncake 2.0 Client Swarm, evaluated using **[NVIDIA AIPerf Benchmark Suite](https://github.com/ai-dynamo/aiperf)** (`https://github.com/ai-dynamo/aiperf`).

---

## 🖥️ Benchmark Cluster Hardware Specifications

- **Cluster Hardware**: **10 x NVIDIA RTX A2000 (8GB VRAM)** Distributed Swarm Nodes
- **Target Model**: `Qwen/Qwen3-4B-AWQ` (AWQ 4-bit Quantized Model)
- **Target Gateway Endpoint**: `http://10.0.2.201:50006/v1`
- **Benchmarking Tool**: [NVIDIA AIPerf](https://github.com/ai-dynamo/aiperf) (`nvcr.io/nvidia/ai-dynamo/aiperf`)

---

## ⚡ AIPerf Execution Command

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

### Benchmark Profile Parameters
- **Input Sequence Length (ISL)**: `512` tokens (`stddev: 0`)
- **Output Sequence Length (OSL)**: `128` tokens (`stddev: 0`)
- **Concurrency (Concurrent Clients)**: `100` workers
- **Total Request Volume**: `10,000` completed HTTP requests

---

## 📊 Benchmark Results

### 1. NVIDIA AIPerf | LLM Metrics

| Metric | avg | min | max | p99 | p90 | p50 | std |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Request Latency (ms)** | **4,159.46** | 1,511.92 | 11,639.09 | 8,529.24 | 6,287.76 | 3,901.27 | 1,491.46 |
| **E2E Output Token Throughput (tokens/sec/user)** | **34.74** | 11.00 | 84.66 | 67.47 | 51.68 | 32.81 | 12.07 |
| **Output Sequence Length (tokens)** | **128.00** | 125.00 | 132.00 | 128.00 | 128.00 | 128.00 | 0.16 |
| **Input Sequence Length (tokens)** | **512.00** | 512.00 | 513.00 | 512.00 | 512.00 | 512.00 | 0.01 |
| **Output Token Throughput (tokens/sec)** | **3,066.17** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Request Throughput (requests/sec)** | **23.96** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Request Count (requests)** | **10,000.00** | N/A | N/A | N/A | N/A | N/A | N/A |

---

### 2. NVIDIA AIPerf | LLM Metrics: Usage

| Metric | avg | min | max | p99 | p90 | p50 | std |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Usage Prompt Tokens (tokens)** | **520.00** | 520.00 | 521.00 | 520.00 | 520.00 | 520.00 | 0.01 |
| **Usage Completion Tokens (tokens)** | **128.00** | 128.00 | 128.00 | 128.00 | 128.00 | 128.00 | 0.00 |
| **Usage Total Tokens (tokens)** | **648.00** | 648.00 | 649.00 | 648.00 | 648.00 | 648.00 | 0.01 |
| **Total Usage Prompt Tokens (tokens)** | **5,200,002.00** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Total Usage Completion Tokens (tokens)** | **1,280,000.00** | N/A | N/A | N/A | N/A | N/A | N/A |
| **Total Usage Total Tokens (tokens)** | **6,480,002.00** | N/A | N/A | N/A | N/A | N/A | N/A |

---

## 📈 Performance Summary & Key Highlights

1. **High Sustained Output Throughput**:
   Achieves **`3,066.17 tokens/sec`** total system generation throughput across 10 x RTX A2000 (8GB) GPUs.
2. **High Concurrency Handling**:
   Sustains **`23.96 requests/sec`** under **`100` concurrent benchmark workers** with **100% success rate** across 10,000 requests.
3. **Low Latency Per User**:
   Average per-user decode generation speed of **`34.74 tokens/sec/user`**, with a p50 latency of **`3,901.27 ms`** for 648 total tokens per request.
