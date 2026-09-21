package main

import (
	"context"
	"log/slog"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
)

// reloadOnSignal reloads the daemon's startup configuration file. It shares
// the same backend.ActionReload path as the TUI's "reload" command, so a
// SIGHUP and a typed "reload" behave identically.
func reloadOnSignal(ctx context.Context, daemon *backend.Backend, logger *slog.Logger) {
	if _, err := daemon.Execute(ctx, backend.Request{Action: backend.ActionReload}); err != nil {
		logger.Error("configuration reload failed", "trigger", "SIGHUP", "error", err)
		return
	}
	logger.Info("configuration reloaded", "trigger", "SIGHUP")
}
