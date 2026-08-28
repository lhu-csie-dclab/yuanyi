# 配置管理与参数技术手册 (简体中文)

本文档为 Yuanyi Client Agent 的配置管理提供详细说明，涵盖 [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)、[`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json) 与 [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json)。

---

> [!WARNING]
> **生产环境部署警告与未测试参数声明**：
> 本软件目前处于**实验研究阶段**，**不推荐在正式生产环境 (Production) 部署**。目前仅基准设置（`Qwen3-4B-AWQ`, `protocol: "tcp"`）经测试验证，其余未经测试的参数可能产生不可预期的行为。

---

## 🛰️ `server_mode`（可选 Hub 模式）

```json
{
  "server_mode": {
    "enabled": false
  }
}
```

`server_mode.enabled` 默认为 `false`，开启后这个节点会兼任 Hub（合并原本独立中央服务器的能力：
节点/排行榜数据库、GPU 算分、中央派发器、Hub 专属仪表板）。完整字段说明见
[`HUB_MODE.md`](HUB_MODE.md)。

---

## 📡 对外可达性（`announce_addr` / `behind_nat`）

这两个选项用来告诉节点“外界能不能连到我”。**普通节点两个都不用设**，留空即可自动检测。
只有在节点对自己网络环境的判断会出错时才需要——在 Docker 里这是常态。

### 自建中继节点：`announce_addr` 是必填

> [!IMPORTANT]
> 跑在 Docker（或经过端口转发）的中继节点，**不设这个就会安静地失去中继功能**，
> 但其他地方看起来都完全正常，非常难察觉。

libp2p 只有在“认为自己对外可达”时才会启动 Circuit Relay 服务。容器里的中继节点只能看到
容器内部地址（`172.17.x`、`172.18.x`），自我探测必然失败，于是判定自己是私有地址而默默不提供
中继服务——即使它其实从外网完全连得到。依赖它的节点就会收到：

```
error opening hop stream to relay: protocols not supported: [/libp2p/circuit/relay/0.2.0/hop]
```

把 `announce_addr` 设成该节点**实际**可达的地址即可同时解决两件事：这个地址会被广播给其他
节点，并且可达性被明确声明，中继服务才会真正启动：

```json
"p2p": {
  "server_address": "/dns4/relay.example.com/tcp/50004/p2p/12D3KooW...",
  "announce_addr": "/dns4/relay.example.com/tcp/50004"
}
```

注意 announce 地址**不带** `/p2p/<peerID>` 后缀——它是一个地址，不是完整的节点引用。

### `behind_nat`：让重启后更快重新加入

这纯粹是启动速度的优化，不改变任何原本做得到或做不到的事。libp2p 必须先确定自己的可达性才会
去向中继申请 reservation，而自行判断需要数分钟的探测——这段期间重启过的节点在自己局域网外是连
不到的。事先声明可以把这段缩短到数秒。

建议留空。自动检测会检查本机网卡是否有公网 IP；没有就视为在 NAT 后面，这对几乎所有家用机器都是
正确的。只有在要覆盖这个判断时才明确设置。它与 `announce_addr` 互斥（两者的主张相反），同时设置
会在启动时直接报错。

---

## 🍰 `mooncake.json` 传输协议设置 (`protocol: "tcp"`)

```json
{
  "metadata_server": "P2PHANDSHAKE",
  "global_segment_size": "0",
  "local_buffer_size": "17179869184",
  "protocol": "tcp",
  "device_name": ""
}
```
