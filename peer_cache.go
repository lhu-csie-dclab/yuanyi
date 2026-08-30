// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// PeerCache is the in-process canonical store of live peer state for hub mode. It is the sole
// writer of every peers-table column at runtime: gossip/connect/health/contribution updates
// land here first (cheap, in-memory), and a periodic flusher batches them into peers.db instead
// of one SQLite transaction per event. peers.db becomes a flush target and a boot-time
// warm-start/export source rather than something touched on the gossip hot path. The
// ip_verified precedence and cumulative-stat keep-max rules replicate DBManager.UpsertPeer and
// DBManager.SyncPeerStats exactly -- see those comments in server_db.go for the reasoning.
type PeerCache struct {
	mu             sync.Mutex
	peers          map[string]*PeerData
	pendingEvents  []PeerEvent
	pendingDeletes map[string]struct{}
	done           chan struct{}
}

// PeerSnapshot is the wire format for bulk peer-state transfer between hubs: the payload of the
// /hub/api/snapshot endpoint and of syncPeerCacheFromSeed's boot-time fetch.
type PeerSnapshot struct {
	Version     int        `json:"version"`
	GeneratedAt string     `json:"generated_at"`
	Peers       []PeerData `json:"peers"`
}

// NewPeerCache builds an empty cache. Callers warm-start it via LoadSnapshot before use.
func NewPeerCache() *PeerCache {
	return &PeerCache{
		peers:          make(map[string]*PeerData),
		pendingDeletes: make(map[string]struct{}),
		done:           make(chan struct{}),
	}
}

// UpsertGossip merges an incoming GossipSub broadcast into the cache. Replicates UpsertPeer's
// ip_verified precedence (a gossip self-report never clobbers a connection-observed address)
// and SyncPeerStats' independent per-column keep-max reconciliation, guarded by the same
// "any nonzero cumulative stat" condition the original call site used. Returns true if this
// peer was not previously known, so the caller can queue a JOIN audit event.
func (pc *PeerCache) UpsertGossip(info GPUInfo) bool {
	gpuData, err := json.Marshal(info)
	if err != nil {
		return false
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.pendingDeletes, info.NodeID)

	p, ok := pc.peers[info.NodeID]
	isNew := !ok
	if !ok {
		p = &PeerData{PeerID: info.NodeID}
		pc.peers[info.NodeID] = p
	}

	if !p.IPVerified {
		p.IPAddress = info.Addr
	}
	p.LastPing = time.Now().Format(time.RFC3339)
	p.GPUInfo = string(gpuData)
	p.BootstrapAddr = info.BootstrapAddr
	p.EngineID = info.EngineID
	p.Role = info.Role

	if info.TotalTokens > 0 || info.TotalRequests > 0 {
		if info.TotalRequests > int64(p.TotalRequests) {
			p.TotalRequests = int(info.TotalRequests)
		}
		if info.TotalTokens > p.TotalTokens {
			p.TotalTokens = info.TotalTokens
		}
		addedScore := float64(info.TotalRequests)*10.0 + float64(info.TotalTokens)/10.0
		if addedScore > p.ContributionScore {
			p.ContributionScore = addedScore
		}
	}

	return isNew
}

// UpsertConnection records that a peer connected, mirroring UpsertPeerConnection: the address
// comes from an actually-observed connection so it's trusted (ip_verified=true), and an
// existing row's role is left untouched since a connect event carries no role information (see
// the equivalent comment on the old DBManager.UpsertPeerConnection).
func (pc *PeerCache) UpsertConnection(peerID, ipAddress string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.pendingDeletes, peerID)

	now := time.Now().Format(time.RFC3339)
	p, ok := pc.peers[peerID]
	if !ok {
		pc.peers[peerID] = &PeerData{
			PeerID:     peerID,
			GPUInfo:    "[]",
			IPAddress:  ipAddress,
			LastPing:   now,
			IPVerified: true,
		}
		return
	}
	p.IPAddress = ipAddress
	p.LastPing = now
	p.IPVerified = true
}

// RemovePeer drops a peer from the live map immediately (so reads reflect it right away) and
// queues the ID for a batched DELETE on the next flush. If the peer reconnects or re-gossips
// before that flush, UpsertConnection/UpsertGossip cancel the pending delete, so a stale DELETE
// is never issued after a re-insert.
func (pc *PeerCache) RemovePeer(peerID string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.peers, peerID)
	pc.pendingDeletes[peerID] = struct{}{}
}

// SetPing mirrors UpdatePeerPing, including its quirk of storing the literal string "OK" (not a
// timestamp) into last_ping on a successful health check -- replicated verbatim, not "fixed".
func (pc *PeerCache) SetPing(peerID, value string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if p, ok := pc.peers[peerID]; ok {
		p.LastPing = value
	}
}

// UpdateHealth mirrors UpdatePeerHealth. No-op if the peer is unknown, matching the original
// UPDATE ... WHERE peer_id = ?, which silently affects zero rows for an unknown peer.
func (pc *PeerCache) UpdateHealth(peerID string, failCount, penaltyPoints int) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if p, ok := pc.peers[peerID]; ok {
		p.FailCount = failCount
		p.PenaltyPoints = penaltyPoints
	}
}

// QueueEvent appends an audit event for the next flush. Every discrete event is preserved --
// none are dropped or coalesced, only batched in timing.
func (pc *PeerCache) QueueEvent(peerID, ipAddress, eventType string, failCount, penaltyPoints int, detail string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.pendingEvents = append(pc.pendingEvents, PeerEvent{
		PeerID: peerID, IPAddress: ipAddress, EventType: eventType,
		FailCount: failCount, PenaltyPoints: penaltyPoints,
		Timestamp: time.Now().Format(time.RFC3339), Detail: detail,
	})
}

// IncrementContribution mirrors DBManager.IncrementPeerContribution's request/token accounting
// and scoring formula exactly. No-op if the peer is unknown (same as the original's UPDATE
// against a nonexistent row).
func (pc *PeerCache) IncrementContribution(peerID string, requests int, tokens int64) {
	if requests <= 0 {
		requests = 1
	}
	addedScore := float64(requests)*10.0 + float64(tokens)/10.0

	pc.mu.Lock()
	defer pc.mu.Unlock()
	p, ok := pc.peers[peerID]
	if !ok {
		return
	}
	p.TotalRequests += requests
	p.TotalTokens += tokens
	p.ContributionScore += addedScore
}

// IncrementTokensDetail mirrors DBManager.IncrementPeerTokensDetail exactly.
func (pc *PeerCache) IncrementTokensDetail(peerID string, requests int, inTokens, outTokens int64) {
	if requests <= 0 {
		requests = 1
	}
	totTokens := inTokens + outTokens
	addedScore := float64(requests)*10.0 + float64(totTokens)/10.0

	pc.mu.Lock()
	defer pc.mu.Unlock()
	p, ok := pc.peers[peerID]
	if !ok {
		return
	}
	p.TotalRequests += requests
	p.TotalTokens += totTokens
	p.InTokens += inTokens
	p.OutTokens += outTokens
	p.ContributionScore += addedScore
}

// ResetStats zeroes every cumulative contribution counter for all cached peers, returning how
// many were affected. Peer identity, address and health state are untouched -- only the
// accumulating totals.
//
// UpsertGossip merges these fields with a keep-max rule, which makes a polluted value permanent
// on its own: a peer whose count was inflated by a since-fixed accounting bug keeps
// broadcasting that inflated total, and keep-max means the hub can only ever adopt it again.
// Clearing the hub alone is therefore not enough -- reset the source nodes first (their
// /api/stats/reset), then this, or the next gossip round (~3s) restores what was just cleared.
func (pc *PeerCache) ResetStats() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for _, p := range pc.peers {
		p.TotalRequests = 0
		p.TotalTokens = 0
		p.InTokens = 0
		p.OutTokens = 0
		p.ContributionScore = 0
	}
	return len(pc.peers)
}

// Snapshot returns a point-in-time copy of every known peer, replacing DBManager.GetAllPeers()
// as the read source for the proxy backend list, the rank manager, and the dashboard.
func (pc *PeerCache) Snapshot() []PeerData {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	out := make([]PeerData, 0, len(pc.peers))
	for _, p := range pc.peers {
		out = append(out, *p)
	}
	return out
}

// mergePeer folds one snapshot entry into the cache. Caller must hold pc.mu. Used by
// LoadSnapshot only (boot-time local-DB warm start and seed-fetch bulk load), both of which run
// before gossip processing starts, so an unknown peer is simply inserted as-is. For a peer
// that's already present (the local warm start already ran before a seed snapshot arrives),
// only the cumulative stats are reconciled with the same keep-max rule as UpsertGossip -- every
// other field (address, role, ...) is left alone, since gossip refreshes it within ~3s anyway,
// and a stale on-disk/seed snapshot has no way to assert a more trustworthy ip_verified state
// than whatever is already live.
func (pc *PeerCache) mergePeer(pd PeerData) {
	p, ok := pc.peers[pd.PeerID]
	if !ok {
		cp := pd
		pc.peers[pd.PeerID] = &cp
		return
	}
	if pd.TotalRequests > p.TotalRequests {
		p.TotalRequests = pd.TotalRequests
	}
	if pd.TotalTokens > p.TotalTokens {
		p.TotalTokens = pd.TotalTokens
	}
	if pd.ContributionScore > p.ContributionScore {
		p.ContributionScore = pd.ContributionScore
	}
	if pd.InTokens > p.InTokens {
		p.InTokens = pd.InTokens
	}
	if pd.OutTokens > p.OutTokens {
		p.OutTokens = pd.OutTokens
	}
}

// LoadSnapshot bulk-merges a slice of peers into the cache -- used for the local-DB warm start
// in NewApp and for a fetched seed snapshot in syncPeerCacheFromSeed.
func (pc *PeerCache) LoadSnapshot(peers []PeerData) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for _, p := range peers {
		pc.mergePeer(p)
	}
}

// Flush drains the cache into peers.db via one batched transaction. It copies the pending
// events/deletes queues, commits, and only then removes exactly what was committed --
// anything queued concurrently during the flush (mid-flush QueueEvent/RemovePeer calls) is
// appended after the copy and is never touched by the post-commit drain, so it's picked up by
// the next flush intact. A failed flush leaves both queues untouched for retry next tick --
// only a hard crash between flushes loses data, never a transient DB error.
func (pc *PeerCache) Flush(db *DBManager) error {
	pc.mu.Lock()
	peersSnap := make([]PeerData, 0, len(pc.peers))
	for _, p := range pc.peers {
		peersSnap = append(peersSnap, *p)
	}
	eventsSnap := append([]PeerEvent(nil), pc.pendingEvents...)
	deletesSnap := make([]string, 0, len(pc.pendingDeletes))
	for id := range pc.pendingDeletes {
		deletesSnap = append(deletesSnap, id)
	}
	pc.mu.Unlock()

	if len(peersSnap) == 0 && len(eventsSnap) == 0 && len(deletesSnap) == 0 {
		return nil
	}

	if err := db.BatchFlush(peersSnap, eventsSnap, deletesSnap); err != nil {
		return err
	}

	pc.mu.Lock()
	pc.pendingEvents = pc.pendingEvents[len(eventsSnap):]
	for _, id := range deletesSnap {
		delete(pc.pendingDeletes, id)
	}
	pc.mu.Unlock()
	return nil
}

// StartFlusher runs the periodic batch-flush loop until StopFlusher is called. Modeled on
// RankManager.Start's ticker/done idiom (server_rank.go). Intended to be launched with `go`.
func (pc *PeerCache) StartFlusher(db *DBManager, intervalSec int) {
	if intervalSec <= 0 {
		intervalSec = 45
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := pc.Flush(db); err != nil {
				logInfo("[PeerCache] Flush failed, will retry next interval: %v", err)
			}
		case <-pc.done:
			return
		}
	}
}

// StopFlusher terminates the loop started by StartFlusher.
func (pc *PeerCache) StopFlusher() {
	close(pc.done)
}

// syncPeerCacheFromSeed does a best-effort, one-time bulk catch-up from a configured seed hub
// at boot, so a new or rejoining hub doesn't have to wait for gossip to trickle its state back
// in over time. Tries each configured URL in order and stops at the first success -- mirrors
// this codebase's "any one reachable bootstrap entry is enough" philosophy for
// p2p.server_addresses. Never aborts startup: any failure just leaves the cache with whatever
// the local-DB warm start already provided.
func syncPeerCacheFromSeed(app *App) {
	if !app.Config.ServerMode.Enabled || app.DB == nil || len(app.Config.ServerMode.SnapshotSeedURLs) == 0 {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	for _, url := range app.Config.ServerMode.SnapshotSeedURLs {
		resp, err := client.Get(url)
		if err != nil {
			app.TUI.AddLog("[WARN]", fmt.Sprintf("[PeerCache] Snapshot fetch from %s failed: %v", url, err))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			app.TUI.AddLog("[WARN]", fmt.Sprintf("[PeerCache] Snapshot fetch from %s returned %s", url, resp.Status))
			continue
		}

		var snap PeerSnapshot
		err = json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if err != nil {
			app.TUI.AddLog("[WARN]", fmt.Sprintf("[PeerCache] Snapshot decode from %s failed: %v", url, err))
			continue
		}

		app.PeerCache.LoadSnapshot(snap.Peers)
		app.TUI.AddLog("[INFO]", fmt.Sprintf("[PeerCache] Bootstrapped %d peers from snapshot seed %s", len(snap.Peers), url))
		if err := app.DB.BatchFlush(app.PeerCache.Snapshot(), nil, nil); err != nil {
			app.TUI.AddLog("[WARN]", fmt.Sprintf("[PeerCache] Failed to persist fetched snapshot: %v", err))
		}
		return
	}
	app.TUI.AddLog("[WARN]", "[PeerCache] All snapshot seed URLs unreachable; starting with local state only.")
}
