// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// vramScoreMultiplier converts VRAM capacity in GiB into a hardware contribution score.
const vramScoreMultiplier = 300

// defaultVRAMGB is assumed when a GPU model cannot be resolved to a known capacity.
const defaultVRAMGB = 16

// GPUSpec is one entry of the GPU specification database, kept as raw JSON because
// upstream records are heterogeneous (numbers, strings, ranges, currency-prefixed values).
type GPUSpec map[string]json.RawMessage

// getString returns the first key that decodes as a string.
func (s GPUSpec) getString(keys ...string) string {
	for _, k := range keys {
		if raw, ok := s[k]; ok {
			var str string
			if err := json.Unmarshal(raw, &str); err == nil {
				return str
			}
		}
	}
	return ""
}

var numberRe = regexp.MustCompile(`-?\d+(\.\d+)?`)

// extractNumber pulls a numeric value out of a heterogeneous JSON field. Plain numbers are
// decoded directly; strings such as "24 GB" or "$1,200" are parsed with a regex, and ranges
// such as "10-12" are averaged.
func extractNumber(raw json.RawMessage) (float64, bool) {
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, true
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, false
	}
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if s == "" || s == "?" || lower == "-n/a" || lower == "nan" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "$")

	matches := numberRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	if len(matches) == 1 {
		v, err := strconv.ParseFloat(matches[0], 64)
		if err != nil {
			return 0, false
		}
		return v, true
	}

	sum, count := 0.0, 0
	for _, m := range matches {
		if v, err := strconv.ParseFloat(m, 64); err == nil {
			sum += v
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}

// getFlexNumber returns the first key that yields a usable number.
func (s GPUSpec) getFlexNumber(keys ...string) float64 {
	for _, k := range keys {
		if raw, ok := s[k]; ok {
			if v, ok := extractNumber(raw); ok {
				return v
			}
		}
	}
	return 0
}

// getVRAM resolves the VRAM capacity of a spec entry in GiB.
func (s GPUSpec) getVRAM() float64 {
	if v := s.getFlexNumber("Memory Size (GiB)"); v > 0 {
		return v
	}
	if v := s.getFlexNumber("Memory Configuration Size (MB)"); v > 0 {
		return v / 1024
	}
	return 0
}

// ReadGPUDatabase loads and parses the cached GPU specification database.
func ReadGPUDatabase(filePath string) (map[string]GPUSpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read GPU database file: %v", err)
	}
	var gpuMap map[string]GPUSpec
	if err := json.Unmarshal(data, &gpuMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GPU database: %v", err)
	}
	return gpuMap, nil
}

// cleanGPUName strips trailing parenthesised detail, e.g. "RTX 4090(24576MB)" -> "RTX 4090".
func cleanGPUName(raw string) string {
	if idx := strings.Index(raw, "("); idx != -1 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// normalize lowercases and collapses separators so model names compare loosely.
func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("_", " ", "-", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// matchGPUSpec fuzzily resolves a reported model name against the specification database,
// preferring the longest matching variant so that "RTX 4090" beats a shorter "RTX 40".
func matchGPUSpec(rawName string, gpuMap map[string]GPUSpec) (GPUSpec, string, bool) {
	name := normalize(cleanGPUName(rawName))
	if name == "" {
		return nil, "", false
	}

	var best GPUSpec
	bestKey := ""
	bestLen := -1

	for key, spec := range gpuMap {
		variants := []string{
			normalize(key),
			normalize(spec.getString("Model")),
			normalize(spec.getString("Model name")),
			normalize(spec.getString("Code name")),
		}
		for _, v := range variants {
			if v == "" {
				continue
			}
			if v == name || strings.Contains(name, v) || strings.Contains(v, name) {
				if len(v) > bestLen {
					bestLen = len(v)
					best = spec
					bestKey = key
				}
			}
		}
	}

	if bestLen < 0 {
		return nil, "", false
	}
	return best, bestKey, true
}

var vramRe = regexp.MustCompile(`(?i)\(\s*(\d+)\s*(?:MB)?\s*\)`)
var vramGBRe = regexp.MustCompile(`(?i)(\d+)\s*GB`)

// knownModelVRAM is the fallback capacity table consulted when neither the reported string
// nor the specification database yields a capacity.
var knownModelVRAM = []struct {
	token string
	gb    float64
}{
	{"4090", 24}, {"3090", 24}, {"4080", 16}, {"3080", 10}, {"4070", 12},
	{"3070", 8}, {"2080", 8}, {"A100", 80}, {"H100", 80}, {"L40", 48},
	{"A10", 24}, {"T4", 24},
}

// extractVRAMFromID derives VRAM capacity from a reported model string, first from an explicit
// size and then from a known-model lookup.
func extractVRAMFromID(rawID string) (float64, bool) {
	if matches := vramRe.FindStringSubmatch(rawID); len(matches) >= 2 {
		if val, err := strconv.ParseFloat(matches[1], 64); err == nil && val > 0 {
			if val > 1024 {
				return val / 1024, true
			}
			return val, true
		}
	}

	if m := vramGBRe.FindStringSubmatch(rawID); len(m) >= 2 {
		if gb, err := strconv.ParseFloat(m[1], 64); err == nil && gb > 0 {
			return gb, true
		}
	}

	upper := strings.ToUpper(rawID)
	for _, e := range knownModelVRAM {
		if strings.Contains(upper, e.token) {
			return e.gb, true
		}
	}

	return 0, false
}

// RankManager periodically scores every known peer by GPU capability and publishes top.json.
type RankManager struct {
	db     *DBManager
	gpuMap map[string]GPUSpec
	done   chan struct{}
}

// NewRankManager builds a RankManager, loading the GPU database if it is already cached.
func NewRankManager(db *DBManager) *RankManager {
	gpuMap, err := ReadGPUDatabase(gpuDatabaseFile)
	if err != nil {
		gpuMap = make(map[string]GPUSpec)
	}

	return &RankManager{
		db:     db,
		gpuMap: gpuMap,
		done:   make(chan struct{}),
	}
}

// getGPUMap lazily reloads the GPU database if it was unavailable at construction time.
func (rm *RankManager) getGPUMap() map[string]GPUSpec {
	if len(rm.gpuMap) == 0 {
		if gpuMap, err := ReadGPUDatabase(gpuDatabaseFile); err == nil && len(gpuMap) > 0 {
			rm.gpuMap = gpuMap
		}
	}
	return rm.gpuMap
}

// Start clears any stale ranking file and begins the periodic scoring loop.
func (rm *RankManager) Start() {
	if err := os.Remove("top.json"); err != nil && !os.IsNotExist(err) {
		logInfo("[Rank] Failed to clear stale top.json: %v", err)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		rm.TriggerUpdate()

		for {
			select {
			case <-ticker.C:
				rm.TriggerUpdate()
			case <-rm.done:
				return
			}
		}
	}()
}

// Stop terminates the periodic scoring loop.
func (rm *RankManager) Stop() {
	close(rm.done)
}

// CalculateScore scores a peer from its serialized GPUInfo snapshot as VRAM_GB * 300 * count,
// falling back to a single default-capacity GPU when the payload cannot be resolved.
func (rm *RankManager) CalculateScore(gpuInfoStr string) float64 {
	fallback := float64(defaultVRAMGB * vramScoreMultiplier)

	var info GPUInfo
	if err := json.Unmarshal([]byte(gpuInfoStr), &info); err != nil {
		if gpuInfoStr != "" {
			if vram, ok := extractVRAMFromID(gpuInfoStr); ok && vram > 0 {
				return vram * vramScoreMultiplier
			}
		}
		return fallback
	}

	gpus := info.GPUs
	if len(gpus) == 0 && info.Summary != "" {
		gpus = parseSummaryGPUs(info.Summary)
	}

	gpuMap := rm.getGPUMap()
	totalScore := 0.0

	for _, entry := range gpus {
		num := entry.Num
		if num <= 0 {
			num = 1
		}
		spec, _, ok := matchGPUSpec(entry.ID, gpuMap)
		vramGB, vramOk := extractVRAMFromID(entry.ID)
		if !vramOk && ok {
			vramGB = spec.getVRAM()
		}
		if vramGB <= 0 {
			vramGB = defaultVRAMGB
		}
		totalScore += vramGB * vramScoreMultiplier * float64(num)
	}

	if totalScore <= 0 {
		return fallback
	}
	return totalScore
}

// TriggerUpdate rescores every peer and atomically republishes top.json.
func (rm *RankManager) TriggerUpdate() {
	peers, err := rm.db.GetAllPeers()
	if err != nil {
		logInfo("[Rank] Failed to read peers: %v", err)
		return
	}

	type rankedPeer struct {
		peer  map[string]string
		score float64
	}

	rankedPeers := make([]rankedPeer, 0, len(peers))
	for _, p := range peers {
		rankedPeers = append(rankedPeers, rankedPeer{
			peer: map[string]string{
				"peer_id":        p.PeerID,
				"gpu_info":       p.GPUInfo,
				"ip":             p.IPAddress,
				"last_ping":      p.LastPing,
				"bootstrap_addr": p.BootstrapAddr,
				"engine_id":      p.EngineID,
			},
			score: rm.CalculateScore(p.GPUInfo),
		})
	}

	sort.SliceStable(rankedPeers, func(i, j int) bool {
		return rankedPeers[i].score > rankedPeers[j].score
	})

	rankedResult := make([]map[string]string, len(rankedPeers))
	for i, rp := range rankedPeers {
		rp.peer["score"] = fmt.Sprintf("%d", int(math.Round(rp.score)))
		rp.peer["rank_position"] = fmt.Sprintf("%d", i+1)
		rankedResult[i] = rp.peer
	}

	topJSON, err := json.MarshalIndent(rankedResult, "", "  ")
	if err != nil {
		logInfo("[Rank] Failed to serialize ranking: %v", err)
		return
	}

	// Write to a temporary file and rename so readers never observe a partial file.
	tmpFile := "top.json.tmp"
	if err := os.WriteFile(tmpFile, topJSON, 0644); err != nil {
		logInfo("[Rank] Failed to write temporary ranking file: %v", err)
		return
	}
	if err := os.Rename(tmpFile, "top.json"); err != nil {
		logInfo("[Rank] Failed to publish top.json: %v", err)
	}
}
