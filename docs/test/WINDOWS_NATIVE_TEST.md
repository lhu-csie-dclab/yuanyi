# Windows Native Deployment Test

End-to-end validation of the **native Windows execution path** (`runner.go: startVLLMWindows`)
on real consumer hardware: a fresh `git clone`, a from-scratch build, and real inference
through the OpenAI-compatible gateway — no Docker, no WSL.

This test was run by following [`docs/install/windows/README.md`](../install/windows/README.md)
step by step. Two genuine bugs surfaced during the run; both were fixed and re-verified on the
same machine before this document was finalized (see [Bugs found and fixed](#-bugs-found-and-fixed)).

**Test date:** 2026-08-21

---

## 🖥️ Test Environment

| Item | Value |
| :--- | :--- |
| OS | Windows 11 Home (26200) |
| GPU | NVIDIA **GeForce RTX 3080 Laptop** (8 GB VRAM) |
| NVIDIA driver | 555.97 (CUDA 12.x) |
| Go toolchain | `go1.26.4 windows/amd64` |
| Node.js | 22 (dashboard build) |
| uv | 0.11.32 |
| Python | 3.12.13 (`uv`-managed, in `.venv`) |
| PyTorch | `2.6.0+cu124` |
| vLLM | `0.9.2` ([`SystemPanic/vllm-windows`](https://github.com/SystemPanic/vllm-windows) wheel, `cp312-win_amd64`) |
| Model | `Qwen/Qwen2.5-3B-Instruct-AWQ` (4-bit AWQ) |
| Repository commit | `152af6d` |
| GPU baseline (idle desktop) | ~2.0 GB used of 8 GB |

> [!NOTE]
> Unlike the Linux nodes in [`MULTI_NODE_CLONE_TEST.md`](MULTI_NODE_CLONE_TEST.md), this is a
> **single standalone machine with no swarm peers** — which turned out to matter (see bug 2).

---

## 🧪 Methodology

1. Fresh `git clone` of this repository into a clean directory.
2. Supplied only what a real participant would: `swarm.key`, `config.json`, `mooncake.json`.
   `identity.key` / `stats.json` / `peers.db` were left for the application to generate.
3. Built the Vue dashboard (`npm ci && npm run build`) then the Go binary (`go build`).
4. Started `client.exe` and verified the Windows-specific startup path in its own logs.
5. Exercised the gateway: single, sequential, concurrent, and streaming (SSE) requests.
6. Verified the dashboard, and the three standalone PowerShell daemon scripts.

---

## 📊 Results

### 1. Build & Startup

| Check | Result |
| :--- | :--- |
| `git clone` → commit `152af6d` | ✅ |
| `npm ci && npm run build` (Vue 3 + Vite + Tailwind) | ✅ 2.75 s |
| `go build -o client.exe .` (native Windows) | ✅ **5.36 s**, 55.1 MB binary |
| Windows auto-detection (`runtime.GOOS=windows`) | ✅ switched to native mode, no Docker involved |
| GPU detection via `nvidia-smi` | ✅ `NVIDIA GeForce RTX 3080 Laptop GPU(8192MB) x1 (driver 555.97)` |
| `.venv` auto-discovery | ✅ mounted `.\vllm-windows\.venv\Scripts\python.exe` |
| vLLM engine ready (`:8100/health` → 200) | ✅ ~30–50 s from launch |
| Gateway listening (`:50006`) | ✅ |
| Web dashboard listening (`:50007`) | ✅ |

### 2. Inference through the OpenAI gateway (`:50006`)

| Pattern | Requests | Success | Latency |
| :--- | :---: | :---: | :--- |
| Single (cold, first request) | 1 | ✅ 1/1 | 1.44 s |
| Sequential (warm) | 5 | ✅ 5/5 | 0.16 s – 0.22 s |
| **Concurrent** | 8 | ✅ **8/8** | 0.99 s – 2.67 s |
| Streaming (SSE, `stream: true`) | 1 | ✅ 1/1 | 13 `data:` chunks, `HTTP 200` |

GPU sampled under load: **6976 MiB used, 37 % utilization, 50 °C** — confirming the physical
GPU is genuinely executing inference, not falling back to CPU.

The 8 concurrent requests all completing within a single ~2.7 s window (rather than serializing
into ~8 × single-request latency) is vLLM's continuous batching working correctly on Windows.

### 3. Model alias registration

`config.json` shipped the Linux defaults (`model_name: "Qwen3-4B-AWQ"`,
`model_path: "/data/model"`). `/data/model` does not exist on Windows, so the agent
substituted a Windows-verified Hugging Face model and logged it explicitly:

```
[Warning] 設定檔指定的模型 "Qwen3-4B-AWQ" 在 Windows 原生模式下不可用，已改用 Qwen/Qwen2.5-3B-Instruct-AWQ 代替
```

`GET :8100/v1/models` then correctly reported **both** aliases:

| Alias | Purpose |
| :--- | :--- |
| `Qwen3-4B-AWQ` | the configured name — keeps gateway/swarm routing resolving |
| `Qwen/Qwen2.5-3B-Instruct-AWQ` | the model actually loaded — honest identity |

### 4. Web dashboard & standalone daemon scripts

| Check | Result |
| :--- | :--- |
| `GET :50007/` (embedded Vue SPA) | ✅ `HTTP 200`, 459 B index |
| `GET :50007/assets/index-*.js` | ✅ `HTTP 200`, 103,165 B |
| `GET :50007/api/local_stats` | ✅ live counters |
| `.\start_vllm.ps1` (standalone, no Go agent) | ✅ launched hidden, polled to ready, reported `:8100` |
| `.\status_vllm.ps1` | ✅ `RUNNING (Port 8100 is open)`, correct PID + model |
| `.\stop_vllm.ps1` | ✅ terminated process, VRAM released (6.9 GB → 2.0 GB) |

### 5. P2P swarm

The node generated a fresh, stable `identity.key` / PeerID and started its libp2p host
successfully. It could **not** reach the configured bootstrap node from this network
(`failed to negotiate security protocol`), so it ran as a **standalone node with 0 peers** —
which is what exposed bug 2 below. Local inference is unaffected by having no peers.

---

## 🐛 Bugs found and fixed

Both were found *because* the guide was executed rather than merely written, and both are fixed
and re-verified on this same hardware.

### Bug 1 — Gateway returned `404` for every request

`--served-model-name` was set to the *substituted* model, so vLLM no longer recognized the
configured name (`Qwen3-4B-AWQ`) that the gateway forwards — every gateway request 404'd while
vLLM itself was perfectly healthy.

**Root cause:** an over-correction in an earlier review fix (PR #14). Making the served name
honest broke routing.
**Fix:** register **both** aliases (vLLM's `--served-model-name` accepts a list) — routing keeps
working *and* `/v1/models` tells the truth.
**Verified:** gateway request with the configured name → `HTTP 200`.

### Bug 2 — 7 of 8 concurrent requests returned `502` on a standalone node

The concurrency-aware dispatcher (PR #4) marks the local engine busy and offloads overflow to
P2P peers. With **zero peers** — the normal case for a single Windows machine — there was
nowhere to offload to, so every request past the first fell through to
`All P/D execution attempts failed`.

**Fix:** compute the peer list *before* the local/remote decision; when no peers are available,
queue on the local engine instead of failing. vLLM's continuous batching handles the
concurrency, so queuing is strictly better than a 502.
**Verified:** 8 concurrent requests went from **1/8 → 8/8**.

Also corrected during this run: the three PowerShell helper scripts and `serve_api.py` were
hardcoded to port `8000` while the agent launches vLLM on `8100`, so `status_vllm.ps1` /
`stop_vllm.ps1` could never see or stop a running instance. All four now agree on `8100`
(verified in §4 above).

---

## ✅ Conclusion

**Windows 10/11 can run this project natively.** A fresh clone builds in seconds with the
standard Go toolchain, the agent auto-detects Windows, discovers the GPU and the local Python
environment without configuration, and serves real inference through the OpenAI-compatible
gateway — single, sequential, concurrent, and streaming — on a consumer 8 GB laptop GPU.

The single most important Windows-specific caveat: a **standalone node has no peers to offload
to**, so the dispatcher must (and now does) queue locally under concurrent load rather than
attempting a P2P handoff that cannot succeed.

Setup instructions: [`docs/install/windows/README.md`](../install/windows/README.md).
