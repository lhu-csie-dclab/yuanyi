// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"
)

// ConnNotifee implements libp2p's network.Notifee to mirror connect/disconnect events
// into the local hub database. It is only registered when server mode is enabled.
type ConnNotifee struct {
	app *App
}

// Connected upserts the newly connected peer into the local peers.db.
func (n *ConnNotifee) Connected(nwt network.Network, c network.Conn) {
	remotePeer := c.RemotePeer()
	remoteAddr := c.RemoteMultiaddr().String()

	go func() {
		if err := n.app.DB.UpsertPeerConnection(remotePeer.String(), remoteAddr); err != nil {
			logInfo("[Hub] Failed to upsert peer: %v", err)
			return
		}
		if n.app.ServerProxy != nil {
			n.app.ServerProxy.reloadBackendsFromDB()
		}
	}()
}

// Disconnected removes the peer from the local peers.db.
func (n *ConnNotifee) Disconnected(nwt network.Network, c network.Conn) {
	remotePeer := c.RemotePeer()
	go func() {
		if err := n.app.DB.DeletePeer(remotePeer.String()); err != nil {
			logInfo("[Hub] Failed to delete peer: %v", err)
		}
	}()
}

func (n *ConnNotifee) OpenedStream(network.Network, network.Stream)     {}
func (n *ConnNotifee) ClosedStream(network.Network, network.Stream)     {}
func (n *ConnNotifee) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *ConnNotifee) ListenClose(network.Network, multiaddr.Multiaddr) {}

// loadOrGenerateIdentity loads a persisted Ed25519 identity, generating and saving one
// if none exists yet, so the host keeps a stable PeerID across restarts.
func loadOrGenerateIdentity(path string) crypto.PrivKey {
	if data, err := os.ReadFile(path); err == nil {
		if priv, err := crypto.UnmarshalPrivateKey(data); err == nil {
			return priv
		}
	}
	priv, _, _ := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if data, err := crypto.MarshalPrivateKey(priv); err == nil {
		os.WriteFile(path, data, 0600)
	}
	return priv
}

// serverProcessGossipMessage persists an already-decoded gossip broadcast into the local
// peers.db. Because every server-mode node subscribes to the same network-wide gossip
// topic, each hub converges to the same view independently without any hub-to-hub sync.
func serverProcessGossipMessage(info GPUInfo, app *App) {
	if info.NodeID == "" {
		return
	}

	gpus := info.GPUs
	if len(gpus) == 0 && info.Summary != "" {
		gpus = parseSummaryGPUs(info.Summary)
		info.GPUs = gpus
	}

	existingPeers, _ := app.DB.GetAllPeers()
	isNew := true
	for _, ep := range existingPeers {
		if ep.PeerID == info.NodeID {
			isNew = false
			break
		}
	}

	gpuData, err := json.Marshal(info)
	if err != nil {
		return
	}
	if err := app.DB.UpsertPeer(info.NodeID, info.Addr, string(gpuData), info.BootstrapAddr, info.EngineID, info.Role); err != nil {
		return
	}

	if info.TotalTokens > 0 || info.TotalRequests > 0 {
		_ = app.DB.SyncPeerStats(info.NodeID, info.TotalRequests, info.TotalTokens)
	}
	if isNew {
		app.DB.RecordEvent(info.NodeID, info.Addr, "JOIN", 0, 0, "New node joined via gossip broadcast")
	}
	if app.ServerProxy != nil {
		app.ServerProxy.reloadBackendsFromDB()
	}
}

// gpuSummaryPattern extracts model/capacity/count triples from a free-form summary string,
// e.g. "NVIDIA RTX 4090(24576MB) x2".
var gpuSummaryPattern = regexp.MustCompile(`(?i)([^,;()\n]+?)\s*\(\s*(\d+)\s*(?:MB)?\s*\)\s*(?:x\s*(\d+)|\s*(\d+)\s*[\x{5f35}\x{500b}])`)

// parseSummaryGPUs parses a GPU summary string into structured entries.
func parseSummaryGPUs(summary string) []GPUEntry {
	matches := gpuSummaryPattern.FindAllStringSubmatch(summary, -1)
	if len(matches) == 0 {
		return nil
	}

	counts := make(map[string]int)
	var order []string
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		vram := m[2]
		id := fmt.Sprintf("%s(%s)", name, vram)

		numStr := m[3]
		if numStr == "" && len(m) > 4 {
			numStr = m[4]
		}
		num, err := strconv.Atoi(numStr)
		if err != nil {
			num = 1
		}

		if _, ok := counts[id]; !ok {
			order = append(order, id)
		}
		counts[id] += num
	}

	entries := make([]GPUEntry, 0, len(order))
	for _, id := range order {
		entries = append(entries, GPUEntry{ID: id, Num: counts[id]})
	}
	return entries
}

// startServerPingLoop periodically pings every known peer and updates its health state:
// a success resets the failure count (and awards a recovery penalty point if it had been
// failing), while repeated failures accumulate toward server_mode.max_fail_count.
func startServerPingLoop(ctx context.Context, app *App, h host.Host) {
	interval := time.Duration(app.Config.ServerMode.CheckIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			peers, err := app.DB.GetAllPeers()
			if err != nil || len(peers) == 0 {
				continue
			}

			ps := ping.NewPingService(h)
			for _, p := range peers {
				go pingOnePeer(ctx, app, ps, p)
			}
		}
	}
}

// pingOnePeer runs a single health check against one peer and records the outcome.
func pingOnePeer(ctx context.Context, app *App, ps *ping.PingService, peerData PeerData) {
	pid, err := peer.Decode(peerData.PeerID)
	if err != nil {
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	ch := ps.Ping(pingCtx, pid)
	var pingErr error
	select {
	case res := <-ch:
		pingErr = res.Error
	case <-pingCtx.Done():
		pingErr = pingCtx.Err()
	}

	if pingErr == nil {
		app.DB.UpdatePeerPing(peerData.PeerID, "OK")
		if peerData.FailCount > 0 {
			newPenalty := peerData.PenaltyPoints + 1
			app.DB.UpdatePeerHealth(peerData.PeerID, 0, newPenalty)
			app.DB.RecordEvent(peerData.PeerID, peerData.IPAddress, "RECOVERED_PENALTY", 0, newPenalty,
				fmt.Sprintf("Node recovered after %d consecutive failures (+1 penalty point)", peerData.FailCount))
		}
		return
	}

	newFail := peerData.FailCount + 1
	maxFail := app.Config.ServerMode.MaxFailCount
	if maxFail <= 0 {
		maxFail = 3
	}

	if newFail >= maxFail {
		app.DB.RecordEvent(peerData.PeerID, peerData.IPAddress, "WARNING_OFFLINE", newFail, peerData.PenaltyPoints,
			fmt.Sprintf("%d/%d consecutive connection failures", newFail, maxFail))
		return
	}

	app.DB.UpdatePeerHealth(peerData.PeerID, newFail, peerData.PenaltyPoints)
	app.DB.RecordEvent(peerData.PeerID, peerData.IPAddress, "FAIL_INCREMENT", newFail, peerData.PenaltyPoints,
		fmt.Sprintf("Connection test failed %d/%d times", newFail, maxFail))
}
