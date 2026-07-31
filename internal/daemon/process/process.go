package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

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
	Name string
	Conf config.ConfigurationProgram
	Cmd  *exec.Cmd

	State    State
	ExitCode int

	stdoutFile *os.File
	stderrFile *os.File

	done    chan struct{}
	waitErr error
	mu      sync.RWMutex
}

func (p *Process) closeOutputFiles() error {
	var err error

	if p.stdoutFile != nil {
		err = errors.Join(err, p.stdoutFile.Close())
		p.stdoutFile = nil
	}

	if p.stderrFile != nil {
		err = errors.Join(err, p.stderrFile.Close())
		p.stderrFile = nil
	}

	return err
}

func New(name string, conf config.ConfigurationProgram) (*Process, error) {
	if len(conf.Command) == 0 {
		return nil, fmt.Errorf("error while creating the process")
	}
	cmd := exec.Command(conf.Command[0], conf.Command[1:]...)
	cmd.Dir = conf.Workdir

	cmd.Env = os.Environ()

	for key, value := range conf.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	var err error
	var stdoutFile *os.File
	var stderrFile *os.File
	if conf.Output.Stdout == "" {
		cmd.Stdout = os.Stdout
	} else {
		stdoutFile, err = os.OpenFile(conf.Output.Stdout, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("start: %w", err)
		}
		cmd.Stdout = stdoutFile
	}
	if conf.Output.Stderr == "" {
		cmd.Stderr = os.Stderr
	} else {
		stderrFile, err = os.OpenFile(conf.Output.Stderr, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			if stdoutFile != nil {
				stdoutFile.Close()
			}
			return nil, fmt.Errorf("start: %w", err)
		}
		cmd.Stderr = stderrFile
	}

	return &Process{
		Name:       name,
		Conf:       conf,
		Cmd:        cmd,
		State:      NOT_STARTED,
		stdoutFile: stdoutFile,
		stderrFile: stderrFile,
		done:       make(chan struct{}),
	}, nil
}

func (p *Process) Start() error {
	if err := p.Cmd.Start(); err != nil {
		p.closeOutputFiles()
		return fmt.Errorf("start: %w", err)
	}
	p.mu.Lock()
	p.State = RUNNING
	p.mu.Unlock()

	go p.waitForExit()

	return nil
}

func (p *Process) Wait() error {
	<-p.done

	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.waitErr
}

func (p *Process) waitForExit() {
	err := p.Cmd.Wait()

	closeErr := p.closeOutputFiles()
	err = errors.Join(err, closeErr)

	p.mu.Lock()
	p.waitErr = err
	if p.Cmd.ProcessState != nil {
		p.ExitCode = p.Cmd.ProcessState.ExitCode()
	}

	if err != nil {
		p.State = EXITED_WERROR
	} else {
		p.State = EXITED
	}

	p.mu.Unlock()

	close(p.done)
}

func (p *Process) Kill() error {
	if err := p.Cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	return nil
}

func (p *Process) Stop() error {
	if err := p.Kill(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	if err := p.Wait(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return nil
}

func (p *Process) Status() State {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.State
}

func (p *Process) GetExitCode() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.ExitCode
}
