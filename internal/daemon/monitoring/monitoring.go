package monitoring

import (
	"os/exec"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

// handle monitoring
// translate exit code into human-readable output
// apply defined rules
// handle process journey (health, restart-policy, exit)
// handle env and output dir

type Process struct {
	ProcessName string
	Config      config.ConfigurationProgram
	Command     *exec.Cmd
}

func NewProcess(name string) (*Process, error) {
	return &Process{ProcessName: name}, nil
}
