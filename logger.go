// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
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

// tuiSlogHandler routes slog output into the TUI's own log buffer (visible in the System
// Logs tab) instead of slog's default behavior of writing straight to os.Stderr. That
// default matters here: the Bubble Tea TUI takes over the terminal via an alt-screen
// buffer, but a raw write to stderr shares the same physical console and isn't part of
// that buffer, so it punches straight through underneath the TUI -- every logInfo/logError
// call in hub-mode code (server_proxy.go, p2p.go, server_db.go, ...) was doing exactly
// that, visible as a stream of "[ServerDispatch] Backends reloaded" lines scrolling in
// below an otherwise correctly-rendered TUI frame. Wired up in main.go via
// slog.SetDefault, once app.TUI exists and before app.Start runs any logging goroutines.
type tuiSlogHandler struct {
	t *TUI
}

func newTUISlogHandler(t *TUI) *tuiSlogHandler { return &tuiSlogHandler{t: t} }

func (h *tuiSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *tuiSlogHandler) Handle(_ context.Context, r slog.Record) error {
	level := "[INFO]"
	switch r.Level {
	case slog.LevelError:
		level = "[ERROR]"
	case slog.LevelWarn:
		level = "[WARN]"
	case slog.LevelDebug:
		level = "[DEBUG]"
	}
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})
	h.t.AddLog(level, msg)
	return nil
}

func (h *tuiSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *tuiSlogHandler) WithGroup(string) slog.Handler       { return h }
