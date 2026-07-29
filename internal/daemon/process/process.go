package process

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

type State string

const (
	NOT_STARTED   State = "not_started"
	RUNNING       State = "running"
	EXITED        State = "exited_without_error"
	EXITED_WERROR State = "exited_with_error"
)

type Process struct {
	Name     string
	Conf     config.ConfigurationProgram
	Cmd      *exec.Cmd
	State    State
	ExitCode int
}

func New(name string, conf config.ConfigurationProgram) *Process {
	cmd := exec.Command(conf.Command[0], conf.Command[1:]...)
	cmd.Dir = conf.Workdir

	for key, value := range conf.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	if conf.Output.StdOut == "" {
		cmd.Stdout = os.Stdout
	}
	if conf.Output.StdErr == "" {
		cmd.Stderr = os.Stderr
	}

	return &Process{
		Name:  name,
		Conf:  conf,
		Cmd:   cmd,
		State: NOT_STARTED,
	}
}

func (p *Process) Start() error {
	err := p.Cmd.Start()
	if err != nil {
		return err
	}
	p.State = RUNNING
	return nil
}

func (p *Process) Wait() error {
	err := p.Cmd.Wait()
	if err != nil {
		return err
	}
	p.ExitCode = p.Cmd.ProcessState.ExitCode()
	switch {
	case p.ExitCode != 0:
		p.State = EXITED_WERROR
	default:
		p.State = EXITED
	}
	return nil
}

func (p *Process) Kill() error {
	err := p.Cmd.Process.Kill()
	if err != nil {
		return err
	}
	return nil
}

func (p *Process) Status() State {
	return p.State
}
