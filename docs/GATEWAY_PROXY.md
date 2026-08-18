# OpenAI API Gateway & Local-First Proxy Dispatcher

This document details the architecture and operational modes of the OpenAI-compatible API Gateway implemented in [`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go).

---

## 🔀 Overview

The Local Dispatcher operates on port `50006` (`proxy_port`). It acts as a smart gateway, supporting Local-First transparent SSE passthrough, atomic health status checks, and multi-stage P/D disaggregated scheduling.

```
API Client (aiperf / OpenAI SDK)
       │
       ▼
LocalDispatcher (proxy.go :50006)
       │
       ├─► 1. Check vLLM Readiness (atomic.Bool)
       │
       ├─► 2. Local vLLM (127.0.0.1:8100)
       │       - Zero-Buffer SSE Passthrough (http.Flusher)
       │       - Instant 0ms response
       │
       └─► 3. P2P Swarm Backup (If Local Unready/Busy)
               - Round-Robin fallback over libp2p
```

---

## 🚀 Key Dispatcher Features

### 1. vLLM Health Check & Readiness State (`startVLLMHealthChecker`)
- Scrapes `http://127.0.0.1:8100/health` every 5 seconds.
- Sets `vllmReady` (`atomic.Bool`) to `true` when vLLM responds with `200 OK`.
- Silently suppresses log warnings during initial 15-30 second vLLM model loading.

### 2. Transparent Zero-Buffer SSE Passthrough (`proxyToLocalVLLMDirect`)
- Forwards HTTP POST requests directly to `http://127.0.0.1:8100/v1/chat/completions`.
- Streams response chunks dynamically using a 4096-byte rolling buffer and `flusher.Flush()`.
- Supports `stream: true` (Server-Sent Events) and `stream: false` (JSON) with zero latency.

---

## ⚙️ Scheduling Modes

### Mode 1: Local-First & PD-Together Hybrid Mode (`IsPDTogether == true`)
1. **Primary**: Routes to local GPU (`127.0.0.1:8100`).
2. **Backup**: If local vLLM is unready or busy, selects remote P2P peers via Round-Robin (`streamToPeer`).

### Mode 2: Disaggregated Prefill/Decode Mode (`IsPDTogether == false`)
1. **Stage 1 (Prefill)**: Constructs request with `max_tokens: 1`, sending to designated Prefill node for KV cache precomputation.
2. **Stage 2 (Decode)**: Sends decode request referencing precomputed KV cache to designated Decode node.

---

## 🌐 Mooncake KV Tunnel (`/mooncake_kv/`)
Proxies low-level KV cache transfers between nodes over `/mooncake_kv/{peer_id}/{port}` endpoints.
