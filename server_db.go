// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DBManager owns the SQLite connection backing hub-mode peer and event persistence.
type DBManager struct {
	db *sql.DB
}

// PeerData is a single row of the peers table describing a known node in the mesh.
type PeerData struct {
	PeerID            string  `json:"peer_id"`
	GPUInfo           string  `json:"gpu_info"`
	IPAddress         string  `json:"ip_address"`
	LastPing          string  `json:"last_ping"`
	BootstrapAddr     string  `json:"bootstrap_addr"`
	EngineID          string  `json:"engine_id"`
	FailCount         int     `json:"fail_count"`
	PenaltyPoints     int     `json:"penalty_points"`
	TotalRequests     int     `json:"total_requests"`
	TotalTokens       int64   `json:"total_tokens"`
	InTokens          int64   `json:"in_tokens"`
	OutTokens         int64   `json:"out_tokens"`
	ContributionScore float64 `json:"contribution_score"`
}

// PeerEvent is a single row of the peer_events audit table.
type PeerEvent struct {
	ID            int64  `json:"id"`
	PeerID        string `json:"peer_id"`
	IPAddress     string `json:"ip_address"`
	EventType     string `json:"event_type"`
	FailCount     int    `json:"fail_count"`
	PenaltyPoints int    `json:"penalty_points"`
	Timestamp     string `json:"timestamp"`
	Detail        string `json:"detail"`
}

// peerColumns is the shared projection used by peer queries. The COALESCE guards let
// rows written before an additive migration still scan cleanly.
const peerColumns = `peer_id, gpu_info, ip_address, last_ping, COALESCE(bootstrap_addr, ''), COALESCE(engine_id, ''),
	COALESCE(fail_count, 0), COALESCE(penalty_points, 0), COALESCE(total_requests, 0), COALESCE(total_tokens, 0),
	COALESCE(in_tokens, 0), COALESCE(out_tokens, 0), COALESCE(contribution_score, 0.0)`

// scanPeers materializes a result set produced with peerColumns into PeerData values.
func scanPeers(rows *sql.Rows) []PeerData {
	var peers []PeerData
	for rows.Next() {
		var p PeerData
		if err := rows.Scan(&p.PeerID, &p.GPUInfo, &p.IPAddress, &p.LastPing, &p.BootstrapAddr, &p.EngineID,
			&p.FailCount, &p.PenaltyPoints, &p.TotalRequests, &p.TotalTokens, &p.InTokens, &p.OutTokens,
			&p.ContributionScore); err == nil {
			peers = append(peers, p)
		}
	}
	return peers
}

// NewDBManager opens the SQLite database in WAL mode and ensures the schema exists.
func NewDBManager(dbPath string) (*DBManager, error) {
	if dbPath == "" {
		dbPath = "./peers.db"
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout=10000&_pragma=journal_mode=WAL", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	db.SetMaxOpenConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	if _, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS peers (
		peer_id TEXT PRIMARY KEY,
		ip_address TEXT,
		last_ping TEXT,
		gpu_info TEXT,
		bootstrap_addr TEXT,
		engine_id TEXT
	);`); err != nil {
		return nil, fmt.Errorf("failed to create peers table: %v", err)
	}

	// Additive migrations for databases created by earlier versions. Errors are ignored
	// because ALTER TABLE fails when the column already exists.
	for _, stmt := range []string{
		"ALTER TABLE peers ADD COLUMN bootstrap_addr TEXT;",
		"ALTER TABLE peers ADD COLUMN engine_id TEXT;",
		"ALTER TABLE peers ADD COLUMN fail_count INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN penalty_points INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN total_requests INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN total_tokens INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN in_tokens INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN out_tokens INTEGER DEFAULT 0;",
		"ALTER TABLE peers ADD COLUMN contribution_score REAL DEFAULT 0.0;",
	} {
		db.Exec(stmt)
	}

	if _, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS peer_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		peer_id TEXT,
		ip_address TEXT,
		event_type TEXT,
		fail_count INTEGER,
		penalty_points INTEGER,
		timestamp TEXT,
		detail TEXT
	);`); err != nil {
		return nil, fmt.Errorf("failed to create peer_events table: %v", err)
	}

	logInfo("[DB] Initialized at %s; peers and peer_events tables ready", dbPath)
	return &DBManager{db: db}, nil
}

// Close releases the underlying SQLite connection pool.
func (m *DBManager) Close() error {
	return m.db.Close()
}

// UpsertPeer inserts or refreshes the dynamic state of a peer node.
func (m *DBManager) UpsertPeer(peerID, ipAddress, gpuInfo, bootstrapAddr, engineID string) error {
	_, err := m.db.Exec(`
		INSERT INTO peers (peer_id, ip_address, last_ping, gpu_info, bootstrap_addr, engine_id)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET
			ip_address=excluded.ip_address,
			last_ping=excluded.last_ping,
			gpu_info=excluded.gpu_info,
			bootstrap_addr=excluded.bootstrap_addr,
			engine_id=excluded.engine_id;
	`, peerID, ipAddress, time.Now().Format(time.RFC3339), gpuInfo, bootstrapAddr, engineID)
	return err
}

// UpdatePeerPing records the latest round-trip result for a peer.
func (m *DBManager) UpdatePeerPing(peerID string, rtt string) error {
	_, err := m.db.Exec("UPDATE peers SET last_ping = ? WHERE peer_id = ?", rtt, peerID)
	return err
}

// DeletePeer removes a peer row.
func (m *DBManager) DeletePeer(peerID string) error {
	_, err := m.db.Exec("DELETE FROM peers WHERE peer_id = ?", peerID)
	return err
}

// DeleteOldPeers removes peers whose last ping is older than the given threshold.
func (m *DBManager) DeleteOldPeers(threshold time.Duration) error {
	rows, err := m.db.Query("SELECT peer_id, last_ping FROM peers")
	if err != nil {
		return err
	}
	defer rows.Close()

	var toDelete []string
	now := time.Now()

	for rows.Next() {
		var id, lastPing string
		if err := rows.Scan(&id, &lastPing); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, lastPing)
		if err == nil && now.Sub(t) > threshold {
			toDelete = append(toDelete, id)
		}
	}

	if len(toDelete) == 0 {
		return nil
	}

	placeholders := make([]string, len(toDelete))
	args := make([]interface{}, len(toDelete))
	for i, id := range toDelete {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM peers WHERE peer_id IN (%s)", strings.Join(placeholders, ","))
	_, err = m.db.Exec(query, args...)
	return err
}

// UpdatePeerHealth persists the consecutive failure count and accumulated penalty points.
func (m *DBManager) UpdatePeerHealth(peerID string, failCount, penaltyPoints int) error {
	_, err := m.db.Exec("UPDATE peers SET fail_count = ?, penalty_points = ? WHERE peer_id = ?", failCount, penaltyPoints, peerID)
	return err
}

// RecordEvent appends an audit entry to the peer_events table.
func (m *DBManager) RecordEvent(peerID, ipAddress, eventType string, failCount, penaltyPoints int, detail string) error {
	_, err := m.db.Exec(`
		INSERT INTO peer_events (peer_id, ip_address, event_type, fail_count, penalty_points, timestamp, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, peerID, ipAddress, eventType, failCount, penaltyPoints, time.Now().Format(time.RFC3339), detail)
	return err
}

// GetRecentEvents returns the most recent audit entries, newest first.
func (m *DBManager) GetRecentEvents(limit int) ([]PeerEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := m.db.Query(`
		SELECT id, COALESCE(peer_id, ''), COALESCE(ip_address, ''), COALESCE(event_type, ''), fail_count, penalty_points, timestamp, COALESCE(detail, '')
		FROM peer_events ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []PeerEvent
	for rows.Next() {
		var e PeerEvent
		if err := rows.Scan(&e.ID, &e.PeerID, &e.IPAddress, &e.EventType, &e.FailCount, &e.PenaltyPoints, &e.Timestamp, &e.Detail); err == nil {
			events = append(events, e)
		}
	}
	return events, nil
}

// IncrementPeerContribution adds request and token counts, scoring requests*10 + tokens/10.
func (m *DBManager) IncrementPeerContribution(peerID string, requests int, tokens int64) error {
	if requests <= 0 {
		requests = 1
	}
	addedScore := float64(requests)*10.0 + float64(tokens)/10.0
	_, err := m.db.Exec(`
		UPDATE peers SET
			total_requests = total_requests + ?,
			total_tokens = total_tokens + ?,
			contribution_score = contribution_score + ?
		WHERE peer_id = ?
	`, requests, tokens, addedScore, peerID)
	return err
}

// IncrementPeerTokensDetail adds prompt and completion token counts separately.
func (m *DBManager) IncrementPeerTokensDetail(peerID string, requests int, inTokens int64, outTokens int64) error {
	if requests <= 0 {
		requests = 1
	}
	totTokens := inTokens + outTokens
	addedScore := float64(requests)*10.0 + float64(totTokens)/10.0
	_, err := m.db.Exec(`
		UPDATE peers SET
			total_requests = total_requests + ?,
			total_tokens = total_tokens + ?,
			in_tokens = in_tokens + ?,
			out_tokens = out_tokens + ?,
			contribution_score = contribution_score + ?
		WHERE peer_id = ?
	`, requests, totTokens, inTokens, outTokens, addedScore, peerID)
	return err
}

// SyncPeerStats reconciles gossiped cumulative counters, keeping whichever value is larger so
// that hub-observed totals are never rolled backwards by a stale broadcast.
func (m *DBManager) SyncPeerStats(peerID string, requests int64, tokens int64) error {
	if requests <= 0 && tokens <= 0 {
		return nil
	}
	addedScore := float64(requests)*10.0 + float64(tokens)/10.0
	_, err := m.db.Exec(`
		UPDATE peers SET
			total_requests = CASE WHEN ? > total_requests THEN ? ELSE total_requests END,
			total_tokens = CASE WHEN ? > total_tokens THEN ? ELSE total_tokens END,
			contribution_score = CASE WHEN ? > contribution_score THEN ? ELSE contribution_score END
		WHERE peer_id = ?
	`, requests, requests, tokens, tokens, addedScore, addedScore, peerID)
	return err
}

// GetLeaderboard returns all peers ordered by contribution score, highest first.
func (m *DBManager) GetLeaderboard() ([]PeerData, error) {
	rows, err := m.db.Query("SELECT " + peerColumns + " FROM peers ORDER BY contribution_score DESC, total_requests DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPeers(rows), nil
}

// GetAllPeers returns every known peer row.
func (m *DBManager) GetAllPeers() ([]PeerData, error) {
	rows, err := m.db.Query("SELECT " + peerColumns + " FROM peers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPeers(rows), nil
}
