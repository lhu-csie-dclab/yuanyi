# Configuration Management & Settings Guide

This document provides a detailed reference for configuration handling in Mooncake 2.0 Client Agent, covering [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go), [`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json), [`.env.example`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/.env.example), and [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json).

---

> [!WARNING]
> **Production Disclaimer & Untested Options Notice**:
> This software is currently in an **experimental research phase** and is **NOT recommended for production environments**. Only baseline settings (`Qwen3-4B-AWQ`, `protocol: "tcp"`) have been benchmarked; all other unverified configuration parameters remain **untested**.

---

## 📄 File Overview

- **`config.go`**: Go configuration parser, single-line comment stripper, active NIC detector, and defensive port validator.
- **`config.json`**: Primary JSON configuration file defining ports, P2P trackers, Docker arguments, and vLLM hyperparameters.
- **`.env.example`**: Environment template for host model paths and docker container environment bindings.
- **`mooncake.json`**: Low-level protocol and buffer configuration for the Mooncake KV Cache Transfer Engine.

---

## ⚙️ `config.json` Field Specification

```json
{
  "version": "1.0",
  "web_port": 50007,
  "proxy_port": 50006,
  "p2p": {
    "server_address": "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh"
  },
  "docker": {
    "container_name": "vllm_node",
    "image": "vllm-runtime-mooncake:latest",
    "network": "host",
    "shm_size": "16gb",
    "iface": "eth0"
  },
  "paths": {
    "config_path": "/app/config.json",
    "model_path": "/data/model",
    "mooncake_path": "/data/mooncake.json"
  },
  "vllm": {
    "model_name": "Qwen3-4B-AWQ",
    "max_model_len": 8192,
    "gpu_memory_utilization": 0.75,
    "port": 8100,
    "tensor_parallel_size": 1,
    "dtype": "float16",
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": 8998,
    "mooncake_abort_request_timeout": 15,
    "attention_backend": "FLASH_ATTN",
    "placement_group_bundle_strategy": "SPREAD"
  }
}
```

### Key Parameters

| Key | Default | Type | Description |
| :--- | :--- | :--- | :--- |
| `web_port` | `50007` | Integer | HTTP port for Web Monitoring Dashboard console & APIs. |
| `proxy_port` | `50006` | Integer | HTTP port for OpenAI-compatible API Gateway. |
| `p2p.server_address` | Multiaddr | String | Bootstrap Tracker Multiaddress for DHT discovery & NAT relay. |
| `vllm.port` | `8100` | Integer | Local vLLM engine HTTP endpoint. |
| `vllm.gpu_memory_utilization` | `0.75` | Float | Maximum VRAM memory allocation ratio reserved for vLLM & KV cache. |
| `vllm.kv_role` | `"kv_both"` | String | P/D disaggregation role: `"kv_prefill"`, `"kv_decode"`, or `"kv_both"`. |
| `vllm.mooncake_bootstrap_port` | `8998` | Integer | Mooncake KV Cache transfer control port. |

---

## 🌐 Active Network Interface Auto-Detection (`detectActiveNetworkInterface`)

`config.go` automatically discovers active physical network interfaces if `docker.iface` is omitted:

1. Iterates over all system interfaces (`net.Interfaces()`).
2. Filters out Loopback (`127.0.0.1`) and `DOWN` interfaces.
3. Prioritizes physical NIC prefixes (`eth*`, `en*`, `wlan*`) with bound IP addresses.
4. Falls back to default `"eth0"` if no matching NIC is found.

---

## 🛡️ Port Conflict Defense Mechanism

`LoadOrCreateConfig` validates all assigned ports to prevent runtime collisions:
- If `web_port <= 0` $\rightarrow$ set to `50007`.
- If `proxy_port <= 0` $\rightarrow$ set to `50006`.
- If `vllm.port == proxy_port` or `<= 0` $\rightarrow$ reset to `8100`.
- If `vllm.mooncake_bootstrap_port == proxy_port` or `<= 0` $\rightarrow$ reset to `8998`.

---

## 🍰 `mooncake.json` Transport Configuration

`mooncake.json` configures the Mooncake KV Cache transfer engine transport layer to use **TCP Sockets** (`protocol: "tcp"`) for cross-node KV cache transfers:

```json
{
  "metadata_server": "P2PHANDSHAKE",
  "global_segment_size": "0",
  "local_buffer_size": "17179869184",
  "protocol": "tcp",
  "device_name": ""
}
```

### Mooncake Parameters Breakdown

- **`protocol`: `"tcp"`**: Enforces TCP transport layer protocol for KV cache streaming (compatible with standard Ethernet without requiring specialized InfiniBand / RoCE RDMA hardware).
- **`metadata_server`: `"P2PHANDSHAKE"`**: Delegates metadata server negotiation to Mooncake Client P2P handshake mechanism.
- **`local_buffer_size`: `"17179869184"`**: Allocates 16 GB (`16 * 1024^3` bytes) local RAM buffer for KV cache staging.
