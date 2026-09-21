package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
)

func TestReloadOnSignalAppliesConfigChanges(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "taskmaster.yaml")
	writeReloadConfig := func(autostart bool) {
		content := "taskmaster:\n  worker:\n    cmd: [\"/bin/sleep\", \"30\"]\n    process-nb: 1\n    output:\n      stdout: /dev/null\n      stderr: /dev/null\n    journey:\n      autostart: " + boolString(autostart) + "\n"
		if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	writeReloadConfig(false)
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("config.LoadConfig() error = %v", err)
	}
	s, err := supervisor.New(cfg)
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	daemon, err := backend.New(s, configPath)
	if err != nil {
		t.Fatalf("backend.New() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	writeReloadConfig(true)
	reloadOnSignal(context.Background(), daemon, logger)

	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot := s.Snapshot()
		if len(snapshot.Programs) == 1 && len(snapshot.Programs[0].Instances) == 1 &&
			snapshot.Programs[0].Instances[0].State == process.RUNNING {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("autostarted instance did not reach running, snapshot = %+v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReloadOnSignalReportsInvalidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "taskmaster.yaml")
	if err := os.WriteFile(configPath, []byte("taskmaster:\n  worker:\n    cmd: [\"/bin/sleep\", \"30\"]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("config.LoadConfig() error = %v", err)
	}
	s, err := supervisor.New(cfg)
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	daemon, err := backend.New(s, configPath)
	if err != nil {
		t.Fatalf("backend.New() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := os.WriteFile(configPath, []byte("taskmaster: {}\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	// Must not panic; the failure is only observable via the logger.
	reloadOnSignal(context.Background(), daemon, logger)

	snapshot := s.Snapshot()
	if len(snapshot.Programs) != 1 {
		t.Fatalf("snapshot = %+v, want the original program preserved", snapshot)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
