# 🧪 Experimental Research Stage & Untested Parameters Manual

This document details the experimental scope, benchmarked baseline configurations, untested parameters, and production disclaimers for the Mooncake 2.0 Client Agent.

---

## ⚠️ Production Disclaimer

> [!WARNING]
> **NOT RECOMMENDED FOR PRODUCTION DEPLOYMENTS**:
> This software is currently an **experimental academic and research prototype**. It is designed to demonstrate distributed disaggregated LLM inference swarms and is **NOT recommended for mission-critical or commercial production environments**.

---

## 📊 Benchmarked Baseline Configuration

The system has ONLY been benchmarked and verified under the following exact setup:

- **Model**: `Qwen/Qwen3-4B-AWQ`
- **Transport Protocol**: `protocol: "tcp"` (in `mooncake.json`)
- **Concurrency**: `100` concurrent requests (via NVIDIA AIPerf)
- **Hardware Cluster**: 10 x NVIDIA RTX A2000 8GB GPUs (Proxmox VE 9.1 LXC Container)

---

## 🛑 Untested Parameters & Risks

The following configurations and features remain **untested** and may cause instability or unexpected failure:

1. **Alternative LLM Models**: Models other than `Qwen3-4B-AWQ` (e.g., Llama-3, DeepSeek) have not been load-tested.
2. **Alternative Transport Protocols**: RDMA, RoCE, or non-TCP transport layers in `mooncake.json`.
3. **Alternative Quantization Schemes**: Unquantized FP16, AWQ-8bit, or GPTQ modes.
4. **Higher Concurrency**: Concurrency scaling beyond 100 concurrent workers.
5. **Multi-GPU Tensor Parallelism**: TP > 1 within a single container.
