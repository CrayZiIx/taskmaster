package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeConfig(t *testing.T) {
	input := `
taskmaster:
  my-program:
    workdir: "/var/lib/temp"
    umask: "022"
    cmd:
      - "/usr/bin/my-program"
      - "-v"
    journey:
      autostart: true
      health-time: 1000
      restart-policy:
        restart-case: "unexpected"
        restart-nb: 3
      exit:
        code: [0]
        signal: ["SIGTERM"]
        timeout-ms: 5000
    process-nb: 1
    output:
      stdout: "/dev/null"
      stderr: "/dev/null"
    env:
      APP_ENV: "prod"
      LOG_LEVEL: "info"

  my-program-2:
    cmd:
      - "/usr/bin/my-program-2"
`

	cfg, err := decodeConfig(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	want := map[string]ConfigurationProgram{
		"my-program": {
			Command:   []string{"/usr/bin/my-program", "-v"},
			Workdir:   "/var/lib/temp",
			Umask:     "022",
			ProcessNb: 1,
			Journey: JourneyType{
				AutoStart:  true,
				HealthTime: 1000,
				RestartPolicy: RestartPolicyType{
					RestartCase: "unexpected",
					RestartNb:   3,
				},
				Exit: ExitType{
					ExitCodes:   []int{0},
					ExitSignals: []string{"SIGTERM"},
					Timeout:     5000,
				},
			},
			Output: OutputType{
				Stdout: "/dev/null",
				Stderr: "/dev/null",
			},
			Environment: map[string]string{
				"APP_ENV":   "prod",
				"LOG_LEVEL": "info",
			},
		},
		"my-program-2": {
			Command: []string{"/usr/bin/my-program-2"},
		},
	}

	if !reflect.DeepEqual(cfg.Programs, want) {
		t.Errorf("Programs = %#v, want %#v", cfg.Programs, want)
	}
}

func TestDecodeConfigRejectsUnknownField(t *testing.T) {
	input := `
taskmaster:
  my-program:
    unknown-field: true
`

	_, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() expected an error for an unknown field")
	}
}

func TestDecodeConfigRejectsInvalidYAML(t *testing.T) {
	input := "taskmaster: ["

	_, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() expected an error for invalid YAML")
	}
}

func TestParseConfigAppliesDefaults(t *testing.T) {
	cfg, err := ParseConfig(strings.NewReader(`
taskmaster:
  worker:
    cmd: ["echo", "hello"]
`))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	program := cfg.Programs["worker"]
	if program.ProcessNb != 1 {
		t.Fatalf("ProcessNb = %d, want 1", program.ProcessNb)
	}
	if program.Umask != "022" {
		t.Fatalf("Umask = %q, want 022", program.Umask)
	}
	if program.Journey.RestartPolicy.RestartCase != "never" {
		t.Fatalf("RestartCase = %q, want never", program.Journey.RestartPolicy.RestartCase)
	}
	if !reflect.DeepEqual(program.Journey.Exit.ExitCodes, []int{0}) {
		t.Fatalf("ExitCodes = %#v, want [0]", program.Journey.Exit.ExitCodes)
	}
	if program.Journey.Exit.Timeout != 5000 {
		t.Fatalf("Timeout = %d, want 5000", program.Journey.Exit.Timeout)
	}
	if program.Journey.Stop.Signal != "SIGTERM" {
		t.Fatalf("Stop.Signal = %q, want SIGTERM", program.Journey.Stop.Signal)
	}
}

func TestParseConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "missing taskmaster programs",
			input: "taskmaster: {}",
			want:  "at least one program",
		},
		{
			name:  "missing command",
			input: "taskmaster:\n  worker: {}",
			want:  "taskmaster.worker.cmd",
		},
		{
			name:  "zero process count",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    process-nb: 0",
			want:  "taskmaster.worker.process-nb",
		},
		{
			name:  "invalid umask",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    umask: 088",
			want:  "taskmaster.worker.umask",
		},
		{
			name:  "invalid restart case",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    journey:\n      restart-policy:\n        restart-case: sometimes",
			want:  "restart-case",
		},
		{
			name:  "restart count with never",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    journey:\n      restart-policy:\n        restart-case: never\n        restart-nb: 1",
			want:  "restart-nb",
		},
		{
			name:  "invalid exit code",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    journey:\n      exit:\n        code: [256]",
			want:  "exit.code",
		},
		{
			name:  "invalid signal",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    journey:\n      exit:\n        signal: [TERM]",
			want:  "exit.signal",
		},
		{
			name:  "invalid stop signal",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    journey:\n      stop:\n        signal: UNKNOWN",
			want:  "journey.stop.signal",
		},
		{
			name:  "invalid environment name",
			input: "taskmaster:\n  worker:\n    cmd: [echo]\n    env:\n      BAD-NAME: value",
			want:  "invalid environment variable name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("ParseConfig() expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestParseConfigRejectsMultipleDocuments(t *testing.T) {
	_, err := ParseConfig(strings.NewReader("taskmaster:\n  worker:\n    cmd: [echo]\n---\ntaskmaster: {}"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("ParseConfig() error = %v, want multiple-document error", err)
	}
}

func TestLoadConfigUsesExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskmaster.yaml")
	if err := os.WriteFile(path, []byte("taskmaster:\n  worker:\n    cmd: [echo]"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if _, ok := cfg.Programs["worker"]; !ok {
		t.Fatal("LoadConfig() did not load worker")
	}
}

func TestManagerReloadIsAtomic(t *testing.T) {
	initial, err := ParseConfig(strings.NewReader("taskmaster:\n  old:\n    cmd: [echo, old]"))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	manager, err := NewManager(initial)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if _, err := manager.Reload(strings.NewReader("taskmaster:\n  broken:\n    cmd: []")); err == nil {
		t.Fatal("Reload() expected an error")
	}
	current := manager.Current()
	if _, ok := current.Programs["old"]; !ok {
		t.Fatal("failed reload replaced the current configuration")
	}

	if _, err := manager.Reload(strings.NewReader("taskmaster:\n  new:\n    cmd: [echo, new]")); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	current = manager.Current()
	if _, ok := current.Programs["new"]; !ok {
		t.Fatal("successful reload did not replace the current configuration")
	}
}
