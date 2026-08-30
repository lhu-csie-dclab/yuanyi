# 🪟 Windows Native Installation Guide

Run the Yuanyi Client Agent **natively on Windows 10/11 — no Docker, no WSL**. The Go
agent detects Windows at startup (`runtime.GOOS == "windows"`), reads your GPU via
`nvidia-smi`, locates a local Python virtual environment, and launches vLLM as a native
subprocess.

> [!NOTE]
> Every step below was executed end-to-end on real hardware before this guide was written.
> See [`docs/test/WINDOWS_NATIVE_TEST.md`](../../test/WINDOWS_NATIVE_TEST.md) for the verified
> results (build times, inference latency, concurrency behaviour, and the two bugs this test
> surfaced and fixed).

---

## ⚡ Quickest route: the installer script

[`install.ps1`](../../../install.ps1) performs every step in this guide interactively —
including building the dashboard and Go binary and setting up the Python/vLLM environment —
and handles uninstall and model management afterwards:

```powershell
irm https://raw.githubusercontent.com/lhu-csie-dclab/yuanyi/main/install.ps1 -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File install.ps1
```

| Command | What it does |
| :--- | :--- |
| `install.ps1` | Interactive menu |
| `install.ps1 install` | Install or update |
| `install.ps1 models` | Download / switch / delete Hugging Face models |
| `install.ps1 status` | Show what is installed and whether it is running |
| `install.ps1 start` | Start the node |
| `install.ps1 stop` | Force-stop the node (and any vLLM child processes) |
| `install.ps1 restart` | Force-stop, then start |
| `install.ps1 uninstall` | Remove it (offers to back up `swarm.key`, asks about models) |

Every prompt has a default — pressing Enter throughout produces a working node. You can
override the install directory, node role, `swarm.key` (paste one to join an existing swarm, or
leave blank to generate), all six ports, and the model.

Don't want to press Enter through every prompt? Add `--example` (or `-y` / `--yes`) after the
command to accept the default answer at each one automatically, e.g.
`install.ps1 install --example` installs and starts a node fully unattended in one shot.
Confirmations that would be destructive or surprising to run unattended (uninstalling, deleting
a model, turning this node into a network hub) still default to "no" even with this flag.

Unattended mode still defaults to generating a brand-new swarm.key, which starts an isolated
swarm. To join an EXISTING swarm unattended, add `-SwarmKeyPath <path>` pointing at that
swarm's key file, e.g. `install.ps1 install --example -SwarmKeyPath C:\path\to\swarm.key`.

Missing `git`, `go`, `npm`, or `uv`? The script offers to install them for you via `winget`
(and `uv`'s official installer), so §1's prerequisites are handled automatically — it also
refreshes `PATH` in-process so the install continues without reopening the terminal.

> [!TIP]
> Choosing **relay-only** at the first prompt skips the Python environment and model download
> entirely, so a Windows machine with no GPU can contribute in a couple of minutes.

**The rest of this document is the manual equivalent.** Read it if you want to understand what
the script does, customise something it does not prompt for, or troubleshoot.

---

## 1. Prerequisites

| Requirement | Minimum | Verified on |
| :--- | :--- | :--- |
| OS | Windows 10 / 11 (x64) | Windows 11 Home |
| GPU | NVIDIA, **≥ 8 GB VRAM** recommended | GeForce RTX 3080 Laptop (8 GB) |
| NVIDIA driver | ≥ 550.xx (CUDA 12.x capable) | 555.97 |
| [Go](https://go.dev/dl/) | 1.26+ | go1.26.4 windows/amd64 |
| [Node.js](https://nodejs.org/) | 22+ | Node 22 (needed to build the dashboard) |
| [uv](https://docs.astral.sh/uv/) | any recent | uv 0.11.32 |
| [Git for Windows](https://git-scm.com/) | any recent | — |
| Free disk | ~15 GB (PyTorch CUDA + model weights) | — |

Install `uv` if you don't have it:
```powershell
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

Confirm your GPU and driver are visible:
```powershell
nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader
```

---

## 2. Clone the repository

```powershell
git clone https://github.com/lhu-csie-dclab/yuanyi.git
cd yuanyi
```

---

## 3. Create the Python environment and install vLLM for Windows

Official vLLM wheels are Linux-only, so Windows uses the community-built
[`SystemPanic/vllm-windows`](https://github.com/SystemPanic/vllm-windows) wheels (Apache-2.0,
see [`NOTICE`](../../../NOTICE)).

> [!IMPORTANT]
> The agent auto-discovers the virtual environment at one of these paths, relative to the
> directory you run the binary from. Create it at one of them, or the agent falls back to a
> bare `python` on `PATH`:
> - `.\.venv\Scripts\python.exe`
> - `.\vllm-windows\.venv\Scripts\python.exe`
> - `..\vllm-windows\.venv\Scripts\python.exe`
> - `..\.venv\Scripts\python.exe`

```powershell
# Clone the Windows vLLM build repo inside your checkout (gives you .\vllm-windows\.venv)
git clone https://github.com/SystemPanic/vllm-windows
cd vllm-windows

# Python 3.12 virtual environment
uv venv .venv --python 3.12
.\.venv\Scripts\activate

# PyTorch with CUDA 12.4
uv pip install torch==2.6.0+cu124 torchvision==0.21.0+cu124 torchaudio==2.6.0+cu124 `
  --extra-index-url https://download.pytorch.org/whl/cu124

# Download and install the Windows vLLM wheel (v0.9.2 / cu124 / cp312)
gh release download v0.9.2 -R SystemPanic/vllm-windows -D wheels_v092
uv pip install wheels_v092\vllm-0.9.2+cu124-cp312-cp312-win_amd64.whl

# Pin a compatible Transformers. Both bounds matter:
#   >=4.51 -- Qwen3 (Qwen3ForCausalLM) was only added in 4.51. Older versions fail at
#             startup with "Transformers does not recognize this architecture".
#   <5.0   -- 5.x removes APIs vLLM 0.9.2 still calls (tokenizer.all_special_tokens_extended).
uv pip install "transformers>=4.51.0,<5.0.0"

cd ..
```

No GitHub CLI? Download the `.whl` manually from the
[v0.9.2 release page](https://github.com/SystemPanic/vllm-windows/releases/tag/v0.9.2).

#### Patch vLLM's `ovis.py` (required)

vLLM 0.9.2 registers an `aimv2` model config at import time, but Transformers ships its
own `aimv2` (present throughout 4.51–4.x and in 5.x), so vLLM aborts before it can even
be imported:

```
ValueError: 'aimv2' is already used by a Transformers config, pick another name.
```

Pinning Transformers does **not** avoid this. Wrap the three registrations so an
already-registered config is a no-op — `install.ps1` does this for you automatically:

```powershell
$ovis = ".\vllm-windows\.venv\Lib\site-packages\vllm\transformers_utils\configs\ovis.py"
$out = New-Object System.Collections.Generic.List[string]
foreach ($line in [IO.File]::ReadAllLines($ovis)) {
    if ($line -match '^AutoConfig\.register\(') {
        $out.Add('try:'); $out.Add('    ' + $line); $out.Add('except ValueError:'); $out.Add('    pass')
    } else { $out.Add($line) }
}
[IO.File]::WriteAllLines($ovis, $out)
```

Re-running it is safe: once patched the calls are indented, so they no longer match.

Verify the stack:
```powershell
.\vllm-windows\.venv\Scripts\python.exe -c "import torch, vllm; print(torch.__version__, torch.cuda.is_available(), vllm.__version__)"
# expected: 2.6.0+cu124 True 0.9.2
```

---

## 4. Build the agent

The dashboard is compiled into the binary via `//go:embed web-ui/dist`, so the frontend must be
built **before** `go build`:

```powershell
cd web-ui
npm ci
npm run build
cd ..

go build -o client.exe .
```

---

## 5. Create the private network key (`swarm.key`)

> [!IMPORTANT]
> **Do this before the first launch.** The agent refuses to start without a valid `swarm.key`,
> and **every node in the same mesh must carry the byte-identical key** — it is the pre-shared
> key (PSK) that defines the private network.

**Starting a new mesh?** Generate a fresh key (pure PowerShell, no OpenSSL required):

```powershell
$bytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
$hex = -join ($bytes | ForEach-Object { $_.ToString("x2") })
[IO.File]::WriteAllText("swarm.key", "/key/swarm/psk/1.0.0/`n/base16/`n$hex`n")
```

The file must end up exactly **96 bytes** with **LF** line endings — check with
`(Get-Item swarm.key).Length`. Use `[IO.File]::WriteAllText` as shown rather than
`Set-Content` or `>` redirection: those add a BOM and/or CRLF endings.

**Joining an existing mesh?** Do **not** generate one — obtain the exact `swarm.key` from
whoever operates that mesh and copy it in byte-for-byte, then confirm it matches a working
node:

```powershell
(Get-FileHash swarm.key -Algorithm SHA256).Hash.ToLower()
```

> [!WARNING]
> Do **not** use `swarm.key.example` as your real key. It is a public placeholder committed to
> this repository, so anyone could use it to join your mesh. Keep the real key out of version
> control (`.gitignore` already excludes it).

---

## 6. Configure

`config.json` is created automatically on first run. Two Windows-specific notes:

- **Model** — `paths.model_path` defaults to the Linux container path `/data/model`, which
  doesn't exist on Windows. When `vllm.model_name` is also left at its default
  (`Qwen/Qwen3-4B-AWQ`), the agent logs a warning and downloads that same model straight from
  Hugging Face on first start, since there's nothing to substitute it with (the default *is*
  the fallback here) -- Windows-native compatibility for it is unverified. If you point
  `vllm.model_name` at something else and Windows has no local weights for it, the agent falls
  back to `Qwen/Qwen3-4B-AWQ` instead and registers **both** names as aliases, so requests
  using either name resolve. To use your own local weights, point `paths.model_path` at a real
  Windows directory.
- **VRAM** — `vllm.gpu_memory_utilization` is a fraction of **total** VRAM, not free VRAM. On
  an 8 GB card with a desktop session already using ~2 GB, leave headroom below the `0.95`
  default; raise it only on a dedicated GPU with nothing else drawing VRAM.

---

## 7. Run

```powershell
.\client.exe
```

Startup takes roughly 45-60 seconds (model load dominates). The vLLM console tab should show,
in order: Windows detection → GPU detection via `nvidia-smi` → the mounted `.venv` path → vLLM
boot.

Verify:
```powershell
# vLLM engine
curl.exe http://127.0.0.1:8100/health

# OpenAI-compatible gateway
curl.exe -X POST http://127.0.0.1:50006/v1/chat/completions `
  -H "Content-Type: application/json" `
  -d '{\"model\":\"Qwen/Qwen3-4B-AWQ\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}],\"max_tokens\":50}'
```

Dashboard: <http://localhost:50007>

---

## 8. Optional: run vLLM as a background daemon

To run vLLM alone (without the Go agent), three helper scripts are provided. All three default
to port **8100**, matching `config.json`'s `vllm.port`:

```powershell
.\start_vllm.ps1     # launch hidden background process, poll until ready
.\status_vllm.ps1    # PID, port state, loaded model, API health
.\stop_vllm.ps1      # terminate and free VRAM
```

To autostart on login: press `Win + R`, run `shell:startup`, and add a shortcut targeting:
```cmd
powershell.exe -ExecutionPolicy Bypass -WindowStyle Hidden -File "C:\path\to\your\yuanyi-checkout\start_vllm.ps1"
```

---

## 9. Troubleshooting

| Symptom | Cause & fix |
| :--- | :--- |
| `failed to open swarm.key` on startup | The key was never created. See §5 — the agent will not start without it. |
| `failed to parse swarm.key` on startup | The file exists but is malformed, usually a BOM or CRLF endings from `Set-Content`/`>`. Recreate it with `[IO.File]::WriteAllText` as shown in §5; it must be exactly 96 bytes. |
| `未找到 .venv 目錄` warning at startup | The venv isn't at one of the four discovery paths in §3. Move it, or run `client.exe` from the directory containing it. |
| vLLM exits during load, or CUDA OOM | `gpu_memory_utilization` is a fraction of *total* VRAM, not free VRAM. Close other GPU apps or lower it (`0.60`–`0.70` on an 8 GB card). |
| Gateway returns `404` for your model | The requested `model` string matches neither registered alias. Check what is actually served: `curl.exe http://127.0.0.1:8100/v1/models`. |
| `Could not apply Windows TCPStore compatibility patch` warning | Harmless on PyTorch 2.6 — the private torch internal that patch targets no longer exists, and this execution path doesn't need it. Startup continues normally. |
| Bootstrap peer connection fails | Almost always a **`swarm.key` mismatch** — libp2p reports a PSK mismatch as `failed to negotiate security protocol: incoming message was too large`, not as an auth error. Compare hashes with a working node (`sha256sum swarm.key`). Also confirm the bootstrap multiaddress is reachable. A standalone node with no peers still serves inference locally. |
| `go build` fails on `embed web-ui/dist` | §4's `npm run build` was skipped — `web-ui/dist` must exist before `go build`. |

---

## Related documentation

- [`docs/test/WINDOWS_NATIVE_TEST.md`](../../test/WINDOWS_NATIVE_TEST.md) — verified test results on real hardware
- [`docs/CONFIG.md`](../../CONFIG.md) — full configuration reference
- [`docs/P2P_NETWORK.md`](../../P2P_NETWORK.md) — `swarm.key` generation and mesh joining
- [`docs/install/ubuntu/README.md`](../ubuntu/README.md) — Ubuntu / Linux deployment
- [`docs/install/proxmox/README.md`](../proxmox/README.md) — Proxmox VE + LXC GPU passthrough
- [`docs/RUNNER_DOCKER.md`](../../RUNNER_DOCKER.md) — the Linux/Docker execution paths
