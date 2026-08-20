// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"net/http"
	"os"
)

// gpuDatabaseURL is the upstream dataset used to resolve GPU specifications for scoring.
const gpuDatabaseURL = "https://raw.githubusercontent.com/voidful/gpu-info-api/gpu-data/gpu.json"

// gpuDatabaseFile is the local cache path for the downloaded GPU specification database.
const gpuDatabaseFile = "gpu_database.json"

// scanGPUlevel downloads the GPU specification database once, skipping if already cached.
// It is only invoked when server mode is enabled, since scoring is a hub-only concern.
func scanGPUlevel() {
	if _, err := os.Stat(gpuDatabaseFile); err == nil {
		logInfo("[Rank] GPU database already present: %s, skipping download", gpuDatabaseFile)
		return
	} else if !os.IsNotExist(err) {
		logInfo("[Rank] Failed to stat GPU database: %v", err)
		return
	}

	logInfo("[Rank] Downloading GPU database from %s", gpuDatabaseURL)

	resp, err := http.Get(gpuDatabaseURL)
	if err != nil {
		logInfo("[Rank] Failed to download GPU database: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logInfo("[Rank] GPU database download returned HTTP %d", resp.StatusCode)
		return
	}

	out, err := os.Create(gpuDatabaseFile)
	if err != nil {
		logInfo("[Rank] Failed to create GPU database file: %v", err)
		return
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		logInfo("[Rank] Failed to write GPU database: %v", err)
		return
	}

	logInfo("[Rank] GPU database downloaded: %s (%.2f MB)", gpuDatabaseFile, float64(written)/(1024*1024))
}
