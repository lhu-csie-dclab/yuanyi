// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// DockerConfig defines parameters for Docker container execution.
type DockerConfig struct {
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Network       string `json:"network"`
	ShmSize       string `json:"shm_size"`
	Iface         string `json:"iface"`
}

// PathsConfig defines host paths mounted into the container.
type PathsConfig struct {
	ConfigPath   string `json:"config_path"`
	ModelPath    string `json:"model_path"`
	MooncakePath string `json:"mooncake_path"`
}

// VLLMConfig holds vLLM inference engine runtime parameters.
type VLLMConfig struct {
	ModelName                    string  `json:"model_name"`
	MaxModelLen                  int     `json:"max_model_len"`
	GpuMemoryUtilization         float64 `json:"gpu_memory_utilization"`
	Port                         int     `json:"port"`
	TensorParallelSize           int     `json:"tensor_parallel_size"`
	Dtype                        string  `json:"dtype"`
	KVRole                       string  `json:"kv_role"`
	MooncakeBootstrapPort        int     `json:"mooncake_bootstrap_port"`
	MooncakeAbortRequestTimeout  int     `json:"mooncake_abort_request_timeout"`
	AttentionBackend             string  `json:"attention_backend"`
	PlacementGroupBundleStrategy string  `json:"placement_group_bundle_strategy"`
}

// P2PConfig holds configuration for libp2p bootstrap nodes.
type P2PConfig struct {
	// ServerAddress is a single bootstrap multiaddress, kept for backward compatibility.
	ServerAddress string `json:"server_address"`
	// ServerAddresses is the preferred list of bootstrap/hub seed multiaddresses. Any one
	// reachable seed is enough to join the mesh and discover the rest via the DHT, so this
	// list is an entry point rather than a runtime single point of failure.
	ServerAddresses []string `json:"server_addresses,omitempty"`
	// AnnounceAddr is the address other nodes should use to reach THIS node, e.g.
	// "/dns4/relay.example.com/tcp/50004" or "/ip4/203.0.113.7/tcp/50004". Set it when this
	// node is reachable at an address it cannot discover for itself -- the common case is a
	// public relay behind Docker port-mapping or NAT port-forwarding, where every address
	// the process can actually see belongs to a container/private network (172.17.x, 10.x)
	// and is useless to anyone outside it.
	//
	// Setting this asserts "I am genuinely reachable here", which has a second, essential
	// effect: libp2p only runs its Circuit Relay *service* while it believes it is publicly
	// reachable (see relaysvc.RelayManager.reachabilityChanged), and a containerized relay's
	// own AutoNAT probes fail precisely because it advertises those unreachable private
	// addresses -- so it concludes "private" and silently never offers the relay service that
	// NAT'd peers depend on, reporting "protocols not supported: .../circuit/relay/0.2.0/hop"
	// to anyone trying to use it. Declaring the real address here fixes both halves: it is
	// advertised to peers, and reachability is asserted so the relay service actually starts.
	//
	// Leave blank on an ordinary node: libp2p's own detection is correct when a node can see
	// its own addresses, and wrongly forcing "public" on a NAT'd node stops it from
	// requesting the relay reservations it needs.
	AnnounceAddr string `json:"announce_addr,omitempty"`
	// BehindNAT declares that this node is behind NAT and can never be dialed directly from
	// outside its own network -- the normal case for a home machine contributing a GPU.
	//
	// It exists to remove a startup delay, not to change what is possible. libp2p only asks
	// a relay for a circuit reservation once its reachability is known, and working that out
	// by itself (AutoNAT's ambient mode) needs several dial-back probes from other peers,
	// which takes minutes. Until that finishes, nothing outside the LAN can route to this
	// node at all, so a node that restarts is unreachable for that entire window -- measured
	// at roughly three minutes on a real node. Declaring it up front short-circuits the
	// probing (autonat.WithReachability emits the reachability event immediately at startup),
	// so the reservation is requested right away and the node is reachable in seconds.
	//
	// Safe for any genuinely NAT'd node: private addresses are still advertised, so peers on
	// the same LAN keep dialing this node directly, and only public addresses -- which a
	// NAT'd node does not have -- are dropped in favour of relay addresses. Do NOT set it on
	// a node that really is publicly reachable: it would route through a relay unnecessarily.
	// Mutually exclusive with AnnounceAddr, which asserts the opposite.
	//
	// Leave it out of config.json entirely (nil) to auto-detect, which is the default and the
	// right choice for almost everyone: a node whose network interfaces carry no public IP
	// address cannot be dialed directly from outside regardless, so it is treated as NAT'd.
	// Someone contributing a spare GPU from home should not have to know what NAT is, let
	// alone hand-edit a flag, to avoid a multi-minute stall after every restart. Set it
	// explicitly only to override that detection.
	BehindNAT *bool `json:"behind_nat,omitempty"`
}

// ServerModeClusterConfig configures the hub's prefill/decode node allocation.
type ServerModeClusterConfig struct {
	PrefillNodes int `json:"prefill_nodes"` // Dedicated prefill node cap; 0 means PD-Together mode.
	DecodeNodes  int `json:"decode_nodes"`  // Dedicated decode node cap; 0 means PD-Together mode.
}

// ServerModeConfig controls whether this client also acts as a hub node (the merged
// equivalent of the standalone Central Server): maintaining the shared peers/leaderboard
// database, relaying traffic, and serving cluster topology. Disabled by default, so a
// plain client's behavior is unaffected. The hub dashboard itself has no port of its own --
// it is mounted at /hub/ on the client's own web_port (see RegisterHubRoutes in
// server_web.go).
type ServerModeConfig struct {
	Enabled bool `json:"enabled"`
	// RelayOnly makes this node contribute network capacity instead of GPU capacity:
	// it joins the swarm, provides the libp2p Circuit Relay service so NAT'd peers can
	// reach each other, and runs the hub services -- but never starts a local vLLM and
	// never accepts inference work from other nodes. Its own gateway still works: requests
	// sent to it are forwarded to peers that do have GPUs, so a machine with no GPU can
	// still both use and contribute to the swarm.
	//
	// Enabling this implies Enabled, since relaying and hub duties share the same host
	// configuration. LoadOrCreateConfig sets Enabled automatically.
	RelayOnly        bool                    `json:"relay_only"`
	P2PPort          int                     `json:"p2p_port"`
	ProxyPort        int                     `json:"proxy_port"`
	DatabasePath     string                  `json:"database_path"`
	MaxFailCount     int                     `json:"max_fail_count"`
	CheckIntervalSec int                     `json:"check_interval_sec"`
	Cluster          ServerModeClusterConfig `json:"cluster"`
	// FlushIntervalSec controls how often PeerCache (peer_cache.go) batches its in-memory
	// peer state and audit-event queue into one peers.db transaction, instead of one
	// transaction per gossip message (every peer re-announces every 3s network-wide, so
	// per-message writes add up to a steady, unnecessary drip of disk I/O on a hub node).
	FlushIntervalSec int `json:"flush_interval_sec,omitempty"`
	// SnapshotSeedURLs are /hub/api/snapshot URLs of other already-running hubs this node can
	// bulk-fetch peer state from at boot, instead of waiting for gossip to trickle it back in.
	// Tried in order; the first reachable one is enough, mirroring p2p.server_addresses'
	// resilience story. Leave empty (the default) to skip this and rely on gossip alone.
	SnapshotSeedURLs []string `json:"snapshot_seed_urls,omitempty"`
}

// ClientConfig is the top-level configuration structure.
type ClientConfig struct {
	Version    string           `json:"version"`
	WebPort    int              `json:"web_port"`
	ProxyPort  int              `json:"proxy_port"`
	P2P        P2PConfig        `json:"p2p"`
	Docker     DockerConfig     `json:"docker"`
	Paths      PathsConfig      `json:"paths"`
	VLLM       VLLMConfig       `json:"vllm"`
	ServerMode ServerModeConfig `json:"server_mode"`
}

const defaultClientConfigStr = `{
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
    "enabled": false,
    "relay_only": false,
    "p2p_port": 50004,
    "proxy_port": 50008,
    "database_path": "./peers.db",
    "max_fail_count": 3,
    "check_interval_sec": 30,
    "flush_interval_sec": 45,
    "snapshot_seed_urls": [],
    "cluster": {
      "prefill_nodes": 0,
      "decode_nodes": 0
    }
  }
}`

// detectActiveNetworkInterface automatically discovers the active non-loopback network interface.
func detectActiveNetworkInterface() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "eth0"
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		name := iface.Name
		if strings.HasPrefix(name, "eth") || strings.HasPrefix(name, "en") || strings.HasPrefix(name, "wlan") {
			return name
		}
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			addrs, _ := iface.Addrs()
			if len(addrs) > 0 {
				return iface.Name
			}
		}
	}

	return "eth0"
}

// removeCommentLines strips single-line comments starting with "//" from JSON bytes.
func removeCommentLines(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			out = append(out, l)
		}
	}
	return []byte(strings.Join(out, "\n"))
}

// LoadOrCreateConfig loads config.json or creates a default configuration file if not found.
func LoadOrCreateConfig(filename string) (*ClientConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(filename, []byte(defaultClientConfigStr), 0644); err != nil {
				return nil, fmt.Errorf("failed to write default config: %v", err)
			}
			data = []byte(defaultClientConfigStr)
		} else {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
	}

	cleanData := removeCommentLines(data)

	var cfg ClientConfig
	if err := json.Unmarshal(cleanData, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	if cfg.Docker.Iface == "" {
		cfg.Docker.Iface = detectActiveNetworkInterface()
	}

	if cfg.WebPort <= 0 {
		cfg.WebPort = 50007
	}
	if cfg.ProxyPort <= 0 {
		cfg.ProxyPort = 50006
	}
	if cfg.VLLM.Port <= 0 || cfg.VLLM.Port == cfg.ProxyPort {
		cfg.VLLM.Port = 8100
	}
	if cfg.VLLM.MooncakeBootstrapPort <= 0 || cfg.VLLM.MooncakeBootstrapPort == cfg.ProxyPort {
		cfg.VLLM.MooncakeBootstrapPort = 8998
	}

	applyServerModeDefaults(&cfg)

	return &cfg, nil
}

// applyServerModeDefaults fills in hub-mode port and timing defaults, steering clear of
// ports already claimed by the client's own web/proxy/vLLM/Mooncake endpoints.
func applyServerModeDefaults(cfg *ClientConfig) {
	if cfg.ServerMode.MaxFailCount <= 0 {
		cfg.ServerMode.MaxFailCount = 3
	}
	if cfg.ServerMode.CheckIntervalSec <= 0 {
		cfg.ServerMode.CheckIntervalSec = 30
	}
	if cfg.ServerMode.DatabasePath == "" {
		cfg.ServerMode.DatabasePath = "./peers.db"
	}
	if cfg.ServerMode.FlushIntervalSec <= 0 {
		cfg.ServerMode.FlushIntervalSec = 45
	}

	// Relay-only nodes still need the hub subsystems (peer database, relay listener,
	// topology API), so treat relay_only as implying enabled rather than making the
	// operator set two flags and silently doing nothing if they set only one.
	if cfg.ServerMode.RelayOnly && !cfg.ServerMode.Enabled {
		cfg.ServerMode.Enabled = true
	}

	used := map[int]bool{cfg.WebPort: true, cfg.ProxyPort: true, cfg.VLLM.Port: true, cfg.VLLM.MooncakeBootstrapPort: true}
	if cfg.ServerMode.ProxyPort <= 0 || used[cfg.ServerMode.ProxyPort] {
		cfg.ServerMode.ProxyPort = 50008
	}
	used[cfg.ServerMode.ProxyPort] = true

	if cfg.ServerMode.P2PPort <= 0 || used[cfg.ServerMode.P2PPort] {
		cfg.ServerMode.P2PPort = 50004
	}
}
