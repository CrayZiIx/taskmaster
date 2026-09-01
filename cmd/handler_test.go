package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
	"github.com/CrayZiIx/taskmaster/internal/tui"
)

func TestCommandHandlerMapsAllDaemonActions(t *testing.T) {
	s, err := supervisor.New(&config.ConfigurationFile{Programs: map[string]config.ConfigurationProgram{
		"worker": {
			Command:   []string{"/bin/sleep", "30"},
			ProcessNb: 1,
			Output:    config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
		},
	}})
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	daemon, err := backend.New(s, "")
	if err != nil {
		t.Fatalf("backend.New() error = %v", err)
	}
	handler := commandHandler(context.Background(), daemon)

	output, err := handler("list", nil)
	if err != nil || !strings.Contains(output, "worker[1]") || !strings.Contains(output, "STOPPED") {
		t.Fatalf("list = %q, error = %v", output, err)
	}
	if _, err := handler("start", []string{"worker"}); err != nil {
		t.Fatalf("start error = %v", err)
	}
	output, err = handler("status", []string{"worker"})
	if err != nil || !strings.Contains(output, "RUNNING") || !strings.Contains(output, "pid=") {
		t.Fatalf("status = %q, error = %v", output, err)
	}
	if _, err := handler("restart", []string{"worker"}); err != nil {
		t.Fatalf("restart error = %v", err)
	}
	if _, err := handler("stop", []string{"worker"}); err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if _, err := handler("quit", nil); !errors.Is(err, tui.ErrExit) {
		t.Fatalf("quit error = %v, want tui.ErrExit", err)
	}
}

func TestCommandHandlerValidatesCommands(t *testing.T) {
	s, err := supervisor.New(&config.ConfigurationFile{Programs: map[string]config.ConfigurationProgram{
		"worker": {Command: []string{"/bin/echo", "ok"}},
	}})
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	daemon, err := backend.New(s, "")
	if err != nil {
		t.Fatalf("backend.New() error = %v", err)
	}
	handler := commandHandler(context.Background(), daemon)

	tests := []struct {
		name string
		cmd  string
		args []string
		want string
	}{
		{name: "missing start argument", cmd: "start", want: "usage: start <program>"},
		{name: "extra list argument", cmd: "list", args: []string{"worker"}, want: "usage: list"},
		{name: "extra quit argument", cmd: "quit", args: []string{"worker"}, want: "usage: quit"},
		{name: "unknown command", cmd: "wat", want: "unknown command: wat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handler(tt.cmd, tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDisplayStateAndInstanceInfo(t *testing.T) {
	if got := displayState(string(process.RUNNING)); got != "RUNNING" {
		t.Fatalf("displayState(running) = %q", got)
	}
	if got := displayState(string(process.EXITED_WERROR)); got != "FATAL" {
		t.Fatalf("displayState(exited_with_error) = %q", got)
	}
	info := formatInstanceInfo(backend.InstanceStatus{
		PID:          42,
		Healthy:      true,
		RestartCount: 2,
		LastExit:     backend.ExitStatus{ExitCode: 7},
		LastError:    "unexpected exit",
	})
	for _, want := range []string{"pid=42", "healthy=true", "restarts=2", "exit=7", "error=unexpected exit"} {
		if !strings.Contains(info, want) {
			t.Fatalf("info = %q, want %q", info, want)
		}
	}
}
