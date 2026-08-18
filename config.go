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
	ServerAddress string `json:"server_address"`
}

// ClientConfig is the top-level configuration structure.
type ClientConfig struct {
	Version   string       `json:"version"`
	WebPort   int          `json:"web_port"`
	ProxyPort int          `json:"proxy_port"`
	P2P       P2PConfig    `json:"p2p"`
	Docker    DockerConfig `json:"docker"`
	Paths     PathsConfig  `json:"paths"`
	VLLM      VLLMConfig   `json:"vllm"`
}

const defaultClientConfigStr = `{
  "version": "1.0",
  "web_port": 50007,
  "proxy_port": 50006,
  "p2p": {
    "server_address": "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh"
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
    "model_name": "Qwen3-4B-AWQ",
    "max_model_len": 8192,
    "gpu_memory_utilization": 0.75,
    "port": 8100,
    "tensor_parallel_size": 1,
    "dtype": "float16",
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": 8998,
    "mooncake_abort_request_timeout": 15,
    "attention_backend": "FLASH_ATTN",
    "placement_group_bundle_strategy": "SPREAD"
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

	return &cfg, nil
}
