package process

import (
	"os"
	"path/filepath"
	"strings"
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
	if err := p.Wait(); err == nil {
		t.Fatal("Wait() expected an error")
	}
	if p.Status() != EXITED_WERROR {
		t.Fatalf("Status() = %q, want %q", p.Status(), EXITED_WERROR)
	}
	if p.GetExitCode() != 7 {
		t.Fatalf("ExitCode = %d, want 7", p.GetExitCode())
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
