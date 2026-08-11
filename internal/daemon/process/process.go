package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

type State string

const (
	NOT_STARTED   State = "not_started"
	STARTING      State = "starting"
	RUNNING       State = "running"
	STOPPING      State = "stopping"
	EXITED        State = "exited_without_error"
	EXITED_WERROR State = "exited_with_error"
	START_FAILED  State = "start_failed"
)

var (
	ErrAlreadyStarted  = errors.New("process has already started")
	ErrNotStarted      = errors.New("process has not started")
	ErrAlreadyStopped  = errors.New("process has already exited")
	ErrStarting        = errors.New("process is still starting")
	ErrAlreadyStopping = errors.New("process is already stopping")
)

type ExitInfo struct {
	ExitCode       int
	Signal         string
	ExitedBySignal bool
	StopRequested  bool
	TimedOut       bool
	WaitErr        error
}

type Process struct {
	Name           string
	InstanceNumber uint
	Conf           config.ConfigurationProgram
	Cmd            *exec.Cmd

	State    State
	ExitCode int

	stdoutFile *os.File
	stderrFile *os.File

	done     chan struct{}
	doneOnce sync.Once
	waitErr  error
	exitInfo ExitInfo

	outputMu     sync.Mutex
	outputClose  error
	outputClosed bool
	mu           sync.RWMutex
}

func (p *Process) closeOutputFiles() error {
	p.outputMu.Lock()
	defer p.outputMu.Unlock()
	if p.outputClosed {
		return p.outputClose
	}
	p.outputClosed = true

	var err error
	if p.stdoutFile != nil {
		err = errors.Join(err, p.stdoutFile.Close())
		p.stdoutFile = nil
	}
	if p.stderrFile != nil {
		err = errors.Join(err, p.stderrFile.Close())
		p.stderrFile = nil
	}
	p.outputClose = err
	return err
}

// New creates a single process without an instance suffix. NewInstances and
// NewInstance are used by the supervisor when process-nb is greater than one.
func New(name string, conf config.ConfigurationProgram) (*Process, error) {
	return newProcess(name, 0, conf)
}

func NewInstance(programName string, instanceNumber uint, conf config.ConfigurationProgram) (*Process, error) {
	if instanceNumber == 0 {
		return nil, fmt.Errorf("instance number must be at least 1")
	}
	return newProcess(fmt.Sprintf("%s[%d]", programName, instanceNumber), instanceNumber, conf)
}

func NewInstances(programName string, conf config.ConfigurationProgram) ([]*Process, error) {
	if err := conf.Normalize(); err != nil {
		return nil, fmt.Errorf("configure process instances for %q: %w", programName, err)
	}

	instances := make([]*Process, 0, conf.ProcessNb)
	for instanceNumber := uint(1); instanceNumber <= conf.ProcessNb; instanceNumber++ {
		instance, err := NewInstance(programName, instanceNumber, conf)
		if err != nil {
			for _, created := range instances {
				_ = created.closeOutputFiles()
			}
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func newProcess(name string, instanceNumber uint, conf config.ConfigurationProgram) (*Process, error) {
	if err := conf.Normalize(); err != nil {
		return nil, fmt.Errorf("configure process %q: %w", name, err)
	}

	cmd := commandFor(conf)
	cmd.Dir = conf.Workdir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = mergedEnvironment(conf.Environment)

	var err error
	var stdoutFile *os.File
	var stderrFile *os.File
	if conf.Output.Stdout == "" {
		cmd.Stdout = os.Stdout
	} else {
		stdoutFile, err = os.OpenFile(conf.Output.Stdout, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("open stdout for %q: %w", name, err)
		}
		cmd.Stdout = stdoutFile
	}
	if conf.Output.Stderr == "" {
		cmd.Stderr = os.Stderr
	} else {
		stderrFile, err = os.OpenFile(conf.Output.Stderr, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			if stdoutFile != nil {
				_ = stdoutFile.Close()
			}
			return nil, fmt.Errorf("open stderr for %q: %w", name, err)
		}
		cmd.Stderr = stderrFile
	}

	return &Process{
		Name:           name,
		InstanceNumber: instanceNumber,
		Conf:           conf,
		Cmd:            cmd,
		State:          NOT_STARTED,
		ExitCode:       -1,
		stdoutFile:     stdoutFile,
		stderrFile:     stderrFile,
		done:           make(chan struct{}),
		exitInfo:       ExitInfo{ExitCode: -1},
	}, nil
}

// commandFor applies umask in the child process. Using syscall.Umask in the
// parent around Cmd.Start would race with concurrent process starts and would
// temporarily alter the daemon's own umask.
func commandFor(conf config.ConfigurationProgram) *exec.Cmd {
	args := []string{"-c", "umask \"$1\"\nshift\nexec \"$@\"", "taskmaster-umask", conf.Umask, conf.Command[0]}
	args = append(args, conf.Command[1:]...)
	return exec.Command("/bin/sh", args...)
}

func mergedEnvironment(overrides map[string]string) []string {
	environment := os.Environ()
	positions := make(map[string]int, len(environment)+len(overrides))
	for index, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			positions[key] = index
		}
	}
	for key, value := range overrides {
		entry := fmt.Sprintf("%s=%s", key, value)
		if index, ok := positions[key]; ok {
			environment[index] = entry
			continue
		}
		positions[key] = len(environment)
		environment = append(environment, entry)
	}
	return environment
}

func (p *Process) Start() error {
	p.mu.Lock()
	switch p.State {
	case NOT_STARTED:
		p.State = STARTING
	case STARTING:
		p.mu.Unlock()
		return ErrStarting
	case RUNNING, STOPPING:
		p.mu.Unlock()
		return ErrAlreadyStarted
	case EXITED, EXITED_WERROR:
		p.mu.Unlock()
		return ErrAlreadyStopped
	case START_FAILED:
		p.mu.Unlock()
		return ErrAlreadyStarted
	}
	p.mu.Unlock()

	if err := p.Cmd.Start(); err != nil {
		startErr := fmt.Errorf("start: %w", err)
		_ = p.closeOutputFiles()
		p.mu.Lock()
		p.State = START_FAILED
		p.waitErr = startErr
		p.exitInfo.WaitErr = startErr
		p.mu.Unlock()
		p.closeDone()
		return startErr
	}

	p.mu.Lock()
	p.State = RUNNING
	p.mu.Unlock()
	go p.waitForExit()
	return nil
}

func (p *Process) Wait() error {
	p.mu.RLock()
	state := p.State
	done := p.done
	p.mu.RUnlock()

	switch state {
	case NOT_STARTED:
		return ErrNotStarted
	case STARTING:
		return ErrStarting
	}

	<-done
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.waitErr
}

func (p *Process) waitForExit() {
	childErr := p.Cmd.Wait()
	cleanupErr := p.closeOutputFiles()

	var exitErr *exec.ExitError
	waitErr := cleanupErr
	if childErr != nil && !errors.As(childErr, &exitErr) {
		waitErr = errors.Join(waitErr, childErr)
	}

	info := ExitInfo{ExitCode: -1, WaitErr: waitErr}
	if p.Cmd.ProcessState != nil {
		info.ExitCode = p.Cmd.ProcessState.ExitCode()
		if status, ok := p.Cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			info.ExitedBySignal = true
			info.Signal = signalName(status.Signal())
		}
	}

	p.mu.Lock()
	info.StopRequested = p.exitInfo.StopRequested
	info.TimedOut = p.exitInfo.TimedOut
	p.exitInfo = info
	p.waitErr = waitErr
	p.ExitCode = info.ExitCode
	if waitErr != nil || info.ExitedBySignal || info.ExitCode != 0 {
		p.State = EXITED_WERROR
	} else {
		p.State = EXITED
	}
	p.mu.Unlock()
	p.closeDone()
}

func (p *Process) Stop() error {
	p.mu.Lock()
	if p.State == STOPPING {
		p.mu.Unlock()
		return ErrAlreadyStopping
	}
	if p.State != RUNNING {
		state := p.State
		p.mu.Unlock()
		if state == NOT_STARTED || state == STARTING {
			return ErrNotStarted
		}
		return ErrAlreadyStopped
	}
	p.State = STOPPING
	p.exitInfo.StopRequested = true
	pid := p.Cmd.Process.Pid
	p.mu.Unlock()

	stopSignal, err := config.SignalNumber(p.Conf.Journey.Stop.Signal)
	if err != nil {
		return fmt.Errorf("stop %q: %w", p.Name, err)
	}
	if err := signalProcessGroup(pid, stopSignal); err != nil && !errors.Is(err, syscall.ESRCH) {
		p.mu.Lock()
		if p.State == STOPPING {
			p.State = RUNNING
		}
		p.mu.Unlock()
		return fmt.Errorf("stop %q: %w", p.Name, err)
	}

	timer := time.NewTimer(time.Duration(p.Conf.Journey.Exit.Timeout) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.waitError()
	case <-timer.C:
		p.mu.Lock()
		p.exitInfo.TimedOut = true
		p.mu.Unlock()
		if err := signalProcessGroup(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("force stop %q: %w", p.Name, err)
		}
		<-p.done
		return p.waitError()
	}
}

// Kill forcefully terminates a running process group without waiting for it.
// Stop should be preferred when the configured graceful signal is appropriate.
func (p *Process) Kill() error {
	p.mu.Lock()
	if p.State != RUNNING && p.State != STOPPING {
		state := p.State
		p.mu.Unlock()
		if state == NOT_STARTED || state == STARTING {
			return ErrNotStarted
		}
		return ErrAlreadyStopped
	}
	if p.State == RUNNING {
		p.State = STOPPING
		p.exitInfo.StopRequested = true
	}
	pid := p.Cmd.Process.Pid
	p.mu.Unlock()

	if err := signalProcessGroup(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill %q: %w", p.Name, err)
	}
	return nil
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process ID %d", pid)
	}
	return syscall.Kill(-pid, signal)
}

func signalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGUSR1:
		return "SIGUSR1"
	case syscall.SIGUSR2:
		return "SIGUSR2"
	default:
		return signal.String()
	}
}

func (p *Process) waitError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.waitErr
}

func (p *Process) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

func (p *Process) Result() ExitInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitInfo
}

func (p *Process) Done() <-chan struct{} {
	return p.done
}

func (p *Process) PID() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.State == NOT_STARTED || p.State == STARTING || p.State == START_FAILED || p.Cmd.Process == nil {
		return 0
	}
	return p.Cmd.Process.Pid
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
