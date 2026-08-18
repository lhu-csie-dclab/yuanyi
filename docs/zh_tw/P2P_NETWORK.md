# P2P 網路與 Swarm Key 金鑰生成手冊

本文件詳細說明 [`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go) 的 P2P 網路架構與 [`swarm.key`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/swarm.key.example) 金鑰生成方法。

---

## 🔑 Swarm Key (`swarm.key`) 生成教學

Mooncake 2.0 採用 libp2p 私有網路 (PSK)，要求參與 Swarm 叢集節點必須持有相符的 `swarm.key`。

### 方法 1: 透過 OpenSSL / Linux Shell 生成（推薦）

```bash
echo -e "/key/swarm/psk/1.0.0/\n/base16/" > swarm.key
openssl rand -hex 32 >> swarm.key
```

### 方法 2: 透過 `go-libp2p-keygen`

```bash
go install github.com/libp2p/go-libp2p-keygen/ipfs-swarm-key-gen@latest
ipfs-swarm-key-gen > swarm.key
```

---

## 🌐 核心服務與協定

- **`/gpu-service/1.0.0`**：節點心跳與 Ping/Pong 檢測協定。
- **`/mooncake-proxy/1.0.0`**：HTTP-over-libp2p 代理與 KV 傳輸通道。
- **本地 TCP VIP 代理 (`127.0.0.X:80Y`)**：透過 `generateVIP` 計算 PeerID 的 SHA-256 雜湊，產生本機迴路 VIP 位址。
