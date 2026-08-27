// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Backend is a remote P2P node the hub can forward inference requests to.
type Backend struct {
	PeerID        peer.ID
	IPAddress     string
	BootstrapAddr string
	EngineID      string
	Host          host.Host
}

// StreamRequest forwards an HTTP request to a remote node over the mooncake-proxy protocol.
func (b *Backend) StreamRequest(ctx context.Context, path string, data []byte) ([]byte, error) {
	stream, err := b.Host.NewStream(ctx, b.PeerID, ProxyProtocolID)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	if err := binary.Write(stream, binary.BigEndian, uint16(8100)); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:8100"

	if err := req.Write(stream); err != nil {
		return nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(stream), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ProxyServer is the hub central OpenAI-compatible dispatcher. Every server-mode node runs
// its own instance, scheduling requests across whatever peers its local peers.db currently
// knows about.
type ProxyServer struct {
	app             *App
	host            host.Host
	prefillBackends []*Backend
	decodeBackends  []*Backend
	mu              sync.RWMutex
	decodeIndex     int
	sem             chan struct{}
}

// NewProxyServer builds a ProxyServer capped at 128 concurrent forwarded requests.
func NewProxyServer(app *App) *ProxyServer {
	return &ProxyServer{
		app: app,
		sem: make(chan struct{}, 128),
	}
}

// reloadBackendsFromDB recomputes the prefill/decode backend split from the local peers.db,
// honoring server_mode.cluster.prefill_nodes and decode_nodes (both zero means PD-Together).
func (p *ProxyServer) reloadBackendsFromDB() {
	peers, err := p.app.DB.GetAllPeers()
	if err != nil {
		logInfo("[ServerDispatch] Failed to reload backends: %v", err)
		return
	}
	if len(peers) == 0 {
		return
	}

	prefillCount := p.app.Config.ServerMode.Cluster.PrefillNodes
	decodeCount := p.app.Config.ServerMode.Cluster.DecodeNodes
	isPDTogether := prefillCount == 0 && decodeCount == 0

	// Relay-only peers run no vLLM, so they must never become inference backends --
	// dispatching to them would always fail. Filter before the prefill/decode split so
	// the P/D counts describe usable capacity rather than being padded with relays.
	// Also check the persisted GPUInfo.Summary: it's been a required (non-omitempty)
	// field since before Role existed, so a peer broadcasting from a pre-Role build
	// (never sends "role":"relay" at all) still gets caught here by its "No GPU
	// Detected" summary -- see the matching check in proxy.go's PD-Together path.
	var usable []PeerData
	for _, p := range peers {
		if p.Role == RoleRelay {
			continue
		}
		var info GPUInfo
		if err := json.Unmarshal([]byte(p.GPUInfo), &info); err == nil && info.Summary == "No GPU Detected" {
			continue
		}
		usable = append(usable, p)
	}
	peers = usable
	if len(peers) == 0 {
		return
	}

	var newPrefill, newDecode []*Backend
	for i, peerData := range peers {
		pid, err := peer.Decode(peerData.PeerID)
		if err != nil {
			continue
		}

		b := &Backend{
			PeerID:        pid,
			IPAddress:     peerData.IPAddress,
			BootstrapAddr: peerData.BootstrapAddr,
			EngineID:      peerData.EngineID,
			Host:          p.host,
		}

		switch {
		case isPDTogether:
			newPrefill = append(newPrefill, b)
		case i < prefillCount:
			newPrefill = append(newPrefill, b)
		case decodeCount > 0 && len(newDecode) >= decodeCount:
			// decode capacity already filled, skip remaining peers
		default:
			newDecode = append(newDecode, b)
		}
	}

	if len(newDecode) == 0 && len(newPrefill) > 0 {
		newDecode = newPrefill
	}

	p.mu.Lock()
	p.prefillBackends = newPrefill
	p.decodeBackends = newDecode
	p.mu.Unlock()

	logInfo("[ServerDispatch] Backends reloaded: prefill=%d decode=%d", len(newPrefill), len(newDecode))
}

// GetTopologyInfo snapshots the current P/D topology for the /api/cluster_topology endpoint.
func (p *ProxyServer) GetTopologyInfo() ClusterTopologyResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prefillCount := p.app.Config.ServerMode.Cluster.PrefillNodes
	decodeCount := p.app.Config.ServerMode.Cluster.DecodeNodes

	toInfo := func(backends []*Backend) []BackendInfo {
		out := make([]BackendInfo, 0, len(backends))
		for _, b := range backends {
			out = append(out, BackendInfo{
				PeerID:        b.PeerID.String(),
				IPAddress:     b.IPAddress,
				BootstrapAddr: b.BootstrapAddr,
				EngineID:      b.EngineID,
			})
		}
		return out
	}

	return ClusterTopologyResponse{
		PrefillNodes:    prefillCount,
		DecodeNodes:     decodeCount,
		IsPDTogether:    prefillCount == 0 && decodeCount == 0,
		PrefillBackends: toInfo(p.prefillBackends),
		DecodeBackends:  toInfo(p.decodeBackends),
	}
}

// handleKVTunnel proxies Mooncake KV cache transfer requests to a peer over libp2p.
func (p *ProxyServer) handleKVTunnel(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(r.URL.Path, "/", 5)
	if len(parts) < 4 {
		http.Error(w, "Invalid KV tunnel path", http.StatusBadRequest)
		return
	}
	targetPeerID := parts[2]
	portStr := parts[3]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		http.Error(w, "Invalid port", http.StatusBadRequest)
		return
	}
	targetPath := "/"
	if len(parts) == 5 {
		targetPath = "/" + parts[4]
	}
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	pid, err := peer.Decode(targetPeerID)
	if err != nil {
		http.Error(w, "Invalid peer ID", http.StatusBadRequest)
		return
	}

	stream, err := p.host.NewStream(r.Context(), pid, ProxyProtocolID)
	if err != nil {
		http.Error(w, "Failed to connect to peer: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	if err := binary.Write(stream, binary.BigEndian, uint16(port)); err != nil {
		http.Error(w, "Failed to write port", http.StatusBadGateway)
		return
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetPath, r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	outReq.Header = r.Header.Clone()
	outReq.Host = "127.0.0.1:" + portStr

	if err := outReq.Write(stream); err != nil {
		http.Error(w, "Failed to write request", http.StatusBadGateway)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(stream), outReq)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// tokenUsage is the subset of a vLLM response used to record contribution scores.
type tokenUsage struct {
	Error interface{} `json:"error"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

// handleProxyRequest is the OpenAI-compatible entry point for /v1/chat/completions and
// /v1/completions, forwarding to a PD-Together backend or running the two-stage
// prefill/decode chain, then crediting the serving peer(s) in the local database.
func (p *ProxyServer) handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	p.sem <- struct{}{}
	defer func() { <-p.sem }()

	if r.Context().Err() != nil {
		writeJSONError(w, "Client Disconnected", 499)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal(body, &reqData); err != nil {
		writeJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cfg := p.app.Config
		isPDTogether := cfg.ServerMode.Cluster.PrefillNodes == 0 && cfg.ServerMode.Cluster.DecodeNodes == 0

		if isPDTogether {
			if p.dispatchPDTogether(w, r, reqData) {
				return
			}
			continue
		}
		if p.dispatchPDSplit(w, r, reqData) {
			return
		}
	}

	http.Error(w, "All backend attempts failed", http.StatusBadGateway)
}

// dispatchPDTogether round-robins a request to a single backend that handles both prefill
// and decode. It returns true once a response has been written (success or terminal error).
func (p *ProxyServer) dispatchPDTogether(w http.ResponseWriter, r *http.Request, reqData map[string]interface{}) bool {
	p.mu.Lock()
	if len(p.prefillBackends) == 0 {
		p.mu.Unlock()
		writeJSONError(w, "No backends available", http.StatusServiceUnavailable)
		return true
	}
	p.decodeIndex = (p.decodeIndex + 1) % len(p.prefillBackends)
	backend := p.prefillBackends[p.decodeIndex]
	p.mu.Unlock()

	reqBytes, err := json.Marshal(reqData)
	if err != nil {
		writeJSONError(w, "Failed to encode request", http.StatusInternalServerError)
		return true
	}

	respBytes, err := backend.StreamRequest(r.Context(), r.URL.Path, reqBytes)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "Client Disconnected", 499)
			return true
		}
		time.Sleep(200 * time.Millisecond)
		return false
	}

	var usage tokenUsage
	if err := json.Unmarshal(respBytes, &usage); err == nil {
		if usage.Error != nil {
			time.Sleep(200 * time.Millisecond)
			return false
		}
		pIn, pOut := usage.Usage.PromptTokens, usage.Usage.CompletionTokens
		if pIn == 0 && pOut == 0 {
			pIn, pOut = 20, 30
		}
		go p.app.DB.IncrementPeerTokensDetail(backend.PeerID.String(), 1, pIn, pOut)
	} else {
		go p.app.DB.IncrementPeerContribution(backend.PeerID.String(), 1, 50)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
	return true
}

// dispatchPDSplit runs the two-stage prefill/decode chain across dedicated backends.
func (p *ProxyServer) dispatchPDSplit(w http.ResponseWriter, r *http.Request, reqData map[string]interface{}) bool {
	p.mu.Lock()
	if len(p.prefillBackends) == 0 || len(p.decodeBackends) == 0 {
		p.mu.Unlock()
		http.Error(w, "No backends available", http.StatusServiceUnavailable)
		return true
	}
	prefillBackend := p.prefillBackends[0]
	p.decodeIndex = (p.decodeIndex + 1) % len(p.decodeBackends)
	decodeBackend := p.decodeBackends[p.decodeIndex]
	p.mu.Unlock()

	prefillReq := cloneRequest(reqData)
	prefillReq["max_tokens"] = 1
	prefillReq["ignore_eos"] = true
	prefillReq["temperature"] = 0.0
	prefillReq["mooncake_peer"] = decodeBackend.BootstrapAddr
	prefillReq["mooncake_engine"] = decodeBackend.EngineID

	prefillBytes, err := json.Marshal(prefillReq)
	if err != nil {
		http.Error(w, "Failed to encode prefill request", http.StatusInternalServerError)
		return true
	}

	prefillResp, err := prefillBackend.StreamRequest(r.Context(), r.URL.Path, prefillBytes)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "Client Disconnected", 499)
			return true
		}
		time.Sleep(200 * time.Millisecond)
		return false
	}

	var prefillRespObj map[string]interface{}
	if err := json.Unmarshal(prefillResp, &prefillRespObj); err == nil {
		if _, hasError := prefillRespObj["error"]; hasError {
			time.Sleep(200 * time.Millisecond)
			return false
		}
	}

	decodeReq := cloneRequest(reqData)
	decodeReq["mooncake_peer"] = prefillBackend.BootstrapAddr
	decodeReq["mooncake_engine"] = prefillBackend.EngineID

	decodeBytes, err := json.Marshal(decodeReq)
	if err != nil {
		http.Error(w, "Failed to encode decode request", http.StatusInternalServerError)
		return true
	}

	decodeResp, err := decodeBackend.StreamRequest(r.Context(), r.URL.Path, decodeBytes)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "Client Disconnected", 499)
			return true
		}
		time.Sleep(200 * time.Millisecond)
		return false
	}

	var usage tokenUsage
	pIn, pOut := int64(20), int64(80)
	if err := json.Unmarshal(decodeResp, &usage); err == nil {
		if usage.Error != nil {
			return false
		}
		if usage.Usage.PromptTokens > 0 {
			pIn = usage.Usage.PromptTokens
		}
		if usage.Usage.CompletionTokens > 0 {
			pOut = usage.Usage.CompletionTokens
		}
	}

	go p.app.DB.IncrementPeerTokensDetail(prefillBackend.PeerID.String(), 1, pIn, 0)
	go p.app.DB.IncrementPeerTokensDetail(decodeBackend.PeerID.String(), 1, 0, pOut)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(decodeResp)
	return true
}

// cloneRequest returns a shallow copy of a decoded JSON request body so per-stage fields
// (max_tokens, mooncake_peer, ...) can be added without mutating the input map.
func cloneRequest(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// modelsHandler serves a static OpenAI-compatible /v1/models listing.
func modelsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{"id": "Qwen3-4B-AWQ", "object": "model", "created": time.Now().Unix(), "owned_by": "yuanyi"},
			{"id": "yuanyi-default", "object": "model", "created": time.Now().Unix(), "owned_by": "yuanyi"},
		},
	})
}

// StartServerDispatch starts the hub central P/D dispatch service on server_mode.proxy_port.
func StartServerDispatch(app *App, h host.Host) {
	app.ServerProxy.host = h
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/completions", app.ServerProxy.handleProxyRequest)
	mux.HandleFunc("/v1/completions/", app.ServerProxy.handleProxyRequest)
	mux.HandleFunc("/v1/chat/completions", app.ServerProxy.handleProxyRequest)
	mux.HandleFunc("/v1/chat/completions/", app.ServerProxy.handleProxyRequest)
	mux.HandleFunc("/mooncake_kv/", app.ServerProxy.handleKVTunnel)
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/v1/models/", modelsHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := app.Config.ServerMode.ProxyPort
	if port <= 0 {
		port = 50008
	}
	logInfo("[ServerDispatch] Listening on :%d", port)
	go func() {
		if err := http.ListenAndServe(":"+strconv.Itoa(port), mux); err != nil {
			logError("[ServerDispatch] Failed to start: %v", err)
		}
	}()
}
