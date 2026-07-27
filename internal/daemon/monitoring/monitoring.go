package monitoring

import (
	"fmt"
	"os/exec"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

// handle monitoring
// translate exit code into human-readable output
// apply defined rules
// handle process journey (health, restart-policy, exit)
// handle env and output dir

type ProcessState string

const (
	NOT_STARTED   ProcessState = "not_started"
	RUNNING       ProcessState = "running"
	EXITED        ProcessState = "exited"
	EXITED_WERROR ProcessState = "exited_with_error"
)

type Process struct {
	ProcessName string
	Config      config.ConfigurationProgram
	Command     *exec.Cmd
}

func NewProcess(name string) (*Process, error) {
	return &Process{}, nil
}

func (p *Process) LoadConfig() error {
	p.Command = exec.Command(p.Config.Command[0], p.Config.Command[1:]...)
	p.Command.Dir = p.Config.Workdir
	return nil
}

func (p *Process) LoadEnv() error {
	for key, value := range p.Config.Environment {
		p.Command.Env = append(p.Command.Env, fmt.Sprintf("%s=%s", key, value))
	}
	return nil
}

func (p *Process) ExecCommand() error {
	output, err := p.Command.Output()
	if err != nil {
		return fmt.Errorf("exec command: %s, error = %s\n", p.Command.Args[0], err)
	}
	fmt.Println(">", p.Command.Args[0])
	fmt.Println(string(output))
	return nil
}

func (p *Process) GetProcessName() string {
	return p.ProcessName
}

func (p *Process) GetExitCode() int {
	return p.Command.ProcessState.ExitCode()
}
