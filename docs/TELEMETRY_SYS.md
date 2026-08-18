# System Telemetry, GPU Metrics, & Persistence Guide

This document details hardware telemetry, vLLM Prometheus metrics scraping, and stats disk persistence implemented in [`sys.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/sys.go) and [`stats.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/stats.json).

---

## 📊 Telemetry Architecture (`sys.go`)

`SysMonitor` collects real-time hardware telemetry and inference throughput metrics every 2 seconds:

```
                  ┌─────────────────────────────────────┐
                  │          SysMonitor (sys.go)        │
                  └──────────────────┬──────────────────┘
                                     │
    ┌────────────────────────────────┼────────────────────────────────┐
    ▼                                ▼                                ▼
┌───────────────────────┐ ┌───────────────────────┐ ┌───────────────────────┐
│ vLLM Prometheus API   │ │ Host Process Metrics  │ │ NVIDIA NVML Telemetry │
│ (127.0.0.1:8100)      │ │ (gopsutil)            │ │ (nvidia-smi CLI)      │
└───────────────────────┘ └───────────────────────┘ └───────────────────────┘
```

---

## 📈 Scraped Metrics Specification

### 1. vLLM Prometheus Metrics Scraper (`metricScraper`)
Scrapes `http://127.0.0.1:8100/metrics` every 2 seconds to compute dynamic throughput rates:

- `vllm:num_requests_running`: Active request queue depth.
- `vllm:gpu_cache_usage_perc`: GPU KV cache memory utilization ratio.
- `vllm:prompt_tokens_total`: Incremental prefill throughput (tokens/s).
- `vllm:generation_tokens_total`: Incremental generation throughput (tokens/s).
- `vllm:time_to_first_token_seconds`: Time-To-First-Token (TTFT) latency.

### 2. Host Process Metrics (`gopsutil`)
- `cpuPercentStr()`: Scrapes CPU utilization percentage.
- `memUsageStr()`: Queries process RSS (Resident Set Size) memory in MB/GB.

### 3. GPU Telemetry (`GetGPUTelemetry` & `nvidia-smi`)
Executes `nvidia-smi` CSV queries to extract:
- GPU Model Summary (e.g. `NVIDIA RTX 4090 x1`)
- GPU Core Temp (℃) & Fan Speed (%)
- GPU & Memory Controller Utilization (%)
- Used VRAM (MB) & Total VRAM (MB)
- Power Draw (W) & Power Limit (W)
- NVIDIA Driver Version

---

## 💾 Stats Disk Persistence (`stats.json`)

To preserve throughput statistics across application restarts, `tui.go` automatically persists cumulative metrics to `./stats.json`:

```json
{
  "requests": 1542,
  "success_count": 1540,
  "error_count": 2
}
```

- **Load**: `loadStatsDisk()` loads initial values upon startup.
- **Save**: Background ticker periodically dumps stats to disk every 5 seconds and upon graceful application exit (`Stop()`).
