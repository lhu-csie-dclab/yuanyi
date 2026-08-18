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
}

// NewApp instantiates the App container and initializes its core subsystems.
func NewApp(cfg *ClientConfig) *App {
	app := &App{Config: cfg}
	app.TUI = NewTUI(app)
	app.Sys = NewSysMonitor(app)
	app.P2P = NewNetworkNode(app)
	app.Runner = NewRunner(app)
	return app
}

// Start launches hardware monitoring, P2P networking, vLLM runner, Web UI, and TUI in sequence.
func (a *App) Start(ctx context.Context) error {
	a.Sys.Start()

	if err := a.P2P.Start(ctx); err != nil {
		return fmt.Errorf("P2P start failed: %w", err)
	}

	go a.Runner.Start(ctx)
	go StartClientWebDashboard(a)

	return a.TUI.Run()
}

// Stop handles graceful shutdown of the inference engine processes and P2P connections.
func (a *App) Stop() {
	a.Runner.Stop()
	a.P2P.Stop()
}
