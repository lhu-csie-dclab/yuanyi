// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	badger "github.com/ipfs/go-ds-badger"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoreds"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/multiformats/go-multiaddr"
)

const GPUProtocolID = "/gpu-service/1.0.0"
const ProxyProtocolID = "/mooncake-proxy/1.0.0"

const (
	NamespaceDHT    = "/my-gpu-network/v1/gpu-info/"
	TopicGPUUpdates = "/my-gpu-network/v1/updates"
)

// GPUEntry describes a single GPU model and its count, used by hub-mode scoring
// (RankManager.CalculateScore) when GPUInfo.GPUs is populated or derived from Summary.
type GPUEntry struct {
	ID  string `json:"id"`
	Num int    `json:"num"`
}

// GPUInfo describes the telemetry and capacity payload broadcast across the P2P mesh.
type GPUInfo struct {
	NodeID        string `json:"node_id"`
	Addr          string `json:"addr"`
	Status        string `json:"status"`
	Timestamp     int64  `json:"timestamp"`
	Summary       string `json:"summary"`
	BootstrapAddr string `json:"bootstrap_addr,omitempty"`
	EngineID      string `json:"engine_id,omitempty"`

	KVCacheUsage   float64 `json:"kv_cache_usage"`
	ActiveRequests int     `json:"active_requests"`
	PrefillSpeed   float64 `json:"prefill_speed"`
	GenSpeed       float64 `json:"gen_speed"`
	AvgTTFT        float64 `json:"avg_ttft"`

	TotalTokens   int64 `json:"total_tokens,omitempty"`
	InTokens      int64 `json:"in_tokens,omitempty"`
	OutTokens     int64 `json:"out_tokens,omitempty"`
	TotalRequests int64 `json:"total_requests,omitempty"`

	GPUTemp       int     `json:"gpu_temp"`
	GPUUtil       int     `json:"gpu_util"`
	MemUtil       int     `json:"mem_util"`
	VRAMUsed      int     `json:"vram_used"`
	VRAMTotal     int     `json:"vram_total"`
	PowerDraw     float64 `json:"power_draw"`
	PowerLimit    float64 `json:"power_limit"`
	FanSpeed      int     `json:"fan_speed"`
	DriverVersion string  `json:"driver_version"`

	// GPUs and Performance are hub-only fields, populated server-side (from Summary via
	// parseSummaryGPUs) when scoring peers; a plain client leaves them at zero value.
	GPUs        []GPUEntry `json:"gpus,omitempty"`
	Performance int        `json:"Performance,omitempty"`
}

type discoveryNotifee struct {
	n *NetworkNode
}

func (d *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	d.n.app.TUI.AddLog("[INFO]", fmt.Sprintf("mDNS peer discovered: %s", pi.ID))
	d.n.host.Connect(context.Background(), pi)
}

// NetworkNode manages the libp2p Host, Kademlia DHT, PubSub, and peer connectivity.
type NetworkNode struct {
	app           *App
	host          host.Host
	ds            *badger.Datastore
	activeProxies sync.Map
	cancel        context.CancelFunc
}

func NewNetworkNode(app *App) *NetworkNode {
	return &NetworkNode{app: app}
}

// Host returns the underlying libp2p host, used by app.go to start hub-mode services
// that need to share this node's connection.
func (n *NetworkNode) Host() host.Host {
	return n.host
}

// resolveSeedAddrs parses P2P.ServerAddresses (preferred, may list several hub seeds) and
// P2P.ServerAddress (single legacy field, kept for backward compatibility) into AddrInfo
// values, de-duplicating by raw string. The result is only an entry point: once a node
// reaches any one seed, DHT discovery finds the rest of the mesh, so no single seed is a
// runtime dependency.
func (n *NetworkNode) resolveSeedAddrs() ([]peer.AddrInfo, error) {
	var raw []string
	raw = append(raw, n.app.Config.P2P.ServerAddresses...)
	if n.app.Config.P2P.ServerAddress != "" {
		raw = append(raw, n.app.Config.P2P.ServerAddress)
	}

	var infos []peer.AddrInfo
	seen := make(map[string]bool)
	for _, s := range raw {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		maddr, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid server address %q: %v", s, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return nil, err
		}
		infos = append(infos, *info)
	}
	return infos, nil
}

func (n *NetworkNode) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	n.cancel = cancel

	if _, err := os.Stat("./my-peerstore/LOCK"); err == nil {
		os.Remove("./my-peerstore/LOCK")
	}

	seedAddrs, err := n.resolveSeedAddrs()
	if err != nil {
		return err
	}
	if len(seedAddrs) == 0 && !n.app.Config.ServerMode.Enabled {
		// A plain client always needs at least one bootstrap seed to join the mesh.
		// A hub-mode node may start with none: it simply becomes a root seed for others.
		return fmt.Errorf("p2p.server_address(es) is empty in config.json")
	}

	keyFile, err := os.Open("swarm.key")
	if err != nil {
		return fmt.Errorf("failed to open swarm.key: %v", err)
	}
	psk, err := pnet.DecodeV1PSK(keyFile)
	keyFile.Close()
	if err != nil {
		return fmt.Errorf("failed to parse swarm.key: %v", err)
	}

	badgerOpts := badger.DefaultOptions
	badgerOpts.Truncate = true
	ds, err := badger.NewDatastore("./my-peerstore", &badgerOpts)
	if err != nil {
		_ = os.RemoveAll("./my-peerstore")
		ds, err = badger.NewDatastore("./my-peerstore", &badgerOpts)
		if err != nil {
			return err
		}
	}
	n.ds = ds

	pstore, err := pstoreds.NewPeerstore(ctx, ds, pstoreds.DefaultOpts())
	if err != nil {
		return err
	}

	// Use a persisted Ed25519 identity so the PeerID stays stable across restarts. This
	// matters most for hub-mode nodes, which other clients may configure as a long-lived
	// bootstrap seed, but a stable identity is harmless (and generally useful) for plain
	// clients too.
	identity := loadOrGenerateIdentity("identity.key")

	opts := []libp2p.Option{
		libp2p.Peerstore(pstore),
		libp2p.PrivateNetwork(psk),
		libp2p.Identity(identity),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.NATPortMap(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	}
	if len(seedAddrs) > 0 {
		opts = append(opts, libp2p.EnableAutoRelay(autorelay.WithStaticRelays(seedAddrs)))
	}

	if n.app.Config.ServerMode.Enabled {
		// Hub mode: listen on a fixed port so other nodes can dial in, and offer Circuit
		// Relay v2 service. Whether this node is actually reachable as a relay depends on
		// its real network position (a NAT'd hub simply won't be dialable, which is a safe
		// no-op); this is a static, Tailscale-like approximation rather than a dynamic
		// AutoNAT-driven election.
		listenPort := n.app.Config.ServerMode.P2PPort
		if listenPort <= 0 {
			listenPort = 50004
		}
		res := relay.DefaultResources()
		res.MaxReservations = 1024
		res.MaxCircuits = 1024
		res.BufferSize = 1073741824
		res.Limit = nil

		opts = append(opts,
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)),
			libp2p.EnableRelayService(relay.WithResources(res)),
			libp2p.ForceReachabilityPublic(),
		)
	} else {
		opts = append(opts, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return err
	}
	n.host = h

	if n.app.Config.ServerMode.Enabled && n.app.ServerProxy != nil {
		n.app.ServerProxy.host = h
	}

	n.setupStreams()
	go n.keepAlive(seedAddrs)

	mdnsService := mdns.NewMdnsService(h, "my-gpu-discovery-service", &discoveryNotifee{n: n})
	if err := mdnsService.Start(); err != nil {
		return err
	}

	n.app.TUI.AddLog("[INFO]", fmt.Sprintf("P2P Node started successfully, ID: %s", h.ID()))

	kademlia, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		return err
	}
	n.bootstrapNode(ctx, kademlia, seedAddrs)

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return err
	}
	topic, err := ps.Join(TopicGPUUpdates)
	if err != nil {
		return err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return err
	}

	go n.gossipPublisher(ctx, topic)
	go n.gossipSubscriber(ctx, sub)

	StartLocalDispatcher(n.app, h)

	// Hub-mode extras: track connect/disconnect events into the local peers.db and run
	// the periodic health-check ping loop.
	if n.app.Config.ServerMode.Enabled && n.app.DB != nil {
		h.Network().Notify(&ConnNotifee{app: n.app})
		go startServerPingLoop(ctx, n.app, h)
	}

	return nil
}

func (n *NetworkNode) setupStreams() {
	n.host.SetStreamHandler(GPUProtocolID, func(s network.Stream) {
		defer s.Close()
		buf := make([]byte, 1024)
		nRead, err := s.Read(buf)
		if err != nil {
			return
		}
		res := fmt.Sprintf("ACK: %s", string(buf[:nRead]))
		s.Write([]byte(res))

		n.app.TUI.UpdateStats(func(st *Stats) {
			st.requests++
			st.successCount++
		})
	})

	n.host.SetStreamHandler(ProxyProtocolID, func(s network.Stream) {
		defer s.Close()

		var targetPort uint16
		if err := binary.Read(s, binary.BigEndian, &targetPort); err != nil {
			n.app.TUI.AddLog("[ERROR]", fmt.Sprintf("Proxy stream target port read error: %v", err))
			return
		}

		// The mooncake-proxy tunnel exists solely to forward vLLM inference
		// requests and Mooncake KV-transfer bootstrap calls between peers.
		// Any peer that can open a libp2p stream to this node controls
		// targetPort, so without an allowlist a malicious/modified peer
		// could redirect the raw HTTP request to any localhost service
		// (e.g. Ray's unauthenticated dashboard/job-submission API on
		// 8275), turning this into a remote code execution pivot across
		// the whole swarm.
		allowedPorts := map[uint16]bool{
			uint16(n.app.Config.VLLM.Port):                  true,
			uint16(n.app.Config.VLLM.MooncakeBootstrapPort): true,
		}
		if !allowedPorts[targetPort] {
			// Logged through both channels deliberately: TUI.AddLog is an in-memory
			// ring buffer (lost on restart, not visible outside the dashboard),
			// while logError goes to stdout/slog so `docker logs`/the host's log
			// driver retains a durable, restart-surviving audit trail of rejected
			// proxy-tunnel attempts and which peer made them.
			msg := fmt.Sprintf("Rejected proxy stream from %s to disallowed local port %d", s.Conn().RemotePeer(), targetPort)
			n.app.TUI.AddLog("[WARN]", msg)
			logError("[security] %s", msg)
			errResp := &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf("target port %d is not allowed", targetPort))),
			}
			errResp.Write(s)
			return
		}

		req, err := http.ReadRequest(bufio.NewReader(s))
		if err != nil {
			return
		}

		req.URL.Scheme = "http"
		req.URL.Host = fmt.Sprintf("127.0.0.1:%d", targetPort)
		req.RequestURI = ""

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			errResp := &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf("Failed to reach local port %d: %v", targetPort, err))),
			}
			errResp.Write(s)
			return
		}
		defer resp.Body.Close()

		resp.Write(s)
	})
}

// bootstrapNode joins the Kademlia DHT and connects to every configured seed address.
func (n *NetworkNode) bootstrapNode(ctx context.Context, kademlia *dht.IpfsDHT, seedAddrs []peer.AddrInfo) {
	if err := kademlia.Bootstrap(ctx); err != nil {
		n.app.TUI.AddLog("[ERROR]", fmt.Sprintf("DHT Bootstrap failed: %v", err))
	}

	for _, addrInfo := range seedAddrs {
		if err := n.host.Connect(ctx, addrInfo); err != nil {
			n.app.TUI.AddLog("[WARN]", fmt.Sprintf("Failed to connect to Bootstrap node %s: %v", addrInfo.ID, err))
		} else {
			n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Connected to Bootstrap node %s and joined DHT", addrInfo.ID))
		}
	}
}

// keepAlive periodically re-dials any configured seed that is not currently connected.
func (n *NetworkNode) keepAlive(seedAddrs []peer.AddrInfo) {
	if len(seedAddrs) == 0 {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, addrInfo := range seedAddrs {
			if n.host.Network().Connectedness(addrInfo.ID) != network.Connected {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := n.host.Connect(ctx, addrInfo); err != nil {
					n.app.TUI.AddLog("[WARN]", fmt.Sprintf("Reconnecting to Bootstrap node %s failed: %v", addrInfo.ID, err))
				} else {
					n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Reconnected to Bootstrap node %s", addrInfo.ID))
				}
				cancel()
			}
		}
	}
}

func generateVIP(peerID string) string {
	hash := sha256.Sum256([]byte(peerID))
	ip3 := (int(hash[0]) % 254) + 1
	ip4 := (int(hash[1]) % 254) + 1
	return fmt.Sprintf("127.0.0.%d:%d", ip3, 8000+ip4)
}

func (n *NetworkNode) startLocalProxyForPeer(targetPeerID string) (string, error) {
	vip := generateVIP(targetPeerID)
	if _, loaded := n.activeProxies.LoadOrStore(targetPeerID, vip); loaded {
		return vip, nil
	}

	listener, err := net.Listen("tcp", vip)
	if err != nil {
		n.activeProxies.Delete(targetPeerID)
		return "", err
	}

	n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Created local VIP proxy for peer %s -> %s", targetPeerID[:8], vip))

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				pid, err := peer.Decode(targetPeerID)
				if err != nil {
					return
				}
				stream, err := n.host.NewStream(context.Background(), pid, GPUProtocolID)
				if err != nil {
					return
				}
				defer stream.Close()

				go io.Copy(stream, c)
				io.Copy(c, stream)
			}(conn)
		}
	}()

	return vip, nil
}

func (n *NetworkNode) gossipPublisher(ctx context.Context, topic *pubsub.Topic) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var addr string
			addrs := n.host.Addrs()
			if len(addrs) > 0 {
				addr = fmt.Sprintf("%s/p2p/%s", addrs[0].String(), n.host.ID().String())
			}

			summary := "No GPU Detected"
			if n.app.Sys != nil {
				sums := n.app.Sys.GetGPUModelSummary()
				if len(sums) > 0 {
					summary = sums[0]
				}
			}

			if n.app != nil && n.app.TUI != nil {
				n.app.TUI.UpdateStats(func(st *Stats) {
					st.peers = len(n.host.Network().Peers())
					st.gpuSummary = summary
				})
			}

			var vm VLLMMetrics
			if n.app.Sys != nil {
				vm = n.app.Sys.GetMetrics()
			}

			var tele GPUTelemetry
			if n.app.Sys != nil {
				tele = n.app.Sys.GetGPUTelemetry()
			}

			var localStats map[string]interface{}
			if n.app != nil && n.app.TUI != nil {
				localStats = n.app.TUI.GetLocalStats()
			}

			totTok, _ := localStats["total_tokens"].(int64)
			inTok, _ := localStats["in_tokens"].(int64)
			outTok, _ := localStats["out_tokens"].(int64)
			totReq, _ := localStats["total_requests"].(int64)

			info := GPUInfo{
				NodeID:        n.host.ID().String(),
				Addr:          addr,
				Status:        "idle",
				Timestamp:     time.Now().Unix(),
				Summary:       summary,
				BootstrapAddr: fmt.Sprintf("http://127.0.0.1:%d/mooncake_kv/%s/%d", n.app.Config.ProxyPort, n.host.ID().String(), n.app.Config.VLLM.MooncakeBootstrapPort),
				EngineID:      n.host.ID().String(),

				KVCacheUsage:   vm.KVCacheUsage,
				ActiveRequests: vm.ActiveRequests,
				PrefillSpeed:   vm.PrefillSpeed,
				GenSpeed:       vm.GenSpeed,
				AvgTTFT:        vm.AvgTTFT,

				TotalTokens:   totTok,
				InTokens:      inTok,
				OutTokens:     outTok,
				TotalRequests: totReq,

				GPUTemp:       tele.GPUTemp,
				GPUUtil:       tele.GPUUtil,
				MemUtil:       tele.MemUtil,
				VRAMUsed:      tele.VRAMUsed,
				VRAMTotal:     tele.VRAMTotal,
				PowerDraw:     tele.PowerDraw,
				PowerLimit:    tele.PowerLimit,
				FanSpeed:      tele.FanSpeed,
				DriverVersion: tele.DriverVersion,
			}

			data, err := json.Marshal(info)
			if err != nil {
				continue
			}
			topic.Publish(ctx, data)
		}
	}
}

func (n *NetworkNode) gossipSubscriber(ctx context.Context, sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.host.ID() {
			continue
		}

		var info GPUInfo
		if err := json.Unmarshal(msg.Data, &info); err != nil {
			continue
		}

		n.app.TUI.RecordPeerInfo(info)
		n.startLocalProxyForPeer(info.NodeID)

		// Hub mode: also persist the broadcast into the local peers.db. Because every
		// hub subscribes to this same network-wide topic, each one converges to the same
		// view independently -- no hub-to-hub replication is needed.
		if n.app.Config.ServerMode.Enabled && n.app.DB != nil {
			serverProcessGossipMessage(info, n.app)
		}

		n.app.TUI.AddLog("[GOSSIP]", fmt.Sprintf("Received broadcast from %s - GPU: %s", info.NodeID[:8], info.Summary))
	}
}

func (n *NetworkNode) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	if n.host != nil {
		n.host.Close()
	}
	if n.ds != nil {
		n.ds.Close()
	}
}
