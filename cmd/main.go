package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
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

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()
	if err := s.Run(ctx); err != nil {
		fmt.Println("supervisor:", err)
	}
}
