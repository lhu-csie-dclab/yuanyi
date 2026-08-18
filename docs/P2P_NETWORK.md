# P2P Mesh Network & Swarm Key Guide

This document provides a comprehensive specification of the P2P networking layer implemented in [`p2p.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/p2p.go), Badger Peerstore persistence, Kademlia DHT discovery, GossipSub broadcasting, TCP VIP proxies, and instructions for generating [`swarm.key`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/swarm.key.example).

---

## 🔑 Swarm Key (`swarm.key`) Generation Guide

Mooncake 2.0 uses a libp2p Private Network (PSK) to ensure that only authorized nodes possessing a valid `swarm.key` can discover and communicate within the P2P inference mesh.

### `swarm.key` File Format

A valid libp2p `swarm.key` consists of a 7-byte header followed by a 32-byte (64 hex characters) random pre-shared secret key:

```text
/key/swarm/psk/1.0.0/
/base16/
0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

### Method 1: Generate via OpenSSL / Linux Shell (Recommended)

Generate a new secret key directly using standard Linux utility tools:

```bash
# Create swarm.key with standard header and 32 random hex bytes
echo -e "/key/swarm/psk/1.0.0/\n/base16/" > swarm.key
openssl rand -hex 32 >> swarm.key
```

Or using `/dev/urandom`:

```bash
echo -e "/key/swarm/psk/1.0.0/\n/base16/" > swarm.key
head -c 32 /dev/urandom | xxd -ps -c 32 >> swarm.key
```

### Method 2: Generate via `go-libp2p-keygen`

Install and run official libp2p keygen tool:

```bash
# Install keygen utility
go install github.com/libp2p/go-libp2p-keygen/ipfs-swarm-key-gen@latest

# Generate swarm.key
ipfs-swarm-key-gen > swarm.key
```

> [!CAUTION]
> **Security Notice**: Never commit `swarm.key` or `identity.key` to Git. Keep `swarm.key` secret and distribute it securely to participating swarm nodes.

---

## 🌐 libp2p Network Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             NetworkNode (p2p.go)                            │
│                                                                             │
│  ┌───────────────────────────────┐     ┌─────────────────────────────────┐  │
│  │ Private Network (PSK / Key)   │     │ Kademlia DHT (ModeServer)       │  │
│  └──────────────┬────────────────┘     └────────────────┬────────────────┘  │
│                 │                                       │                   │
│  ┌──────────────▼────────────────┐     ┌────────────────▼────────────────┐  │
│  │ AutoRelay & HolePunching      │     │ GossipSub (/v1/updates)         │  │
│  └──────────────┬────────────────┘     └────────────────┬────────────────┘  │
│                 │                                       │                   │
│  ┌──────────────▼────────────────┐     ┌────────────────▼────────────────┐  │
│  │ Badger DS Peerstore           │     │ Local TCP VIP Proxy Generator   │  │
│  └───────────────────────────────┘     └─────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🛠️ Protocols & Services

### 1. `/gpu-service/1.0.0`
Ping/Pong health checking protocol. Used to verify connection latency and active peer responsiveness.

### 2. `/mooncake-proxy/1.0.0`
HTTP-over-libp2p tunnel protocol. Enables proxying inference requests and Mooncake KV Cache transfer streams across NAT firewalls.
- **Header**: 2-byte BigEndian target port integer.
- **Body**: Standard HTTP request stream proxied directly to local port.

---

## 🖥️ Local TCP VIP Proxy Algorithm (`generateVIP`)

To allow standard TCP applications to connect to remote P2P peers without libp2p dependencies, `p2p.go` generates deterministic local loopback VIP addresses based on the SHA-256 hash of the target `PeerID`:

```go
func generateVIP(peerID string) string {
	hash := sha256.Sum256([]byte(peerID))
	ip3 := (int(hash[0]) % 254) + 1
	ip4 := (int(hash[1]) % 254) + 1
	return fmt.Sprintf("127.0.0.%d:%d", ip3, 8000+ip4)
}
```

`startLocalProxyForPeer` listens on `127.0.0.X:80Y` and full-duplex pipes data (`io.Copy`) over libp2p streams.

---

## 📢 GossipSub Status Broadcasting (`GPUInfo`)

Every 3 seconds, `gossipPublisher` broadcasts a JSON `GPUInfo` payload containing:
- Node ID & Multiaddress
- GPU model summary, core temp (℃), utilization (%), and power draw (W)
- vLLM metrics: Prefill speed, Gen speed, TTFT, and active request queue
- Cumulative processed token statistics
