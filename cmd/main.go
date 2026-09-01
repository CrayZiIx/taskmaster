package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
	"github.com/CrayZiIx/taskmaster/internal/tui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	configPath := config.DefaultConfigPath
	if len(os.Args) > 1 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Error("configuration load failed", "path", configPath, "error", err)
		return
	}

	s, err := supervisor.NewWithLogger(cfg, logger)
	if err != nil {
		logger.Error("supervisor creation failed", "error", err)
		return
	}
	daemon, err := backend.New(s, configPath)
	if err != nil {
		logger.Error("backend creation failed", "error", err)
		return
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()
	supervisorDone := make(chan error, 1)
	go func() {
		supervisorDone <- s.Run(ctx)
	}()

	shell := tui.New(commandHandler(ctx, daemon))
	if err := shell.RunContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("TUI stopped with an error", "error", err)
	}
	shell.Close()
	if err := s.Shutdown(); err != nil {
		logger.Error("supervisor shutdown failed", "error", err)
	}
	if err := <-supervisorDone; err != nil && !errors.Is(err, supervisor.ErrSupervisorShuttingDown) {
		logger.Error("supervisor stopped with an error", "error", err)
	}
}
