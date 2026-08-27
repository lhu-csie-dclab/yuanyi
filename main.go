// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main is the entry point for the Mooncake 2.0 Client Agent.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, err := LoadOrCreateConfig("./config.json")
	if err != nil {
		fmt.Printf("Failed to initialize config: %v\n", err)
		return
	}

	// Only hub-mode nodes need the GPU specification database used for peer scoring; skip
	// the download for plain clients to avoid an unnecessary startup network request.
	if cfg.ServerMode.Enabled {
		scanGPUlevel()
	}

	app := NewApp(cfg)

	// Route slog (used by hub-mode code: server_proxy.go, p2p.go, server_db.go, ...) into
	// the TUI's own System Logs buffer instead of its default raw write to os.Stderr, which
	// bypasses the TUI's alt-screen entirely and bleeds directly onto the console under it.
	slog.SetDefault(slog.New(newTUISlogHandler(app.TUI)))

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		app.Stop()
		cancel()
	}()

	if err := app.Start(ctx); err != nil {
		log.Fatalf("Client exited with error: %v", err)
	}
}
