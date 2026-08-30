# Hub Mode Guide (Optional Central Server Merge)

This document specifies **hub mode**, an optional capability added to the client agent that
merges the responsibilities of the standalone Yuanyi Central Server into this binary.
It is implemented across [`server_db.go`](../server_db.go), [`server_rank.go`](../server_rank.go),
[`server_p2p.go`](../server_p2p.go), [`server_proxy.go`](../server_proxy.go),
[`server_web.go`](../server_web.go), [`logger.go`](../logger.go), and
[`scanGPUlevel.go`](../scanGPUlevel.go). The hub dashboard's UI lives alongside the client
dashboard's in the shared Vue app at [`web-ui/`](../web-ui) (see
[`DASHBOARD_UI.md`](DASHBOARD_UI.md)); `server_web.go` only provides its `/hub/api/*` JSON
endpoints.

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
- Hub-only dashboard views (leaderboard, peer list, audit events, cluster topology) inside the
  same Vue SPA the client dashboard already serves on `web_port` — no separate port, and no
  separate page: they're reached client-side via hash routes (`/#/hub`, `/#/hub/history`,
  `/#/hub/leaderboard`). The sidebar reveals a "Cluster (Hub Mode)" section once it detects hub
  mode is enabled.
- Circuit Relay v2 service and a fixed listen port on `server_mode.p2p_port`, so peers behind
  NAT can reach the swarm through this node if it is itself publicly reachable.


## 🔀 Relay-Only Mode (contribute without a GPU)

Set `server_mode.relay_only: true` to contribute **network capacity instead of GPU capacity**.
This is useful if you have a well-connected machine (especially a public IP) but no GPU, or you
simply do not want your GPU used by others.

A relay-only node:

- **Runs no local inference.** Ray and vLLM are never started, so **no GPU is required at all**.
- **Provides the libp2p Circuit Relay v2 service**, letting NAT'd peers reach each other through
  it. That is the contribution.
- **Runs the hub services** (peer database, scoring, topology API) — `relay_only` implies
  `enabled`, so you only need to set the one flag.
- **Still works as your own entry point.** Its gateway on `proxy_port` stays open; requests you
  send to it are forwarded to peers that do have GPUs. A GPU-less machine can therefore both
  use and contribute to the swarm.
- **Advertises `role: "relay"`** in its gossip broadcast, so other nodes exclude it when
  choosing where to dispatch inference. Without this they would send it work it cannot do.

```json
"server_mode": {
  "relay_only": true
}
```

> [!NOTE]
> **Relaying does not expose you to other people's prompt content.** Circuit Relay v2 forwards
> the *encrypted* libp2p stream; the security handshake is end-to-end between the two peers, so
> a relay cannot read what passes through it. Contrast this with running an inference node,
> where prompts must be decrypted to be executed — see [`SECURITY.md`](SECURITY.md).
>
> You are still running hub services, which store peers' IP addresses in `peers.db`. See the
> [User Notice](USER_NOTICE.md).

**Interoperability caveat:** peers running builds older than this feature do not understand
`role`, so they may still try to dispatch inference to a relay-only node and fail over to
another peer. Upgrade the swarm together where possible.
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
| `server_mode.relay_only` | `false` | Contribute relaying instead of GPU inference: no local vLLM, advertises `role: "relay"` so peers do not dispatch work here. Implies `enabled`. |
| `server_mode.p2p_port` | `50004` | Fixed libp2p listen port so other nodes can dial in. |
| `server_mode.proxy_port` | `50008` | Central prefill/decode dispatcher HTTP port. |
| `server_mode.database_path` | `./peers.db` | SQLite database file path. |
| `server_mode.max_fail_count` | `3` | Consecutive ping failures before a peer is flagged offline. |
| `server_mode.check_interval_sec` | `30` | Health-check ping interval, in seconds. |
| `server_mode.cluster.prefill_nodes` / `decode_nodes` | `0` / `0` | Dedicated P/D node caps; both `0` means PD-Together mode. |

### Ports

| Port | Must be reachable by | If it is blocked |
| :--- | :--- | :--- |
| `server_mode.p2p_port` (50004) | every node | Cannot bootstrap into the swarm. |
| `server_mode.proxy_port` (50008) | localhost only | Nothing — see below. |
| `web_port` (50007) | operators only | Dashboard unreachable. |

**The hub API needs no exposed port.** `/hub/api/*` is served to other nodes over libp2p
(`/hub-api/1.0.0`, see `HubAPIProtocolID` in `p2p.go`), so `proxy_port` can safely bind to
localhost. This also means the API inherits the swarm's own access control: the PSK in
`swarm.key` gates who can join the private network at all, so anything able to open that stream
is already a member — no keys to distribute, and no unauthenticated port facing the internet.

Browsers cannot speak libp2p, so the dashboard's hub pages call `/hub/api/*` on **their own
node's** `web_port`, which fetches the answer from the hub over libp2p (see 步驟 4.2 in
`web.go`). A useful side effect: any node's dashboard can display hub data, not just the hub's
own.

The exception is `/hub/api/debug/*`. Those mutate cluster state, so they are deliberately not
served over libp2p — every node holds the same `swarm.key`, which makes swarm membership a far
weaker boundary than "an operator with access to this machine". They only answer on the hub's
own local HTTP listener.

To confirm topology sync is working, look for `[SYNC]` lines in a node's logs: a healthy node
logs `同步 Server P/D 拓樸成功` every 10s, and every failure path logs its reason.

### Resetting cumulative stats

Contribution counters (`total_requests`, `total_tokens`, `contribution_score`) only ever grow,
and the hub merges them from gossip with a keep-max rule. A value inflated by a past accounting
bug therefore survives forever unless it is cleared at the source first — **order matters**:

```sh
# 1. every node that reports an inflated total, one at a time
curl -X POST http://<node>:50007/api/stats/reset

# 2. only then the hub
curl -X POST http://<hub>:50008/hub/api/debug/reset_stats
```

Reversing the order accomplishes nothing: the next gossip round (~3s) re-merges whatever the
nodes are still broadcasting. Note also that token counters re-sync from vLLM's own Prometheus
totals within ~2s, so clearing those for real needs a vLLM restart too; request counters have no
such external source and stay cleared.

The hub dashboard itself has no `server_mode.*` port of its own for its UI — its views are part
of the same Vue SPA served on the client's existing `web_port` (default `50007`), reached via
hash routes rather than a real server path. Its JSON calls are same-origin against that same
`web_port`, which relays them to the hub over libp2p as described above.
`LoadOrCreateConfig` defends
`server_mode.proxy_port`/`p2p_port` against colliding with the client's own `web_port`,
`proxy_port`, `vllm.port`, or `vllm.mooncake_bootstrap_port`, resetting to the defaults above
on conflict.

## 🚀 Enabling Hub Mode

Set `server_mode.enabled` to `true` in `config.json` (add the block above if it is missing —
`LoadOrCreateConfig` fills in every other field with the defaults shown). No other changes are
required; on the next start the node downloads the GPU specification database
(`gpu_database.json`, fetched from [voidful/gpu-info-api](https://github.com/voidful/gpu-info-api))
if it is not already present, initializes `peers.db`, and starts the additional services.
