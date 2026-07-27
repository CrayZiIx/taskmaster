package config

import (
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
				StdOut: "/dev/null",
				StdErr: "/dev/null",
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
