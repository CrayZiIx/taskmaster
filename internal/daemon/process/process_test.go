package process

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

func TestNewRejectsEmptyCommand(t *testing.T) {
	_, err := New("empty", config.ConfigurationProgram{})
	if err == nil {
		t.Fatal("New() expected an error for an empty command")
	}
}

func TestNewConfiguresProcess(t *testing.T) {
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout.log")
	stderrPath := filepath.Join(dir, "stderr.log")

	spec := config.ConfigurationProgram{
		Command:     []string{"/bin/echo", "hello"},
		Workdir:     dir,
		Environment: map[string]string{"TASKMASTER_TEST": "configured"},
		Output: config.OutputType{
			Stdout: stdoutPath,
			Stderr: stderrPath,
		},
	}

	p, err := New("test-process", spec)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if p.Name != "test-process" {
		t.Fatalf("Name = %q, want %q", p.Name, "test-process")
	}
	if p.Cmd.Dir != dir {
		t.Fatalf("Cmd.Dir = %q, want %q", p.Cmd.Dir, dir)
	}
	if p.Cmd.Stdout != p.stdoutFile {
		t.Fatal("configured stdout file was not assigned to the command")
	}
	if p.Cmd.Stderr != p.stderrFile {
		t.Fatal("configured stderr file was not assigned to the command")
	}
	if got := environmentValue(p.Cmd.Env, "TASKMASTER_TEST"); got != "configured" {
		t.Fatalf("TASKMASTER_TEST = %q, want %q", got, "configured")
	}

	if err := p.closeOutputFiles(); err != nil {
		t.Fatalf("closeOutputFiles() error = %v", err)
	}
}

func TestProcessWaitSuccess(t *testing.T) {
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout.log")
	stderrPath := filepath.Join(dir, "stderr.log")

	p, err := New("success", config.ConfigurationProgram{
		Command: []string{"/bin/sh", "-c", "printf 'hello'; printf 'warning' >&2"},
		Output: config.OutputType{
			Stdout: stdoutPath,
			Stderr: stderrPath,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if p.Status() != RUNNING {
		t.Fatalf("Status() after Start = %q, want %q", p.Status(), RUNNING)
	}

	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if p.Status() != EXITED {
		t.Fatalf("Status() after Wait = %q, want %q", p.Status(), EXITED)
	}
	if p.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", p.ExitCode)
	}

	assertFileContent(t, stdoutPath, "hello")
	assertFileContent(t, stderrPath, "warning")
	if p.stdoutFile != nil || p.stderrFile != nil {
		t.Fatal("output files were not released after Wait")
	}
}

func TestProcessWaitFailure(t *testing.T) {
	p, err := New("failure", config.ConfigurationProgram{
		Command: []string{"/bin/sh", "-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() error = %v, want nil for a completed child exit", err)
	}
	if p.Status() != EXITED_WERROR {
		t.Fatalf("Status() = %q, want %q", p.Status(), EXITED_WERROR)
	}
	if p.GetExitCode() != 7 {
		t.Fatalf("ExitCode = %d, want 7", p.GetExitCode())
	}
	result := p.Result()
	if result.ExitCode != 7 || result.ExitedBySignal || result.WaitErr != nil {
		t.Fatalf("Result() = %+v, want exit code 7 without signal or wait error", result)
	}
}

func TestNewInstancesAreIndependent(t *testing.T) {
	instances, err := NewInstances("sleep", config.ConfigurationProgram{
		Command:   []string{"/bin/sleep", "30"},
		ProcessNb: 3,
		Output: config.OutputType{
			Stdout: "/dev/null",
			Stderr: "/dev/null",
		},
	})
	if err != nil {
		t.Fatalf("NewInstances() error = %v", err)
	}
	if len(instances) != 3 {
		t.Fatalf("len(instances) = %d, want 3", len(instances))
	}

	for index, instance := range instances {
		wantName := "sleep[" + string(rune('1'+index)) + "]"
		if instance.Name != wantName {
			t.Errorf("instance %d name = %q, want %q", index, instance.Name, wantName)
		}
		if instance.InstanceNumber != uint(index+1) {
			t.Errorf("instance %d number = %d, want %d", index, instance.InstanceNumber, index+1)
		}
		if err := instance.Start(); err != nil {
			t.Fatalf("instance %d Start() error = %v", index, err)
		}
	}

	pids := make(map[int]struct{}, len(instances))
	for _, instance := range instances {
		if instance.Cmd.Process == nil {
			t.Fatal("started instance has no process")
		}
		pids[instance.Cmd.Process.Pid] = struct{}{}
	}
	if len(pids) != len(instances) {
		t.Fatalf("distinct process IDs = %d, want %d", len(pids), len(instances))
	}

	for _, instance := range instances {
		if err := instance.Stop(); err != nil {
			t.Fatalf("Stop(%s) error = %v", instance.Name, err)
		}
	}
}

func TestProcessRejectsInvalidLifecycleOperations(t *testing.T) {
	p, err := New("lifecycle", config.ConfigurationProgram{Command: []string{"/bin/echo", "ok"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !errors.Is(p.Wait(), ErrNotStarted) {
		t.Fatal("Wait() before Start should return ErrNotStarted")
	}
	if !errors.Is(p.Stop(), ErrNotStarted) {
		t.Fatal("Stop() before Start should return ErrNotStarted")
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !errors.Is(p.Start(), ErrAlreadyStarted) {
		t.Fatal("second Start() should return ErrAlreadyStarted")
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !errors.Is(p.Stop(), ErrAlreadyStopped) {
		t.Fatal("Stop() after exit should return ErrAlreadyStopped")
	}
}

func TestConcurrentStartOnlyStartsOnce(t *testing.T) {
	p, err := New("concurrent-start", config.ConfigurationProgram{
		Command: []string{"/bin/sleep", "1"},
		Output:  config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	for range 2 {
		go func() {
			defer group.Done()
			results <- p.Start()
		}()
	}
	group.Wait()
	close(results)

	var successes, lifecycleErrors int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrAlreadyStarted) || errors.Is(err, ErrStarting) {
			lifecycleErrors++
		} else {
			t.Fatalf("unexpected concurrent Start() error = %v", err)
		}
	}
	if successes != 1 || lifecycleErrors != 1 {
		t.Fatalf("concurrent starts = %d successes, %d lifecycle errors; want 1 and 1", successes, lifecycleErrors)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestProcessStopsWithConfiguredSignal(t *testing.T) {
	p, err := New("graceful", config.ConfigurationProgram{
		Command: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
		Journey: config.JourneyType{
			Stop: config.StopType{Signal: "SIGTERM"},
			Exit: config.ExitType{Timeout: 1000},
		},
		Output: config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	result := p.Result()
	if !result.StopRequested || result.TimedOut {
		t.Fatalf("Result() = %+v, want graceful stop without timeout", result)
	}
}

func TestProcessEscalatesAfterStopTimeout(t *testing.T) {
	p, err := New("timeout", config.ConfigurationProgram{
		Command: []string{"/bin/sleep", "30"},
		Journey: config.JourneyType{
			Stop: config.StopType{Signal: "SIGCONT"},
			Exit: config.ExitType{Timeout: 50},
		},
		Output: config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	result := p.Result()
	if !result.StopRequested || !result.TimedOut || result.Signal != "SIGKILL" {
		t.Fatalf("Result() = %+v, want SIGKILL timeout result", result)
	}
}

func TestFailedStartClosesOutputsAndCompletesWait(t *testing.T) {
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "stdout.log")
	p, err := New("failed-start", config.ConfigurationProgram{
		Command: []string{"/bin/echo", "never"},
		Workdir: filepath.Join(dir, "missing"),
		Output: config.OutputType{
			Stdout: stdoutPath,
			Stderr: filepath.Join(dir, "stderr.log"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Start(); err == nil {
		t.Fatal("Start() expected an error for an invalid working directory")
	}
	if p.Status() != START_FAILED {
		t.Fatalf("Status() = %q, want %q", p.Status(), START_FAILED)
	}
	if err := p.Wait(); err == nil {
		t.Fatal("Wait() expected the start error")
	}
	if p.stdoutFile != nil || p.stderrFile != nil {
		t.Fatal("failed start did not close output files")
	}
}

func TestProcessGroupIsGoneAfterStop(t *testing.T) {
	p, err := New("group", config.ConfigurationProgram{
		Command: []string{"/bin/sleep", "30"},
		Output:  config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := p.Cmd.Process.Pid
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := syscall.Kill(-pid, 0); err == nil {
		t.Fatal("process group still exists after Stop()")
	}
}

func TestProcessAppliesUmaskInChild(t *testing.T) {
	dir := t.TempDir()
	createdPath := filepath.Join(dir, "created-by-child")

	p, err := New("umask", config.ConfigurationProgram{
		Command: []string{"/bin/sh", "-c", "touch created-by-child"},
		Workdir: dir,
		Umask:   "077",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	info, err := os.Stat(createdPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("child file mode = %o, want 600", got)
	}
}

func environmentValue(environment []string, key string) string {
	prefix := key + "="
	for i := len(environment) - 1; i >= 0; i-- {
		if after, ok := strings.CutPrefix(environment[i], prefix); ok {
			return after
		}
	}
	return ""
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %q = %q, want %q", path, string(content), want)
	}
}
