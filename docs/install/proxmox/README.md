# 🖥️ Proxmox VE + LXC GPU Passthrough Guide

Run the Yuanyi Client Agent inside a **Proxmox VE LXC container** with a passed-through
NVIDIA GPU. This is how the 10-node cluster behind
[`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md) is deployed: one
LXC per GPU, Docker nested inside each container.

> [!NOTE]
> Every command here was executed on a live Proxmox VE 9.2.2 host — including building a fresh
> container from scratch to confirm the procedure end to end. Versions in the tables are
> observed, not assumed.

---

## ⚡ Where the installer script fits

Unlike the [Ubuntu](../ubuntu/README.md) and [Windows](../windows/README.md) guides, the
installer **cannot do most of this one**. Sections 1-7 configure the *host* and the *container*
— host NVIDIA driver, LXC creation, GPU device passthrough, nested Docker — which has to happen
before there is anywhere for the agent to run.

Once the container is up and `docker run --rm --gpus all ubuntu:24.04 nvidia-smi` works inside
it (end of §6), [`install.sh`](../../../install.sh) takes over and replaces §7 and §8:

```bash
# run this INSIDE the LXC container, not on the Proxmox host
curl -fsSL https://raw.githubusercontent.com/lhu-csie-dclab/yuanyi/main/install.sh -o install.sh
bash install.sh
```

It handles the model download, `swarm.key`, ports, build and launch, and manages models and
uninstall afterwards. For a multi-node cluster, repeat §3-§6 per container and then run the
installer in each.

> [!WARNING]
> Do **not** run the installer on the Proxmox host itself. It is meant for the container; the
> host only needs the driver from §1.

---

## Why LXC instead of a VM

An LXC container shares the host kernel, so the GPU needs **no VFIO/IOMMU passthrough** and the
GPU is not locked to one guest — the same physical card can be exposed to multiple containers.
The trade-off is that the **kernel module lives on the host** and the container only gets the
userspace driver, so the two versions must match exactly (§4).

---

## Architecture

```
Proxmox VE host (kernel + NVIDIA kernel module)
└── LXC container (unprivileged, nesting=1)
    ├── NVIDIA userspace driver  ← installed with --no-kernel-module
    ├── Docker Engine + NVIDIA Container Toolkit  ← no-cgroups = true
    └── yuanyi client container  ← --gpus all
```

Three separate layers must each be told about the GPU. Missing any one produces a different,
confusing error — see [Troubleshooting](#9-troubleshooting).

---

## Verified environment

| Layer | Component | Version |
| :--- | :--- | :--- |
| Host | Proxmox VE | **9.2.2** (kernel `7.0.2-6-pve`) |
| Host | NVIDIA driver (with kernel module) | **610.43.02** |
| Host | GPU | Quadro RTX 4000 8 GB ×5 (compute 7.5) |
| Container | OS template | `ubuntu-26.04-standard_26.04-1_amd64` |
| Container | NVIDIA driver (userspace only) | **610.43.02** — must equal host |
| Container | Docker Engine / Compose | 29.7.2 / v5.5.0 |
| Container | NVIDIA Container Toolkit | 1.20.0 |

---

## 1. Install the NVIDIA driver on the Proxmox host

The **host** owns the kernel module. Do this first — nothing else works until `nvidia-smi`
succeeds here.

```bash
# Headers for the running PVE kernel, needed to build the kernel module
apt-get update
apt-get install -y pve-headers-$(uname -r) build-essential pkg-config

# Blacklist the open-source nouveau driver, then reboot
echo -e "blacklist nouveau\noptions nouveau modeset=0" > /etc/modprobe.d/blacklist-nouveau.conf
update-initramfs -u
reboot
```

After the reboot, install the driver **with** its kernel module (the default):

```bash
DRIVER=610.43.02   # pick your version; the container must match it exactly
wget https://us.download.nvidia.com/XFree86/Linux-x86_64/${DRIVER}/NVIDIA-Linux-x86_64-${DRIVER}.run
chmod +x NVIDIA-Linux-x86_64-${DRIVER}.run
./NVIDIA-Linux-x86_64-${DRIVER}.run --silent

nvidia-smi   # must list your GPUs before continuing
```

Load the modules at boot and create the device nodes:

```bash
cat >> /etc/modules-load.d/nvidia.conf <<'EOF'
nvidia
nvidia_uvm
EOF

cat > /etc/udev/rules.d/70-nvidia.rules <<'EOF'
KERNEL=="nvidia", RUN+="/bin/bash -c '/usr/bin/nvidia-smi -L >/dev/null'"
KERNEL=="nvidia_uvm", RUN+="/bin/bash -c '/usr/bin/nvidia-modprobe -c0 -u'"
EOF
```

> [!IMPORTANT]
> Re-run the host installer after **every PVE kernel upgrade** — the module is built against the
> running kernel and will not survive a kernel change.

---

## 2. Note your GPU device major numbers

The container's cgroup rules need these. **Do not copy the numbers from a guide** — the
`nvidia-uvm` major is assigned dynamically and differs between machines.

```bash
ls -l /dev/nvidia*
```

```
crw-rw-rw- 1 root root 195,   0 /dev/nvidia0          ← major 195
crw-rw-rw- 1 root root 195, 255 /dev/nvidiactl
crw-rw-rw- 1 root root 507,   0 /dev/nvidia-uvm       ← major 507 here, often 234 elsewhere
crw-rw-rw- 1 root root 507,   1 /dev/nvidia-uvm-tools
```

On the verified host `nvidia-uvm` is **507**, while many guides (and older configs on this very
cluster) hardcode **234**. Read your own values.

---

## 3. Create the LXC container

```bash
pct create 200 local:vztmpl/ubuntu-26.04-standard_26.04-1_amd64.tar.zst \
  --hostname yuanyi-node \
  --cores 8 --memory 16384 --swap 512 \
  --rootfs nvme:100 \
  --net0 name=eth0,bridge=vmbr1,ip=10.0.2.200/24,gw=10.0.2.1,type=veth \
  --nameserver 8.8.8.8 \
  --unprivileged 1 \
  --features nesting=1 \
  --ostype ubuntu
```

| Option | Why it matters |
| :--- | :--- |
| `--unprivileged 1` | Recommended. GPU passthrough works fine unprivileged; §5/§6 cover the two extra steps it requires. |
| `--features nesting=1` | **Mandatory** — Docker cannot run inside the container without it. |
| `--rootfs nvme:100` | Allow ~100 GB: the CUDA runtime image alone is over 15 GB, plus model weights. |
| `--memory 16384` | vLLM's host-side buffers need real RAM in addition to VRAM. |

Do **not** start it yet.

---

## 4. Attach the GPU to the container

Append to `/etc/pve/lxc/200.conf`, substituting **your** majors from §2. This snippet detects
them automatically:

```bash
NV_MAJ=$(printf "%d" 0x$(stat -c '%t' /dev/nvidia0))
UVM_MAJ=$(printf "%d" 0x$(stat -c '%t' /dev/nvidia-uvm))
echo "nvidia=$NV_MAJ nvidia-uvm=$UVM_MAJ"

cat >> /etc/pve/lxc/200.conf <<EOF
lxc.cgroup2.devices.allow: c ${NV_MAJ}:* rwm
lxc.cgroup2.devices.allow: c ${UVM_MAJ}:* rwm
lxc.mount.entry: /dev/nvidia0 dev/nvidia0 none bind,optional,create=file
lxc.mount.entry: /dev/nvidiactl dev/nvidiactl none bind,optional,create=file
lxc.mount.entry: /dev/nvidia-uvm dev/nvidia-uvm none bind,optional,create=file
lxc.mount.entry: /dev/nvidia-uvm-tools dev/nvidia-uvm-tools none bind,optional,create=file
EOF
```

**One GPU per container** is the pattern used by the reference cluster: container 101 gets
`/dev/nvidia0`, 102 gets `/dev/nvidia1`, and so on. To give a container several GPUs, add one
`lxc.mount.entry` per device.

> [!NOTE]
> The same `/dev/nvidiaN` may be mounted into multiple containers — LXC does not enforce
> exclusivity. That is useful for sharing, but two vLLM instances on one 8 GB card will exhaust
> VRAM.

Start it and confirm the devices arrived:

```bash
pct start 200
pct exec 200 -- ls -l /dev/nvidia0 /dev/nvidiactl /dev/nvidia-uvm
```

They appear owned by `nobody:nogroup` inside an unprivileged container. That is expected and
does not prevent access.

---

## 5. Install the NVIDIA **userspace** driver inside the container

The container must **not** install a kernel module — the host already provides it. The version
must match the host exactly.

```bash
pct enter 200

apt-get update
apt-get install -y wget kmod pkg-config libglvnd-dev

DRIVER=610.43.02   # must equal the host's `nvidia-smi` version
wget https://us.download.nvidia.com/XFree86/Linux-x86_64/${DRIVER}/NVIDIA-Linux-x86_64-${DRIVER}.run
chmod +x NVIDIA-Linux-x86_64-${DRIVER}.run
./NVIDIA-Linux-x86_64-${DRIVER}.run --no-kernel-module --silent
```

> [!IMPORTANT]
> `--no-kernel-module` is the whole point of this step. Without it the installer tries to build
> a module against a kernel the container does not control, and fails.

Verify — this is the moment GPU passthrough is actually proven:

```bash
nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader
# Quadro RTX 4000, 610.43.02, 8192 MiB
```

---

## 6. Install Docker and the NVIDIA Container Toolkit inside the container

```bash
# Docker Engine
apt-get install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg
. /etc/os-release
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# NVIDIA Container Toolkit
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
  | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
  | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
  > /etc/apt/sources.list.d/nvidia-container-toolkit.list
apt-get update
apt-get install -y nvidia-container-toolkit
nvidia-ctk runtime configure --runtime=docker
```

### ⚠️ The step everyone misses: `no-cgroups = true`

Inside an **unprivileged** LXC container the toolkit cannot manage cgroup device rules — LXC
already did that in §4. Leave this unset and `docker run --gpus all` fails with:

```
nvidia-container-cli: mount error: failed to add device rules: unable to find any existing
device filters attached to the cgroup: bpf_prog_query(BPF_CGROUP_DEVICE) failed: operation not permitted
```

Fix it:

```bash
sed -i 's/^#no-cgroups = false/no-cgroups = true/' /etc/nvidia-container-runtime/config.toml
grep no-cgroups /etc/nvidia-container-runtime/config.toml   # -> no-cgroups = true
systemctl restart docker
```

Now prove the full chain works before deploying anything:

```bash
docker run --rm --gpus all ubuntu:24.04 nvidia-smi --query-gpu=name,driver_version --format=csv,noheader
# Quadro RTX 4000, 610.43.02
```

If that prints your GPU, host → LXC → Docker passthrough is complete.

---

## 7. Download the model

```bash
apt-get install -y git git-lfs
git lfs install
mkdir -p /root/models && cd /root/models
git clone https://huggingface.co/Qwen/Qwen2-VL-2B-Instruct-AWQ     # vision-capable default
```

---

## 8. Install and run the agent

```bash
cd /root
git clone https://github.com/lhu-csie-dclab/yuanyi.git
cd yuanyi
```

Create the private network key — **before** the first launch:

```bash
# New mesh: generate one
printf '/key/swarm/psk/1.0.0/\n/base16/\n%s\n' "$(openssl rand -hex 32)" > swarm.key

# Joining an existing mesh: copy that mesh's key in verbatim instead, then verify
sha256sum swarm.key
```

Every node in a mesh must carry the **byte-identical** key. See
[`docs/install/ubuntu/README.md`](../ubuntu/README.md#6-create-the-private-network-key-swarmkey)
for the full explanation.

Configure `.env`:

```env
ABS_MODEL_PATH=/root/models/Qwen2-VL-2B-Instruct-AWQ
SERVER_ADDRESS=/dns4/your-bootstrap-host/tcp/50004/p2p/12D3KooW...
IFACE=eth0
CLIENT_WEB_PORT=50007
```

Build and launch:

```bash
docker compose up -d --build
docker compose logs -f          # model load takes ~45-90s
```

Verify:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8100/health      # vLLM
curl -s http://127.0.0.1:50006/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"Qwen/Qwen2-VL-2B-Instruct-AWQ","messages":[{"role":"user","content":"Hello"}],"max_tokens":50}'
curl -s http://127.0.0.1:50007/api/peers                                    # mesh peers
```

To scale out, repeat §3-§8 with a new VMID, IP, and a different `/dev/nvidiaN`.

---

## Verification run

This procedure was executed start-to-finish on the live PVE 9.2.2 host, building container 200
from nothing, to confirm each step actually works rather than only reading correctly:

| Step | Observed result |
| :--- | :--- |
| §3 `pct create` (unprivileged, `nesting=1`) | container created |
| §4 device passthrough (auto-detected `195` / `507`) | `/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm` present inside, owned `nobody:nogroup` |
| §5 `--no-kernel-module` driver install | `nvidia-smi` → `Quadro RTX 4000, 610.43.02, 8192 MiB` |
| §6 Docker + toolkit | Docker 29.7.2, Compose v5.5.0, toolkit 1.20.0 |
| §6 **before** `no-cgroups = true` | ❌ `bpf_prog_query(BPF_CGROUP_DEVICE) failed: operation not permitted` |
| §6 **after** `no-cgroups = true` | ✅ `docker run --gpus all` → `Quadro RTX 4000, 610.43.02` |
| §7 model download | 2.5 GB weights fetched |
| §8 `docker compose build` | image built inside the container |
| §8 launch | VRAM climbed 1 MiB → 6318 MiB, vLLM `/health` → `200` |
| §8 inference via gateway | valid completion returned, GPU at **100 %** utilization |
| §8 mesh join | **9 peers discovered** — joined the existing cluster |

The `no-cgroups` row is the reason this guide exists: every other step succeeds without it, and
the failure surfaces only at the last moment as an opaque BPF error.

---

## 9. Troubleshooting

Each layer fails differently — the error tells you which one to fix.

| Error | Layer | Fix |
| :--- | :--- | :--- |
| `nvidia-smi` fails **on the host** | Host driver | §1. Nothing else can work first. Check `nouveau` is blacklisted and the module built. |
| `nvidia-smi: command not found` **in the container** | Container userspace driver | §5 — the userspace driver was never installed. |
| `Failed to initialize NVML: Driver/library version mismatch` | Version skew | Container driver ≠ host driver. Reinstall the container side at the **exact** host version. Typically appears after a host driver upgrade. |
| `No devices were found` in the container, but the host is fine | LXC passthrough | §4 — check `lxc.mount.entry` lines and that the cgroup majors match §2. |
| `bpf_prog_query(BPF_CGROUP_DEVICE) failed: operation not permitted` | Toolkit / cgroups | §6 — set `no-cgroups = true`, restart Docker. The signature error of unprivileged LXC. |
| `could not select device driver "" with capabilities: [[gpu]]` | Toolkit | `nvidia-ctk runtime configure --runtime=docker` was not run, or Docker was not restarted. |
| Docker won't start at all in the container | LXC features | `--features nesting=1` missing. Add to the container config and reboot it. |
| Host driver works, container was fine, breaks after reboot | Host modules | `nvidia_uvm` not auto-loaded. See the `/etc/modules-load.d/nvidia.conf` step in §1. |
| vLLM OOM although the GPU looks idle | VRAM sharing | Another container is holding VRAM on the same `/dev/nvidiaN`. Check with `nvidia-smi` **on the host** — a container only sees its own processes. |

---

## Related documentation

- [`docs/install/ubuntu/README.md`](../ubuntu/README.md) — Ubuntu deployment (applies inside the container)
- [`docs/install/windows/README.md`](../windows/README.md) — native Windows deployment
- [`docs/test/MULTI_NODE_CLONE_TEST.md`](../../test/MULTI_NODE_CLONE_TEST.md) — the 10-node cluster built this way
- [`docs/RUNNER_DOCKER.md`](../../RUNNER_DOCKER.md) — Docker/Ray/vLLM orchestration internals
