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
	"net/http/httputil"
	"net/url"
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
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoreds"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/multiformats/go-multiaddr"
)

// Node roles advertised in GPUInfo.Role. RoleInference is the zero value on purpose:
// nodes running older builds omit the field entirely, and those nodes do serve inference.
const (
	RoleInference = "" // serves inference from a local GPU (default)
	RoleRelay     = "relay"
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
	NodeID string `json:"node_id"`
	Addr   string `json:"addr"`
	// Role advertises what this node contributes. RoleRelay means the node provides
	// network relaying but has no local inference engine, so peers must not dispatch
	// inference to it. An empty value means RoleInference: nodes running older builds
	// omit the field, and they do serve inference, so empty must keep meaning "usable".
	Role          string `json:"role,omitempty"`
	Status        string `json:"status"`
	Timestamp     int64  `json:"timestamp"`
	Summary       string `json:"summary"`
	BootstrapAddr string `json:"bootstrap_addr,omitempty"`
	EngineID      string `json:"engine_id,omitempty"`
	// VLLMPort is the port THIS node's own local vLLM listens on. Peers dispatching
	// a request here must tunnel to this value, not their own vllm.port -- those two
	// only happened to match by convention (both defaulting to 8100) until a node
	// configured a non-default port (e.g. to run alongside another instance),
	// at which point dispatch to it was rejected by the receiving allowlist check
	// in setupStreams. Zero (peers on a pre-fix build) means "unknown, assume 8100".
	VLLMPort int `json:"vllm_port,omitempty"`

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
	seedAddrs     []peer.AddrInfo // bootstrap/hub seeds; also usable as Circuit Relay hops
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
	n.seedAddrs = seedAddrs
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
		// Only recreate the datastore for errors that Truncate:true couldn't already
		// self-heal (that option handles ordinary value-log corruption on open). A
		// held directory lock means another instance of this process is already
		// running against the same peerstore -- deleting it would destroy that
		// other process's data without fixing anything, so fail loudly instead of
		// silently wiping state out from under it.
		if strings.Contains(err.Error(), "Cannot acquire directory lock") {
			return fmt.Errorf("peerstore is locked by another running instance: %v", err)
		}
		msg := fmt.Sprintf("Peerstore open failed (%v), recovering by recreating ./my-peerstore", err)
		n.app.TUI.AddLog("[WARN]", msg)
		logError("[peerstore] %s", msg)
		if rmErr := os.RemoveAll("./my-peerstore"); rmErr != nil {
			return fmt.Errorf("peerstore open failed (%v) and cleanup also failed: %v", err, rmErr)
		}
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

	// An operator-declared announce address overrides self-discovery: see
	// P2PConfig.AnnounceAddr for why a containerized/port-forwarded node cannot work this
	// out for itself. It is folded into the single address factory below rather than added
	// as a second libp2p.AddrsFactory option -- libp2p permits only one and errors with
	// "cannot specify multiple address factories" if given two.
	var announce multiaddr.Multiaddr
	if raw := strings.TrimSpace(n.app.Config.P2P.AnnounceAddr); raw != "" {
		a, err := multiaddr.NewMultiaddr(raw)
		if err != nil {
			return fmt.Errorf("invalid p2p.announce_addr %q: %v", raw, err)
		}
		announce = a
	}

	addrsFactory := filterAdvertisedAddrs
	if announce != nil {
		addrsFactory = func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			// Keep the discovered addresses too: they remain valid for same-LAN peers,
			// and the announced one is only known-better for peers coming from outside.
			return append([]multiaddr.Multiaddr{announce}, filterAdvertisedAddrs(addrs)...)
		}
	}

	opts := []libp2p.Option{
		libp2p.Peerstore(pstore),
		libp2p.PrivateNetwork(psk),
		libp2p.Identity(identity),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.NATPortMap(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
		// Without this, libp2p's identify protocol advertises the raw, unfiltered
		// host.Addrs() list to every peer that connects to us -- on a machine with virtual
		// adapters (Docker bridges, VPN tunnels, ...) that list can and did include
		// loopback and Docker-bridge-gateway addresses, which is what every peer's
		// Peerstore().Addrs() then held for us, and what NewStream() then tried (and
		// failed) to dial. selectBestAddr's fix to our own gossip payload only cleaned up
		// a cosmetic display field; THIS is what actually governs dialability. See
		// filterAdvertisedAddrs for the filtering logic (shared with selectBestAddr).
		libp2p.AddrsFactory(addrsFactory),
	}

	if announce != nil && n.app.Config.P2P.BehindNAT != nil && *n.app.Config.P2P.BehindNAT {
		return fmt.Errorf("p2p.announce_addr and p2p.behind_nat are mutually exclusive: " +
			"announce_addr asserts this node is publicly reachable, behind_nat asserts it is not")
	}

	// nil means auto-detect, which is the default: see P2PConfig.BehindNAT for why this
	// should not be something a home contributor has to configure by hand.
	behindNAT, reason := false, ""
	switch {
	case announce != nil:
		// announce_addr already asserts public reachability below; never also force private.
	case n.app.Config.P2P.BehindNAT != nil:
		behindNAT = *n.app.Config.P2P.BehindNAT
		reason = "set in config"
	default:
		behindNAT = hasNoPublicInterfaceAddr()
		reason = "auto-detected: no public address on any local interface"
	}

	if announce != nil {
		// Asserting reachability is what actually lets a relay behind Docker start its
		// Circuit Relay service at all -- libp2p keeps that service switched off while it
		// believes it is private, which a containerized relay always concludes on its own.
		opts = append(opts, libp2p.ForceReachabilityPublic())
		n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Announcing self as %s (reachability asserted public)", announce))
	} else if behindNAT {
		// Skips AutoNAT's multi-probe ambient detection, which otherwise leaves this node
		// unroutable from outside its LAN for minutes after every start (see
		// P2PConfig.BehindNAT). Forcing reachability emits the reachability event
		// immediately, so AutoRelay requests its circuit reservation right away.
		opts = append(opts, libp2p.ForceReachabilityPrivate())
		n.app.TUI.AddLog("[INFO]", fmt.Sprintf("behind NAT (%s): requesting a relay reservation immediately instead of waiting for AutoNAT", reason))
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

		// ForceReachabilityPublic() used to be set here unconditionally for every
		// server_mode.enabled node. That's wrong for a hub sitting on a private LAN
		// (e.g. a NAT'd GPU box that only *also* happens to run hub services): forcing
		// "public" tells the AutoRelay subsystem this node believes it's already
		// dialable and doesn't need a relay reservation, so it never asks the static
		// relay from EnableAutoRelay above for one. Peers that can't route to its
		// private IP directly then have no path to it at all -- direct dial times out
		// AND the circuit relay attempt fails with NO_RESERVATION, because no
		// reservation was ever requested. Removing the override lets libp2p's own
		// AutoNAT reachability detection decide honestly instead of us asserting it.
		opts = append(opts,
			libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)),
			libp2p.EnableRelayService(relay.WithResources(res)),
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
// keepAlive periodically repairs this node's own connections in the background, so a
// dropped connection (network blip, a peer or relay restarting, ...) is already fixed by
// the time a real request needs it, instead of that request being the first thing to
// discover the problem and fail. Previously this only covered the bootstrap/relay seeds --
// a connection to an ordinary peer that dropped was never proactively redialed at all, so
// recovering it required either a fresh gossip broadcast happening to arrive or a live
// dispatch attempt to trigger a dial (which is the request the operator was already waiting
// on), and in practice that meant an operator restarting the node by hand to "fix" it.
//
// Peers come from TUI.GetPeers(), which already only returns peers seen via gossip in the
// last 90s -- so a genuinely offline peer naturally ages out of this loop on its own within
// that window rather than being redialed forever, no separate bookkeeping needed. Connect
// failures for ordinary peers are deliberately NOT logged (unlike the seed case, this can be
// many peers every cycle, and most transient failures are routine/expected -- logging all of
// them would flood the 300-line ring buffer and bury the WARN/PROXY lines that actually need
// attention, a problem already hit once today with unrelated high-frequency log lines). A
// successful reconnect IS logged: that's the actionable "self-healing just happened" signal.
func (n *NetworkNode) keepAlive(seedAddrs []peer.AddrInfo) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, addrInfo := range seedAddrs {
			if n.host.Network().Connectedness(addrInfo.ID) != network.Connected {
				n.clearDialBackoff(addrInfo.ID)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := n.host.Connect(ctx, addrInfo); err != nil {
					n.app.TUI.AddLog("[WARN]", fmt.Sprintf("Reconnecting to Bootstrap node %s failed: %v", addrInfo.ID, err))
				} else {
					n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Reconnected to Bootstrap node %s", addrInfo.ID))
				}
				cancel()
			}
		}

		for id, info := range n.app.TUI.GetPeers() {
			pid, err := peer.Decode(id)
			if err != nil || pid == n.host.ID() {
				continue
			}
			if n.host.Network().Connectedness(pid) == network.Connected {
				continue
			}
			n.clearDialBackoff(pid)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = n.host.Connect(ctx, peer.AddrInfo{ID: pid})
			cancel()
			if err == nil {
				n.app.TUI.AddLog("[INFO]", fmt.Sprintf("Reconnected to peer %s (%s)", id[:8], info.Summary))
			}
		}
	}
}

// hasNoPublicInterfaceAddr reports whether this machine has no public IP address on any
// local interface, i.e. it cannot be dialed directly from the internet and must go through a
// relay. That is the ordinary situation for a home or office machine: its interfaces carry
// only RFC1918 addresses (192.168.x, 10.x, 172.16-31.x) and the router holds the public one.
//
// Deliberately reads interfaces rather than host.Addrs(): this has to be decided *before*
// libp2p.New, because forcing reachability is a construction-time option, and the whole
// point is to avoid waiting for the host to work it out at runtime.
//
// A NAT'd machine with a port-forward is genuinely reachable yet still has no public
// interface address, so it is detected as NAT'd here. That errs the safe way -- it would use
// a relay it does not strictly need, which works, just with an extra hop -- and an operator
// in that position sets announce_addr, which takes precedence over this entirely. Loopback
// and link-local addresses are ignored; they say nothing about external reachability.
func hasNoPublicInterfaceAddr() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		// Can't tell. Assume not NAT'd and let libp2p's own detection decide, rather than
		// forcing a relay path onto a node that may not need one.
		return false
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			continue
		}
		if !ip.IsPrivate() {
			return false // a genuinely public address exists
		}
	}
	return true
}

// clearDialBackoff drops libp2p's dial-backoff record for one peer, so the reconnect
// attempt that follows actually dials instead of being refused offline.
//
// libp2p backs a peer off after its addresses all fail, for BackoffBase + tries^2 seconds
// (quadratic, capped at 5 minutes), and only clears that record on a *successful* dial.
// While it's in effect, Connect() returns "dial backoff" immediately without touching the
// network -- so a peer that hit a rough patch stays locked out for minutes after the
// network itself has recovered, and the retry loop above cannot heal it, because every
// attempt is refused before it reaches the wire. That is exactly why restarting a node
// "fixed" connectivity during debugging: a restart builds a fresh Swarm whose backoff
// table is empty. This does the same thing without the restart.
//
// Deliberately bounded rather than blanket-disabling backoff: it only runs from the 15s
// keepAlive loop, only for peers that are currently disconnected, and (for the ordinary-peer
// case) only for peers that broadcast gossip within the last 90s -- i.e. peers with recent
// positive proof of life, which are precisely the ones a dial-failure backoff is a false
// positive for. A genuinely dead peer stops gossiping and ages out of that set, so it
// returns to being backed off normally instead of being redialed forever.
func (n *NetworkNode) clearDialBackoff(pid peer.ID) {
	// Network() is a *swarm.Swarm for any host libp2p.New builds, but that isn't part of
	// the network.Network interface contract -- fail soft rather than panicking a node if a
	// future libp2p version returns something else.
	if sw, ok := n.host.Network().(*swarm.Swarm); ok {
		sw.Backoff().Clear(pid)
	}
}

// generateVIP derives a deterministic loopback "virtual IP" from a peer ID: every node in
// the swarm computes the exact same value for the same peer ID (pure function of the ID
// string, nothing machine-specific), so it works as a swarm-wide symbolic alias -- "dial
// this address" means the same thing on any node that has also learned about that peer,
// each running its own local tunnel behind that same address. It is NOT a real network path:
// see startLocalProxyForPeer for what a connection to it actually does.
func generateVIP(peerID string) string {
	hash := sha256.Sum256([]byte(peerID))
	ip3 := (int(hash[0]) % 254) + 1
	ip4 := (int(hash[1]) % 254) + 1
	return fmt.Sprintf("127.0.0.%d:%d", ip3, 8000+ip4)
}

// startLocalProxyForPeer gives external, non-libp2p-aware local processes -- concretely,
// vLLM's own Mooncake KV-transfer connector, configured with kv_role "kv_both" -- a plain
// dialable host:port for a given remote peer, without needing to know that peer's real
// network address or speak libp2p themselves. It is NOT an alternate network path: whether
// the underlying libp2p connection to that peer succeeds still depends on exactly the same
// reachability/relay conditions as any other stream this node opens. This is purely a local
// convenience wrapper around calls this codebase already makes directly elsewhere.
//
// The listener is a plain net/http reverse proxy onto this node's own OpenAI-gateway
// /mooncake_kv/ endpoint (handleKVTunnel in proxy.go), which already correctly proxies
// arbitrary HTTP requests to a peer's local port over a libp2p ProxyProtocolID stream
// (including HTTP keep-alive, multiple requests per connection, etc. -- net/http/httputil
// handles that for free). Earlier this tunneled a single connection directly into
// GPUProtocolID with a raw io.Copy pipe; that handler only exchanges one HTTP request/
// response per libp2p stream and then closes it (see setupStreams), so a persistent local
// TCP connection making more than one request over its lifetime -- normal HTTP/Mooncake
// client behavior -- would break after the first exchange. Routing through the existing
// /mooncake_kv/ handler via a real reverse proxy avoids reimplementing (and
// re-mismatching) that request/response framing a second time.
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

	gatewayPort := n.app.Config.ProxyPort
	mooncakePort := n.app.Config.VLLM.MooncakeBootstrapPort
	pathPrefix := fmt.Sprintf("/mooncake_kv/%s/%d", targetPeerID, mooncakePort)

	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", gatewayPort)}
	rp := httputil.NewSingleHostReverseProxy(target)
	baseDirector := rp.Director
	rp.Director = func(req *http.Request) {
		baseDirector(req)
		req.URL.Path = pathPrefix + req.URL.Path
		req.Host = target.Host
	}
	rp.ErrorLog = nil // avoid stdlib's default os.Stderr logging fighting the TUI's alt-screen

	srv := &http.Server{Handler: rp}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			n.app.TUI.AddLog("[WARN]", fmt.Sprintf("VIP proxy for peer %s stopped: %v", targetPeerID[:8], err))
		}
	}()

	return vip, nil
}

// filterAdvertisedAddrs decides which of this host's addresses are worth telling anyone
// about -- used both as the libp2p.AddrsFactory (governs what the identify protocol
// advertises to every connecting peer, and what host.Addrs() itself returns) and by
// selectBestAddr (picks one for our own gossip payload's display field). host.Addrs()
// enumerates every address libp2p believes it's listening on, in whatever order its
// internal interface enumeration happens to produce -- on a machine with virtual adapters
// (Docker bridges, VPN tunnels, Hyper-V/VirtualBox switches, ...) that order has no relation
// to which address is actually useful to a remote peer, and blindly trusting it was observed
// advertising loopback (127.0.0.1) and Docker bridge gateways (172.17.0.1, 172.18.0.2) --
// each meaningless outside the machine/container that owns it, and exactly the "virtual IP"
// a peer dialing us would fail against.
//
// Loopback/link-local/unspecified addresses are never useful to a remote peer and are
// dropped outright. Everything else is kept (a private-range address is still genuinely
// useful to a same-LAN peer, so it isn't discarded, only deprioritized) with public
// addresses sorted first, since those are the ones most likely to work for a peer outside
// our own network. If filtering would leave nothing at all (e.g. a machine that is
// legitimately loopback-only), the original list is returned unfiltered rather than
// advertising no address at all.
func filterAdvertisedAddrs(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var public, private []multiaddr.Multiaddr
	for _, a := range addrs {
		ipStr, err := a.ValueForProtocol(multiaddr.P_IP4)
		if err != nil {
			ipStr, err = a.ValueForProtocol(multiaddr.P_IP6)
		}
		if err != nil {
			// Not an IP-based address at all (e.g. /p2p-circuit) -- keep it as-is,
			// sorted after direct addresses; it's not a virtual-IP candidate.
			private = append(private, a)
			continue
		}
		ip := net.ParseIP(ipStr)
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			continue
		}
		if ip.IsPrivate() {
			private = append(private, a)
		} else {
			public = append(public, a)
		}
	}
	filtered := append(public, private...)
	if len(filtered) == 0 {
		return addrs
	}
	return filtered
}

// selectBestAddr picks the single multiaddr this node advertises to the swarm as its own
// contact address in our own gossip payload (GPUInfo.Addr) -- a cosmetic display field
// separate from (but filtered the same way as) what libp2p's identify protocol actually
// advertises for dialing; see filterAdvertisedAddrs.
func selectBestAddr(addrs []multiaddr.Multiaddr) multiaddr.Multiaddr {
	filtered := filterAdvertisedAddrs(addrs)
	if len(filtered) == 0 {
		return nil
	}
	return filtered[0]
}

func (n *NetworkNode) gossipPublisher(ctx context.Context, topic *pubsub.Topic) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// An operator-configured announce address wins outright over anything derived
			// from host.Addrs(). It cannot be selected by ordering alone: libp2p sorts the
			// address list after applying AddrsFactory (addrsManager.applyAddrsFactory),
			// so the announce address does not stay at the front where selectBestAddr
			// would find it. That matters beyond cosmetics -- gossipSubscriber feeds this
			// exact field into peers' Peerstores for peers they only know via gossip, so
			// publishing the container-internal address here is what left them holding an
			// address nothing could dial.
			var addr string
			if raw := strings.TrimSpace(n.app.Config.P2P.AnnounceAddr); raw != "" {
				addr = fmt.Sprintf("%s/p2p/%s", raw, n.host.ID().String())
			} else if best := selectBestAddr(n.host.Addrs()); best != nil {
				addr = fmt.Sprintf("%s/p2p/%s", best.String(), n.host.ID().String())
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

			// Relay-only nodes advertise themselves so peers exclude them when choosing
			// where to dispatch inference; they contribute relaying, not GPU capacity.
			role := RoleInference
			if n.app.Config.ServerMode.RelayOnly {
				role = RoleRelay
			}

			info := GPUInfo{
				NodeID:        n.host.ID().String(),
				Addr:          addr,
				Role:          role,
				Status:        "idle",
				Timestamp:     time.Now().Unix(),
				Summary:       summary,
				// A plain, directly-dialable host:port -- not a URL with an embedded path --
				// because this value is handed straight to vLLM's own Mooncake KV-transfer
				// connector (as "mooncake_peer" in proxy.go's P/D-separated dispatch), an
				// external process that dials it as an ordinary socket and knows nothing
				// about this project's /mooncake_kv/ URL scheme. generateVIP(our own ID) is
				// deterministic, so every other node that learns about us (via this same
				// gossip broadcast) independently starts its own local tunnel behind the
				// identical address -- see startLocalProxyForPeer.
				BootstrapAddr: fmt.Sprintf("http://%s", generateVIP(n.host.ID().String())),
				EngineID:      n.host.ID().String(),
				VLLMPort:      n.app.Config.VLLM.Port,

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

		// GossipSub relays a peer's identity+address to nodes it has no direct libp2p
		// connection to (that's the whole point of gossip flooding across relay hops),
		// but the libp2p host's own Peerstore is only populated by actual connections --
		// it never learns about peers this way on its own. Without this, NewStream() to
		// a gossip-only-known peer fails with "no addresses" even though the peer is
		// reachable, which silently breaks remote dispatch for any node that only knows
		// its swarm-mates through gossip (e.g. everything behind a relay/NAT).
		if info.Addr != "" {
			if maddr, err := multiaddr.NewMultiaddr(info.Addr); err == nil {
				if addrInfo, err := peer.AddrInfoFromP2pAddr(maddr); err == nil && addrInfo.ID != n.host.ID() {
					n.host.Peerstore().AddAddrs(addrInfo.ID, addrInfo.Addrs, peerstore.TempAddrTTL)

					// The peer's self-reported address is frequently a private-LAN address
					// (e.g. its own 10.x.x.x) that is only dialable from inside that same
					// LAN -- gossip floods across the whole swarm regardless of network
					// boundary, so most recipients cannot reach it directly. Also register a
					// Circuit Relay v2 hop through each known bootstrap/hub seed (already a
					// live connection, and already configured as a static relay for this
					// host's own reachability via EnableAutoRelay), so the swarm dialer has
					// a real fallback path instead of failing outright with "all dials
					// failed" for every peer that isn't on the local network.
					for _, seed := range n.seedAddrs {
						if seed.ID == addrInfo.ID || seed.ID == n.host.ID() {
							continue
						}
						circuit, cErr := multiaddr.NewMultiaddr("/p2p/" + seed.ID.String() + "/p2p-circuit")
						if cErr == nil {
							n.host.Peerstore().AddAddrs(addrInfo.ID, []multiaddr.Multiaddr{circuit}, peerstore.TempAddrTTL)
						}
					}
				}
			}
		}

		// Hub mode: also persist the broadcast into the local peers.db. Because every
		// hub subscribes to this same network-wide topic, each one converges to the same
		// view independently -- no hub-to-hub replication is needed.
		if n.app.Config.ServerMode.Enabled && n.app.DB != nil {
			serverProcessGossipMessage(info, n.app)
		}

		// Deliberately worded "peer's GPU", not bare "GPU:" -- a relay-only node runs no
		// GPU workload of its own, but every peer's telemetry broadcast still flows through
		// this same log line, so its System Logs fill up with "GPU: Quadro RTX 4000..." at
		// gossip frequency (every ~3s per peer). A user who chose relay-only specifically to
		// avoid GPU usage and then sees "GPU" scrolling through their own node's log reads
		// that as "it's running on GPU after all" -- this node's process/GPU-utilization
		// telemetry says otherwise, but a log line is what people actually watch.
		n.app.TUI.AddLog("[GOSSIP]", fmt.Sprintf("Received broadcast from %s (peer's GPU: %s)", info.NodeID[:8], info.Summary))
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
