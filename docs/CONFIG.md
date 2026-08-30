# Configuration Management & Settings Guide

This document provides a detailed reference for configuration handling in the Yuanyi Client Agent, covering [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go), [`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json), [`.env.example`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/.env.example), and [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json).

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
- **`server_mode` block**: Optional hub mode settings; see [`HUB_MODE.md`](HUB_MODE.md) for the full reference.

---

## ⚙️ `config.json` Field Specification

```json
{
  "version": "1.0",
  "web_port": 50007,
  "proxy_port": 50006,
  "p2p": {
    "server_address": "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh",
    "server_addresses": []
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
    "model_name": "Qwen/Qwen3-4B-AWQ",
    "max_model_len": 16384,
    "max_num_seqs": 32,
    "gpu_memory_utilization": 0.95,
    "port": 8100,
    "tensor_parallel_size": 1,
    "dtype": "float16",
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": 8998,
    "mooncake_abort_request_timeout": 15,
    "attention_backend": "FLASH_ATTN",
    "placement_group_bundle_strategy": "SPREAD"
  },
  "server_mode": {
    "enabled": false
  }
}
```

The optional `p2p.announce_addr` and `p2p.behind_nat` keys are omitted above because an
ordinary node needs neither. If you run the relay other nodes connect to, `announce_addr` is
required — see [Reachability](#-reachability-announce_addr--behind_nat).

### Key Parameters

| Key | Default | Type | Description |
| :--- | :--- | :--- | :--- |
| `web_port` | `50007` | Integer | HTTP port for Web Monitoring Dashboard console & APIs. |
| `proxy_port` | `50006` | Integer | HTTP port for OpenAI-compatible API Gateway. |
| `p2p.server_address` | Multiaddr | String | Single bootstrap seed multiaddress (legacy field, still read as a fallback). |
| `p2p.server_addresses` | `[]` | String[] | Preferred list of bootstrap/hub seed multiaddresses; any one reachable entry is enough to join the mesh. |
| `p2p.announce_addr` | *(unset)* | String | The address other nodes should use to reach **this** node, e.g. `/dns4/relay.example.com/tcp/50004`. **Required when running your own relay behind Docker or port-forwarding** — see [Reachability](#-reachability-announce_addr--behind_nat). |
| `p2p.behind_nat` | *(auto)* | Boolean | Declares this node cannot be dialed from outside its network. Omit it: auto-detection is correct for almost everyone. See [Reachability](#-reachability-announce_addr--behind_nat). |
| `vllm.port` | `8100` | Integer | Local vLLM engine HTTP endpoint. |
| `vllm.gpu_memory_utilization` | `0.95` | Float | Maximum VRAM memory allocation ratio reserved for vLLM & KV cache. |
| `vllm.max_num_seqs` | `32` | Integer | Max concurrent sequences vLLM's scheduler batches at once (`--max-num-seqs`). |
| `vllm.kv_role` | `"kv_both"` | String | P/D disaggregation role: `"kv_prefill"`, `"kv_decode"`, or `"kv_both"`. |
| `vllm.mooncake_bootstrap_port` | `8998` | Integer | Mooncake KV Cache transfer control port. |
| `server_mode.enabled` | `false` | Boolean | Opts this node into hub mode (merged Central Server responsibilities). See [`HUB_MODE.md`](HUB_MODE.md) for the full `server_mode.*` reference. |

---

## 📡 Reachability (`announce_addr` / `behind_nat`)

These two settings tell a node whether the outside world can dial it. **Ordinary nodes need
neither** — leave both unset and detection handles it. They matter when a node's own view of
its network is wrong, which is normal inside Docker.

| Situation | Setting |
| :--- | :--- |
| Home/office machine contributing a GPU | *(nothing — auto-detected)* |
| **You are running the relay/bootstrap node others connect to** | `announce_addr` |
| Auto-detection guessed wrong | `behind_nat: true` / `false` to override |

### Running your own relay: `announce_addr` is required

> [!IMPORTANT]
> A relay in Docker (or behind port-forwarding) **will silently fail to relay** without this,
> while appearing completely healthy in every other way.

libp2p only runs its Circuit Relay *service* while it believes it is publicly reachable. A
containerized relay only ever sees container-internal addresses (`172.17.x`, `172.18.x`), its
self-probes against those fail, so it concludes it is private and quietly declines to relay —
even though it is genuinely reachable from the internet. Peers depending on it then get:

```
error opening hop stream to relay: protocols not supported: [/libp2p/circuit/relay/0.2.0/hop]
```

Setting `announce_addr` to the address the node is *actually* reachable at fixes both halves —
it is advertised to peers, and reachability is asserted so the relay service starts:

```json
"p2p": {
  "server_address": "/dns4/relay.example.com/tcp/50004/p2p/12D3KooW...",
  "announce_addr": "/dns4/relay.example.com/tcp/50004"
}
```

Note the announce address carries **no** `/p2p/<peerID>` suffix — it is an address, not a
full peer reference.

### `behind_nat`: faster rejoin after a restart

Purely a startup optimization; it changes nothing about what is possible. libp2p will not
request a relay reservation until it knows its own reachability, and working that out unaided
takes minutes of probing — during which a restarted node is unreachable from outside its LAN.
Declaring it up front makes the reservation happen in seconds instead.

Omit it. Detection asks whether any local interface has a public IP; if none does, the node is
treated as NAT'd, which is right for essentially every home machine. Set it explicitly only to
override that. It is rejected alongside `announce_addr`, since the two assert opposites.

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
