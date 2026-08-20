# Hub Mode Guide (Optional Central Server Merge)

This document specifies **hub mode**, an optional capability added to the client agent that
merges the responsibilities of the standalone Mooncake 2.0 Central Server into this binary.
It is implemented across [`server_db.go`](../server_db.go), [`server_rank.go`](../server_rank.go),
[`server_p2p.go`](../server_p2p.go), [`server_proxy.go`](../server_proxy.go),
[`server_web.go`](../server_web.go) + `web/hub/`, [`logger.go`](../logger.go), and
[`scanGPUlevel.go`](../scanGPUlevel.go).

---

> [!NOTE]
> Hub mode is **disabled by default** (`server_mode.enabled: false`). A plain client's behavior,
> config schema, and network footprint are completely unaffected unless you opt in.

---

## 🧭 What Hub Mode Adds

Any client node can opt into hub mode. A hub node keeps doing everything a normal client does
(running its own vLLM inference, joining the P2P swarm, exposing its own OpenAI gateway) and
additionally takes on the responsibilities that used to require a separate Central Server
process:

- A local SQLite database (`peers.db`) tracking every peer this node has observed: address,
  GPU info, ping health, and cumulative token/request contribution.
- GPU-based contribution scoring and a `top.json` leaderboard, refreshed every 10 seconds.
- A central prefill/decode dispatcher and `/api/cluster_topology` endpoint, scoped to this
  node's own view of the mesh, on `server_mode.proxy_port`.
- A hub-only web dashboard (leaderboard, peer list, audit events, cluster topology) on
  `server_mode.web_port`, separate from the existing client dashboard on `web_port`.
- Circuit Relay v2 service and a fixed listen port on `server_mode.p2p_port`, so peers behind
  NAT can reach the swarm through this node if it is itself publicly reachable.

## 🌐 Multi-Hub Design: No Single Point of Failure

There is no longer a single, fixed "Central Server." Instead, **any number of nodes can run in
hub mode at the same time**. This is intentional, and works without any hub-to-hub replication
protocol:

- Every node — hub or plain client — already subscribes to the same network-wide GossipSub
  topic (`/my-gpu-network/v1/updates`) and receives every peer's periodic broadcast.
- A hub node's only addition is that it also writes what it receives into its own local
  `peers.db`. Because every hub observes the same broadcast stream, each one's database
  independently converges to the same view of the mesh, typically within one gossip interval
  (~3 seconds).
- If one hub goes offline, every other hub keeps serving its own already-converged view.
  Clients simply query whichever hub they are configured to talk to; there is no coordinator
  to fail over.

**Trade-off, stated plainly**: this is *eventually consistent*, not linearizable. Two hubs can
briefly disagree on a peer's `fail_count`/`penalty_points` (each hub pings independently), and
a brand-new hub's leaderboard is empty until the next few gossip broadcasts arrive. Given a
swarm with many nodes, this is judged an acceptable trade-off in exchange for not needing a
consensus protocol (Raft/CRDT) for what is fundamentally telemetry and scoring data.

## 🌱 Bootstrap Seeds vs. Runtime Dependency

`p2p.server_addresses` (plural) replaces the single `p2p.server_address` as the preferred way
to configure bootstrap seeds; the singular field is still read as a fallback for backward
compatibility. A node only needs to successfully dial **one** address in the list — from there,
Kademlia DHT discovery finds the rest of the mesh, including any other hubs. The seed list is
an entry point for a brand-new node, not a dependency the running mesh needs afterward.

A hub node may also be configured with an empty seed list, in which case it is the first/root
seed other nodes point at.

## 🪪 Stable PeerID (`identity.key`)

Previously, the client host was constructed without `libp2p.Identity(...)`, so its PeerID
changed on every restart. Any node — hub or plain client — now loads or generates a persisted
Ed25519 key from `identity.key` and passes it via `libp2p.Identity(...)`, giving it a stable
PeerID across restarts. This matters most for hub nodes, since other nodes may configure their
`server_addresses` to point at a specific hub PeerID long-term.

## ⚙️ Configuration Reference

```json
{
  "p2p": {
    "server_address": "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh",
    "server_addresses": []
  },
  "server_mode": {
    "enabled": false,
    "p2p_port": 50004,
    "web_port": 50005,
    "proxy_port": 50008,
    "database_path": "./peers.db",
    "max_fail_count": 3,
    "check_interval_sec": 30,
    "cluster": {
      "prefill_nodes": 0,
      "decode_nodes": 0
    }
  }
}
```

| Key | Default | Description |
| :--- | :--- | :--- |
| `p2p.server_addresses` | `[]` | Preferred list of bootstrap/hub seed multiaddresses. |
| `server_mode.enabled` | `false` | Turns hub mode on for this node. |
| `server_mode.p2p_port` | `50004` | Fixed libp2p listen port so other nodes can dial in. |
| `server_mode.web_port` | `50005` | Hub-only dashboard HTTP port. |
| `server_mode.proxy_port` | `50008` | Central prefill/decode dispatcher HTTP port. |
| `server_mode.database_path` | `./peers.db` | SQLite database file path. |
| `server_mode.max_fail_count` | `3` | Consecutive ping failures before a peer is flagged offline. |
| `server_mode.check_interval_sec` | `30` | Health-check ping interval, in seconds. |
| `server_mode.cluster.prefill_nodes` / `decode_nodes` | `0` / `0` | Dedicated P/D node caps; both `0` means PD-Together mode. |

`LoadOrCreateConfig` defends `server_mode.web_port`/`proxy_port`/`p2p_port` against colliding
with the client's own `web_port`, `proxy_port`, `vllm.port`, or `vllm.mooncake_bootstrap_port`,
resetting to the defaults above on conflict.

## 🚀 Enabling Hub Mode

Set `server_mode.enabled` to `true` in `config.json` (add the block above if it is missing —
`LoadOrCreateConfig` fills in every other field with the defaults shown). No other changes are
required; on the next start the node downloads the GPU specification database
(`gpu_database.json`, from the same source used by the former Central Server) if it is not
already present, initializes `peers.db`, and starts the additional services.
