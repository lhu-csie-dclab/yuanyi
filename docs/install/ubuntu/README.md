# 🐧 Ubuntu Installation Guide

Deploy the Yuanyi Client Agent on Ubuntu. This is the **primary, production-tested
platform** — the benchmark and multi-node results in [`docs/test/`](../../test/) were all
produced on this path.

---

## ⚡ Quickest route: the installer script

[`install.sh`](../../../install.sh) performs every step in this guide interactively, and
handles uninstall and model management afterwards. If you just want a working node, this is
all you need:

```bash
curl -fsSL https://raw.githubusercontent.com/lhu-csie-dclab/yuanyi/main/install.sh -o install.sh
bash install.sh
```

| Command | What it does |
| :--- | :--- |
| `bash install.sh` | Interactive menu |
| `bash install.sh install` | Install or update |
| `bash install.sh models` | Download / switch / delete Hugging Face models |
| `bash install.sh status` | Show what is installed and whether it is running |
| `bash install.sh start` | Start the node |
| `bash install.sh stop` | Stop the node |
| `bash install.sh restart` | Stop, then start |
| `bash install.sh uninstall` | Remove it (offers to back up `swarm.key`, asks about models) |

Every prompt has a default — pressing Enter throughout produces a working node. You can
override the install directory, node role (inference or **relay-only**, which needs no GPU),
`swarm.key` (paste one to join an existing swarm, or leave blank to generate), all six ports,
and the model.

Missing `git` or `docker`? The script offers to install them for you (via `apt`/`dnf`/`yum`/
`pacman`/`zypper`), so §1's prerequisites are handled automatically. Docker comes from your
distribution's repository rather than a piped remote script. Because Docker installs stopped
and root-owned, the script also enables the service and adds you to the `docker` group — that
group change only takes effect on a **new login**, so run `newgrp docker` (or log out and back
in) and re-run the script if it says so.

**The rest of this document is the manual equivalent.** Read it if you want to understand what
the script does, customise something it does not prompt for, or troubleshoot.

---

> [!NOTE]
> Versions in the tables below are from the live 10-node cluster that produced
> [`MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md), not from a fresh install
> guess.

Two deployment paths:

| Path | Use when | Host needs |
| :--- | :--- | :--- |
| **A — Docker (recommended)** | Normal deployment | Docker + NVIDIA Container Toolkit only |
| **B — Native build** | Development, or no Docker available | Go 1.26+, Node.js 22+, Python/vLLM |

Path A needs **neither Go nor Node.js on the host** — the multi-stage `Dockerfile` builds the
Vue dashboard and the Go binary inside the image. The verified cluster nodes have no host
Node.js installed at all.

---

## 1. Prerequisites

| Requirement | Minimum | Verified on |
| :--- | :--- | :--- |
| OS | Ubuntu 22.04 / 24.04 / 26.04 LTS (x64) | **Ubuntu 26.04 LTS** |
| GPU | NVIDIA, ≥ 8 GB VRAM recommended | Quadro RTX 4000 (8 GB), compute 7.5 |
| NVIDIA driver | ≥ 550 (CUDA 12.x+); CUDA 13 image needs ≥ 580 | `595.71.05` / `610.43.02` |
| Docker Engine | 24+ | **29.6.1** |
| Docker Compose plugin | v2+ | **v5.3.1** |
| NVIDIA Container Toolkit | 1.14+ | **1.19.1** |
| Go *(path B only)* | 1.26+ | `go1.26.0 linux/amd64` |
| Node.js *(path B only)* | 22+ | — |
| Free disk | ~40 GB (image + model weights) | — |

Check your GPU and driver first — everything else depends on this working:

```bash
nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader
```

---

## 2. Install Git and clone

```bash
sudo apt-get update && sudo apt-get install -y git git-lfs
git clone https://github.com/lhu-csie-dclab/yuanyi.git
cd yuanyi
```

---

## 3. Install Docker Engine

Per the [official Ubuntu instructions](https://docs.docker.com/engine/install/ubuntu/):

```bash
# Remove any distro-packaged versions first
sudo apt-get remove -y docker docker-engine docker.io containerd runc

# Add Docker's official repository
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install engine + compose plugin
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

docker --version && docker compose version
```

To run Docker without `sudo`:

```bash
sudo usermod -aG docker $USER && newgrp docker
```

---

## 4. Install the NVIDIA Container Toolkit

This is what lets containers see the GPU. Installing it is **not** optional for path A.

```bash
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
  | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
  | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
  | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

sudo apt-get update
sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker
```

Verify a container can actually reach the GPU before going further:

```bash
docker run --rm --gpus all ubuntu:24.04 nvidia-smi
```

If that prints your GPU table, the hard part is done.

---

## 5. Download the model

```bash
git lfs install
mkdir -p ~/models && cd ~/models
git clone https://huggingface.co/Qwen/Qwen2-VL-2B-Instruct-AWQ
cd -
```

`Qwen/Qwen2-VL-2B-Instruct-AWQ` is this project's current default (a vision-capable model, so the
Chat page's image attachment works out of the box). Note its **absolute** path — you need it
next. The `Qwen3-4B-AWQ` benchmark figures in [BENCHMARK_RESULTS.md](../../test/BENCHMARK_RESULTS.md)
were measured against the older text-only default and have not been re-run against this model.

---

## 6. Create the private network key (`swarm.key`)

> [!IMPORTANT]
> **Do this before the first launch.** The agent refuses to start without a valid `swarm.key`,
> and **every node in the same mesh must carry the byte-identical key** — it is the pre-shared
> key (PSK) that defines the private network.

**Starting a new mesh?** Generate a fresh key:

```bash
printf '/key/swarm/psk/1.0.0/\n/base16/\n%s\n' "$(openssl rand -hex 32)" > swarm.key
```

The file must be exactly **96 bytes** — check with `wc -c < swarm.key`.

**Joining an existing mesh?** Do **not** generate one — obtain the exact `swarm.key` from
whoever operates that mesh and copy it in byte-for-byte, then confirm it matches:

```bash
sha256sum swarm.key
```

> [!WARNING]
> Do **not** use `swarm.key.example` as your real key. It is a public placeholder committed to
> this repository, so anyone could use it to join your mesh. Keep the real key out of version
> control (`.gitignore` already excludes it).

---

## 7. Configure `.env`

```bash
cp .env.example .env
```

Set the absolute model path and your network interface:

```env
ABS_MODEL_PATH=/home/youruser/models/Qwen2-VL-2B-Instruct-AWQ
SERVER_ADDRESS=/dns4/your-bootstrap-host/tcp/50004/p2p/12D3KooW...
IFACE=eth0
CLIENT_WEB_PORT=50007
```

Find the interface NCCL/GLOO should bind to:

```bash
ip -o -4 route show to default | awk '{print $5}'
```

`SERVER_ADDRESS` is the bootstrap peer multiaddress for the mesh you are joining. Leave it as
shipped if you are the first node — the agent still runs standalone and serves inference
locally.

---

## 8. Path A — build and launch with Docker (recommended)

```bash
docker compose up -d --build
```

The multi-stage build compiles the Vue dashboard (Node stage), then the Go binary, then layers
both onto the CUDA runtime image. First build pulls several GB and takes a while; subsequent
builds are cached.

Watch startup (model load dominates, ~45–90 s):

```bash
docker compose logs -f
```

---

## 9. Path B — native build (no Docker)

Requires Go and Node.js on the host. The dashboard is embedded via `//go:embed web-ui/dist`, so
it must be built **before** `go build`:

```bash
cd web-ui && npm ci && npm run build && cd ..
go build -o client .
./client
```

You must also provide a local vLLM reachable on `vllm.port` (default `8100`), since this path
does not manage containers for you.

---

## 10. Verify

```bash
# vLLM engine
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8100/health

# OpenAI-compatible gateway
curl -s http://127.0.0.1:50006/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"Qwen/Qwen2-VL-2B-Instruct-AWQ","messages":[{"role":"user","content":"Hello"}],"max_tokens":50}'

# Peers discovered on the mesh
curl -s http://127.0.0.1:50007/api/peers
```

Dashboard: `http://<host>:50007`

---

## 11. Troubleshooting

| Symptom | Cause & fix |
| :--- | :--- |
| `failed to open swarm.key` | The key was never created — see §6. The agent will not start without it. |
| `failed to negotiate security protocol: incoming message was too large` | A **`swarm.key` mismatch**, not a network fault. libp2p reports a PSK mismatch this way. Compare `sha256sum swarm.key` against a working node. |
| `docker: Error response ... could not select device driver` | NVIDIA Container Toolkit missing or Docker not restarted after `nvidia-ctk runtime configure`. Re-run §4 and verify with `docker run --rm --gpus all ubuntu:24.04 nvidia-smi`. |
| vLLM exits during load / CUDA OOM | `vllm.gpu_memory_utilization` is a fraction of *total* VRAM, not free VRAM. Lower it, or free the GPU. |
| `go build` fails on `embed web-ui/dist` | Path B only: `npm run build` was skipped. `web-ui/dist` must exist before `go build`. |
| Container starts but GPU is idle | Confirm `deploy.resources.reservations.devices` survived any local `docker-compose.yml` edits, and that `nvidia-smi` works inside: `docker compose exec yuanyi-client nvidia-smi`. |
| Peers never appear | Check `SERVER_ADDRESS` is reachable and the `swarm.key` matches. A node with no peers still serves inference locally — see §5 of the Windows test doc for that behaviour. |

---

## Notes on containerized hosts (LXC / Proxmox)

The verified cluster runs Ubuntu 26.04 inside **unprivileged Proxmox LXC containers** with the
GPU passed through, Docker running nested inside each container. That works, but the GPU device
nodes and the NVIDIA driver must already be visible *inside* the LXC before Docker can pass them
to the app container — verify with `nvidia-smi` in the LXC shell first.

`docker-compose.yml` uses `network_mode: host` and `ipc: host` deliberately: libp2p mDNS/UPnP
needs real host networking, and NCCL needs host shared memory. See
[`docs/RUNNER_DOCKER.md`](../../RUNNER_DOCKER.md).

---

## Related documentation

- [`docs/install/proxmox/README.md`](../proxmox/README.md) — Proxmox VE + LXC GPU passthrough
- [`docs/install/proxmox/README.md`](../proxmox/README.md) — Proxmox VE + LXC GPU passthrough
- [`docs/install/windows/README.md`](../windows/README.md) — native Windows deployment
- [`docs/CONFIG.md`](../../CONFIG.md) — full configuration reference
- [`docs/P2P_NETWORK.md`](../../P2P_NETWORK.md) — `swarm.key` format and mesh joining
- [`docs/RUNNER_DOCKER.md`](../../RUNNER_DOCKER.md) — Docker/Ray/vLLM orchestration internals
- [`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md) — 10-node verified deployment
