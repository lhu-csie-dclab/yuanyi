# Hub 模式手册（可选的中央服务器合并能力，简体中文）

本文档说明**Hub 模式**——一项可选能力，把原本独立的 Mooncake 2.0 Central Server 职责合并进
这个 client 可执行文件本身。实现分散在
[`server_db.go`](../../server_db.go)、[`server_rank.go`](../../server_rank.go)、
[`server_p2p.go`](../../server_p2p.go)、[`server_proxy.go`](../../server_proxy.go)、
[`server_web.go`](../../server_web.go)、[`logger.go`](../../logger.go) 与
[`scanGPUlevel.go`](../../scanGPUlevel.go)。Hub 仪表板的 UI 跟 client 仪表板共用同一个 Vue
应用，放在 [`web-ui/`](../../web-ui)（见 [`DASHBOARD_UI.md`](DASHBOARD_UI.md)）；
`server_web.go` 只提供 `/hub/api/*` 这组 JSON 端点。

（完整英文版：[`docs/HUB_MODE.md`](../HUB_MODE.md)）

---

> [!NOTE]
> Hub 模式**默认关闭**（`server_mode.enabled: false`）。除非你自己开启，否则普通 client 的行为、
> 配置文件结构与网络行为完全不受影响。

---

## 🧭 Hub 模式提供什么

任何 client 节点都可以开启 Hub 模式。开启后，这个节点仍会做一般 client 该做的所有事情
（运行自己的 vLLM 推理、加入 P2P Swarm、提供自己的 OpenAI 网关），并额外承担以前需要独立
Central Server 进程才能做的事：

- 一份本机 SQLite 数据库（`peers.db`），追踪这个节点观察到的每一个 Peer：地址、GPU 信息、
  Ping 健康状态、累计 Token/请求贡献度。
- 基于 GPU 硬件的贡献度算分，以及每 10 秒刷新一次的 `top.json` 排行榜。
- 一个中央 Prefill/Decode 派发器与 `/api/cluster_topology` 端点（仅基于这个节点自己观察到的
  Swarm 视图），监听在 `server_mode.proxy_port`。
- Hub 专属的仪表板页面（排行榜、Peer 列表、审计事件、拓扑），就是 client 仪表板同一个 Vue
  SPA 的一部分，跑在同一个 `web_port`——不占额外端口，也不是真的另一个网址，而是前端用
  hash 路由（`/#/hub`、`/#/hub/history`、`/#/hub/leaderboard`）切换页面。侧边栏检测到 Hub
  模式开启后，会自动显示"Cluster (Hub Mode)"分区。
- Circuit Relay v2 中继服务，并固定监听在 `server_mode.p2p_port`——如果这个节点本身公网可达，
  NAT 后方的 Peer 就能通过它连进 Swarm。


## 🔀 纯中继模式（没有 GPU 也能贡献）

把 `server_mode.relay_only` 设为 `true`，就是**贡献网络带宽而不是 GPU 算力**。
适合「网络条件好（尤其有公网 IP）但没有显卡」，或是「不想把自己的 GPU 借给别人用」的情况。

纯中继节点会：

- **完全不跑本机推理**：不会启动 Ray 与 vLLM，因此**根本不需要 GPU**。
- **提供 libp2p Circuit Relay v2 中继服务**，让 NAT 后面的节点能通过它互相连线 —— 这就是贡献本身。
- **同时运行 Hub 服务**（节点数据库、算分、拓扑 API）。`relay_only` 会自动隐含 `enabled`，
  所以你只需要设定这一个开关。
- **仍然可以当作你自己的入口**：`proxy_port` 上的网关照常开启，你发送的请求会被转发给
  有 GPU 的节点。也就是说，一台没有显卡的机器依然可以「同时使用并贡献」这个 Swarm。
- **在广播中标记 `role: "relay"`**，让其他节点在挑选推理目标时自动排除它。
  少了这个标记，别人会把它当成可用节点、发出它根本做不到的工作。

```json
"server_mode": {
  "relay_only": true
}
```

> [!NOTE]
> **做中继不会让你接触到别人的 Prompt 内容。** Circuit Relay v2 转发的是**已加密**的 libp2p 流，
> 安全握手是在两个端点之间端到端建立的，中继者无法解密经过它的内容。
> 这与「执行推理的节点」形成对比 —— 推理必须解密才能执行，详见 [`SECURITY.md`](../SECURITY.md)。
>
> 但你仍然在运行 Hub 服务，那会把其他节点的 IP 地址写进 `peers.db`。
> 请一并参考[用户须知](USER_NOTICE.md)。

**兼容性注意事项**：比这个功能更旧的版本不认得 `role` 字段，仍可能把推理请求发给纯中继节点、
失败后再改派给别人。可以的话请让整个 Swarm 一起升级。
## 🌐 多 Hub 设计：没有单点故障

现在不再有单一、固定的"Central Server"。相反地，**任意数量的节点可以同时开启 Hub 模式**。
这是刻意的设计，而且不需要任何 Hub 之间的数据复制协议：

- 每个节点——不论是不是 Hub——本来就已经订阅同一个全网广播的 GossipSub topic
  （`/my-gpu-network/v1/updates`），会收到每个 Peer 定期发出的广播。
- Hub 节点唯一多做的事，就是把收到的内容也写进自己本机的 `peers.db`。因为每个 Hub 观察到的
  都是同一个广播流，各自的数据库会独立收敛到相同视图，通常一个 gossip 周期（约 3 秒）内就会
  一致。
- 若某个 Hub 离线，其余 Hub 各自已经收敛好的视图完全不受影响，照常对外服务。Client 只是去问
  自己配置要连的那个 Hub，没有协调者需要故障转移。

**权衡，讲清楚**：这是**最终一致**，不是线性一致。两个 Hub 可能短暂对某个 Peer 的
`fail_count`/`penalty_points` 有不同看法（因为各自独立对外 Ping），一个全新的 Hub 在收到接下来
几次 gossip 广播前排行榜也会是空的。在节点数量很多的 Swarm 里，这被判断为可接受的权衡——换来的
是不需要为了本质上只是遥测与算分数据，去实现 Raft/CRDT 这类共识协议。

## 🌱 Bootstrap 种子 vs. 运行期依赖

`p2p.server_addresses`（复数）取代单一的 `p2p.server_address`，成为配置 Bootstrap 种子的推荐
方式；单数字段仍会被读取作为向后兼容 fallback。节点只需要成功连上列表中**任意一个**地址——
之后 Kademlia DHT 发现就会找到 Swarm 里其余所有节点，包含其他 Hub。这份种子列表只是给全新节点
的敲门砖，不是正在运行的 Swarm 之后需要依赖的东西。

Hub 节点也可以配置成空种子列表，这种情况下它就是其他节点要连的第一个/根种子。

## 🪪 稳定 PeerID（`identity.key`）

以前 client 创建 host 时没有带 `libp2p.Identity(...)`，所以每次重启 PeerID 都会更换。现在不论
是不是 Hub，每个节点都会读取或生成一把持久化的 Ed25519 密钥（`identity.key`），通过
`libp2p.Identity(...)` 传入，让 PeerID 跨重启保持稳定。这对 Hub 节点特别重要，因为其他节点可能
会把自己的 `server_addresses` 长期指向某个特定的 Hub PeerID。

## ⚙️ 配置参考

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

| 字段 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `p2p.server_addresses` | `[]` | Bootstrap/Hub 种子节点列表（推荐写法）。 |
| `server_mode.enabled` | `false` | 是否为这个节点开启 Hub 模式。 |
| `server_mode.relay_only` | `false` | 改为贡献中继而非 GPU 推理：不启动本机 vLLM，并广播 `role: "relay"` 让其他节点不要派工作过来。会自动隐含 `enabled`。 |
| `server_mode.p2p_port` | `50004` | 固定 libp2p 监听端口，供其他节点拨入。 |
| `server_mode.proxy_port` | `50008` | 中央 Prefill/Decode 派发器 HTTP 端口。 |
| `server_mode.database_path` | `./peers.db` | SQLite 数据库文件路径。 |
| `server_mode.max_fail_count` | `3` | 连续 Ping 失败几次后标记该 Peer 离线。 |
| `server_mode.check_interval_sec` | `30` | 健康检查 Ping 的轮询间隔秒数。 |
| `server_mode.cluster.prefill_nodes` / `decode_nodes` | `0` / `0` | 专用 P/D 节点数量上限；两者皆 0 代表 PD-Together 模式。 |

Hub 仪表板本身没有自己的 `server_mode.*` 端口——它的页面是同一个 Vue SPA 的一部分，跑在
client 既有的 `web_port`（默认 `50007`）上，用 hash 路由切换而不是真的另一个服务器路径
（只有 `/hub/api/*` 这组 JSON 端点才是真实路径）。`LoadOrCreateConfig` 会防止
`server_mode.proxy_port`/`p2p_port` 跟这个节点
自己的 `web_port`、`proxy_port`、`vllm.port`、`vllm.mooncake_bootstrap_port` 冲突，冲突时会重置
为上面的默认值。

## 🚀 开启 Hub 模式

在 `config.json` 把 `server_mode.enabled` 设为 `true`（若没有这个区块就照上面补上——
`LoadOrCreateConfig` 会自动为其余字段填入默认值）。不需要其他改动；下次启动时，节点会在
GPU 规格数据库（`gpu_database.json`，来源为 [voidful/gpu-info-api](https://github.com/voidful/gpu-info-api)）不存在时自动
下载，初始化 `peers.db`，并启动上述的额外服务。
