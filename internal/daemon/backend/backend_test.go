package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
)

func TestBackendExecutesLifecycleActions(t *testing.T) {
	initial := backendConfig("worker", 1, false)
	s, err := supervisor.New(initial)
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	b, err := New(s, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := b.Execute(context.Background(), Request{Action: ActionList})
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if len(response.Programs) != 1 || response.Programs[0].Name != "worker" {
		t.Fatalf("list response = %+v", response)
	}

	response, err = b.Execute(context.Background(), Request{Action: ActionStart, Program: "worker"})
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	waitForBackend(t, b, func(response Response) bool {
		return response.Programs[0].Instances[0].State == string(process.RUNNING)
	})

	response, err = b.Execute(context.Background(), Request{Action: ActionStatus, Program: "worker"})
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if len(response.Programs) != 1 || response.Programs[0].Instances[0].PID <= 0 {
		t.Fatalf("status response = %+v", response)
	}

	if _, err := b.Execute(context.Background(), Request{Action: ActionStop, Program: "worker"}); err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestBackendReloadsConfigurationFromPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskmaster.yaml")
	if err := os.WriteFile(path, []byte("taskmaster:\n  worker:\n    cmd: [/bin/sleep, '30']\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	initial := backendConfig("old", 1, false)
	s, err := supervisor.New(initial)
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	b, err := New(s, path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := b.Execute(context.Background(), Request{Action: ActionReload})
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if !response.Reloaded || len(response.Programs) != 1 || response.Programs[0].Name != "worker" {
		t.Fatalf("reload response = %+v", response)
	}

	if _, err := b.Execute(context.Background(), Request{Action: ActionReload, ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")}); !errors.Is(err, ErrConfigurationReload) {
		t.Fatalf("missing reload error = %v, want ErrConfigurationReload", err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestBackendReturnsStableValidationErrors(t *testing.T) {
	s, err := supervisor.New(backendConfig("worker", 1, false))
	if err != nil {
		t.Fatalf("supervisor.New() error = %v", err)
	}
	b, err := New(s, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := b.Execute(context.Background(), Request{Action: ActionStart}); !errors.Is(err, ErrProgramRequired) {
		t.Fatalf("missing program error = %v, want ErrProgramRequired", err)
	}
	if _, err := b.Execute(context.Background(), Request{Action: "unknown"}); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("unknown action error = %v, want ErrUnknownAction", err)
	}
	if _, err := b.Execute(context.Background(), Request{Action: ActionStatus, Program: "missing"}); !errors.Is(err, ErrProgramNotFound) {
		t.Fatalf("missing status error = %v, want ErrProgramNotFound", err)
	}
	if _, err := b.Execute(context.Background(), Request{Action: ActionReload}); !errors.Is(err, ErrConfigPathRequired) {
		t.Fatalf("missing path error = %v, want ErrConfigPathRequired", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Execute(canceled, Request{Action: ActionList}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func backendConfig(name string, processNb uint, autostart bool) *config.ConfigurationFile {
	return &config.ConfigurationFile{Programs: map[string]config.ConfigurationProgram{
		name: {
			Command:   []string{"/bin/sleep", "30"},
			ProcessNb: processNb,
			Output:    config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
			Journey: config.JourneyType{
				AutoStart: autostart,
				RestartPolicy: config.RestartPolicyType{
					RestartCase: string(config.RestartNever),
				},
				Exit: config.ExitType{ExitCodes: []int{0}, Timeout: 100},
				Stop: config.StopType{Signal: "SIGTERM"},
			},
		},
	}}
}

func waitForBackend(t *testing.T, b *Backend, condition func(Response) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := b.Execute(context.Background(), Request{Action: ActionList})
		if err != nil {
			t.Fatalf("status polling error = %v", err)
		}
		if condition(response) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	response, _ := b.Execute(context.Background(), Request{Action: ActionList})
	t.Fatalf("condition not reached; final response = %+v", response)
}
