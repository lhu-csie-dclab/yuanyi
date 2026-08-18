# P2P 网络与 Swarm Key 密钥生成手册 (简体中文)

本文档详细说明 [`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go) 的 P2P 网络架构与 [`swarm.key`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/swarm.key.example) 密钥生成方法。

---

## 🔑 Swarm Key (`swarm.key`) 生成教程

```bash
echo -e "/key/swarm/psk/1.0.0/\n/base16/" > swarm.key
openssl rand -hex 32 >> swarm.key
```
