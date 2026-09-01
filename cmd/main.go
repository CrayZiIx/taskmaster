package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
	"github.com/CrayZiIx/taskmaster/internal/tui"
)

func main() {

	configPath := config.DefaultConfigPath
	if len(os.Args) > 1 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	s, err := supervisor.New(cfg)
	if err != nil {
		fmt.Println(err)
		return
	}
	daemon, err := backend.New(s, configPath)
	if err != nil {
		fmt.Println(err)
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
		fmt.Fprintln(os.Stderr, "tui:", err)
	}
	shell.Close()
	if err := s.Shutdown(); err != nil {
		fmt.Fprintln(os.Stderr, "supervisor shutdown:", err)
	}
	if err := <-supervisorDone; err != nil && !errors.Is(err, supervisor.ErrSupervisorShuttingDown) {
		fmt.Fprintln(os.Stderr, "supervisor:", err)
	}
}
