// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log/slog"
	"os"
)

// logInfo emits an info-level structured log entry with printf-style formatting.
func logInfo(format string, args ...any) {
	if len(args) == 0 {
		slog.Info(format)
	} else {
		slog.Info(fmt.Sprintf(format, args...))
	}
}

// logError emits an error-level structured log entry with printf-style formatting.
func logError(format string, args ...any) {
	if len(args) == 0 {
		slog.Error(format)
	} else {
		slog.Error(fmt.Sprintf(format, args...))
	}
}

// logFatal logs an error and terminates the process with exit code 1.
func logFatal(args ...any) {
	slog.Error(fmt.Sprint(args...))
	os.Exit(1)
}
