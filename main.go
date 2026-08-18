// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main is the entry point for the Mooncake 2.0 Client Agent.
package main

import (
	"context"
	"fmt"
	"log"
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

	app := NewApp(cfg)

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
