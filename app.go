// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
)

// App is the central master application container that orchestrates all subsystems.
type App struct {
	Config *ClientConfig
	TUI    *TUI
	Sys    *SysMonitor
	P2P    *NetworkNode
	Runner *Runner

	// Hub-mode subsystems, present only when config.json's server_mode.enabled is true.
	// A node running in hub mode still performs its own inference like any other client;
	// it additionally maintains the shared peers/leaderboard database, relays traffic for
	// peers behind NAT, and serves the cluster topology other nodes poll.
	DB          *DBManager
	PeerCache   *PeerCache
	Rank        *RankManager
	ServerProxy *ProxyServer
}

// NewApp instantiates the App container and initializes its core subsystems.
func NewApp(cfg *ClientConfig) *App {
	app := &App{Config: cfg}
	app.TUI = NewTUI(app)
	app.Sys = NewSysMonitor(app)
	app.P2P = NewNetworkNode(app)
	app.Runner = NewRunner(app)

	if cfg.ServerMode.Enabled {
		db, err := NewDBManager(cfg.ServerMode.DatabasePath)
		if err != nil {
			app.TUI.AddLog("[ERROR]", fmt.Sprintf("Hub mode database init failed, hub features disabled: %v", err))
		} else {
			app.DB = db
			app.PeerCache = NewPeerCache()
			// Warm-start from whatever was already on disk so the cache is never empty on
			// restart, even before any seed-snapshot fetch or gossip message arrives.
			if existing, err := db.GetAllPeers(); err == nil {
				app.PeerCache.LoadSnapshot(existing)
			}
			app.Rank = NewRankManager(app.PeerCache)
			app.ServerProxy = NewProxyServer(app)
		}
	}

	return app
}

// Start launches hardware monitoring, P2P networking, vLLM runner, Web UI, hub services
// (if enabled), and TUI in sequence.
func (a *App) Start(ctx context.Context) error {
	a.Sys.Start()

	if a.Config.ServerMode.Enabled && a.DB != nil {
		// Best-effort bulk catch-up from a configured seed hub, before gossip processing
		// starts, so gossip always layers on top of the best available baseline rather than
		// racing the fetch.
		syncPeerCacheFromSeed(a)
	}

	if err := a.P2P.Start(ctx); err != nil {
		return fmt.Errorf("P2P start failed: %w", err)
	}

	// A relay-only node contributes network capacity rather than GPU capacity, so it never
	// starts Ray/vLLM -- that is what lets it run on a machine with no GPU at all. Its
	// gateway still runs (see proxy.go): requests sent to it are forwarded to peers that
	// do have GPUs, so the operator can still use the swarm.
	if a.Config.ServerMode.RelayOnly {
		a.TUI.AddVLLMLog("[System] Relay-only mode: skipping local Ray/vLLM startup (no GPU required).")
		a.TUI.AddLog("[INFO]", "Relay-only mode: relaying + hub services only; no local inference is served.")
	} else {
		go a.Runner.Start(ctx)
	}
	go StartClientWebDashboard(a)

	if a.Config.ServerMode.Enabled && a.DB != nil {
		go a.Rank.Start()
		go a.PeerCache.StartFlusher(a.DB, a.Config.ServerMode.FlushIntervalSec)
		go StartServerDispatch(a, a.P2P.Host())
	}

	return a.TUI.Run()
}

// Stop handles graceful shutdown of the inference engine processes, P2P connections, and
// hub services.
func (a *App) Stop() {
	a.Runner.Stop()
	a.P2P.Stop()
	if a.Rank != nil {
		a.Rank.Stop()
	}
	if a.DB != nil {
		// Final synchronous flush so a graceful shutdown never loses the accepted
		// "up to flush_interval_sec" window -- that tradeoff only applies to hard crashes.
		if a.PeerCache != nil {
			a.PeerCache.StopFlusher()
			if err := a.PeerCache.Flush(a.DB); err != nil {
				logInfo("[PeerCache] Final flush on shutdown failed: %v", err)
			}
		}
		a.DB.Close()
	}
}
