// Package backend exposes the daemon operations needed by the TUI.
//
// It is intentionally an in-process API. Process creation, monitoring,
// restart policy, and cleanup remain owned by the supervisor package.
package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
	"github.com/CrayZiIx/taskmaster/internal/daemon/supervisor"
)

type Action string

const (
	ActionList     Action = "list"
	ActionStatus   Action = "status"
	ActionStart    Action = "start"
	ActionStop     Action = "stop"
	ActionRestart  Action = "restart"
	ActionReload   Action = "reload"
	ActionShutdown Action = "shutdown"
)

var (
	ErrUnknownAction         = errors.New("unknown backend action")
	ErrProgramRequired       = errors.New("program is required")
	ErrConfigPathRequired    = errors.New("configuration path is required")
	ErrConfigurationReload   = errors.New("configuration reload failed")
	ErrProgramNotFound       = supervisor.ErrProgramNotFound
	ErrProgramAlreadyStopped = supervisor.ErrProgramAlreadyStopped
)

// Request is the stable command shape used by the TUI.
// ConfigPath is only used by ActionReload; when omitted, the path supplied to
// New is used.
type Request struct {
	Action     Action
	Program    string
	ConfigPath string
}

// Response contains an immutable copy of the supervisor's current status.
// Every action returns a response, including actions that do not change state.
type Response struct {
	ShuttingDown bool
	Reloaded     bool
	Programs     []ProgramStatus
}

type ProgramStatus struct {
	Name      string
	Instances []InstanceStatus
}

type InstanceStatus struct {
	Name            string
	InstanceNumber  uint
	PID             int
	State           string
	Healthy         bool
	RestartCount    uint
	RestartDisabled bool
	LastExit        ExitStatus
	LastError       string
}

type ExitStatus struct {
	ExitCode       int
	Signal         string
	ExitedBySignal bool
	StopRequested  bool
	TimedOut       bool
	WaitError      string
}

type Backend struct {
	supervisor *supervisor.Supervisor
	configPath string
}

func New(s *supervisor.Supervisor, configPath string) (*Backend, error) {
	if s == nil {
		return nil, fmt.Errorf("supervisor must not be nil")
	}
	return &Backend{supervisor: s, configPath: strings.TrimSpace(configPath)}, nil
}

// Execute validates and executes one TUI action.
func (b *Backend) Execute(ctx context.Context, request Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}

	switch request.Action {
	case ActionList:
		return b.response(false, ""), nil
	case ActionStatus:
		response := b.response(false, request.Program)
		if request.Program != "" && len(response.Programs) == 0 {
			return response, fmt.Errorf("%w: %s", ErrProgramNotFound, request.Program)
		}
		return response, nil
	case ActionStart, ActionStop, ActionRestart:
		if strings.TrimSpace(request.Program) == "" {
			return Response{}, ErrProgramRequired
		}
		var err error
		switch request.Action {
		case ActionStart:
			err = b.supervisor.StartProgram(request.Program)
		case ActionStop:
			err = b.supervisor.StopProgram(request.Program)
		case ActionRestart:
			err = b.supervisor.RestartProgram(request.Program)
		}
		response := b.response(false, "")
		if err != nil {
			return response, err
		}
		return response, nil
	case ActionReload:
		path := strings.TrimSpace(request.ConfigPath)
		if path == "" {
			path = b.configPath
		}
		if path == "" {
			return Response{}, ErrConfigPathRequired
		}
		cfg, err := config.LoadConfig(path)
		if err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrConfigurationReload, err)
		}
		if err := b.supervisor.Reload(cfg); err != nil {
			return b.response(false, ""), fmt.Errorf("%w: %v", ErrConfigurationReload, err)
		}
		return b.response(true, ""), nil
	case ActionShutdown:
		err := b.supervisor.Shutdown()
		response := b.response(false, "")
		if err != nil {
			return response, err
		}
		return response, nil
	default:
		return Response{}, fmt.Errorf("%w: %q", ErrUnknownAction, request.Action)
	}
}

func (b *Backend) response(reloaded bool, programName string) Response {
	snapshot := b.supervisor.Snapshot()
	response := Response{ShuttingDown: snapshot.ShuttingDown, Reloaded: reloaded}
	for _, program := range snapshot.Programs {
		if programName != "" && program.Name != programName {
			continue
		}
		programStatus := ProgramStatus{Name: program.Name, Instances: make([]InstanceStatus, 0, len(program.Instances))}
		for _, instance := range program.Instances {
			programStatus.Instances = append(programStatus.Instances, InstanceStatus{
				Name:            instance.Name,
				InstanceNumber:  instance.InstanceNumber,
				PID:             instance.PID,
				State:           string(instance.State),
				Healthy:         instance.Healthy,
				RestartCount:    instance.RestartCount,
				RestartDisabled: instance.RestartDisabled,
				LastExit:        exitStatus(instance.LastExit),
				LastError:       instance.LastError,
			})
		}
		response.Programs = append(response.Programs, programStatus)
	}
	return response
}

func exitStatus(info process.ExitInfo) ExitStatus {
	status := ExitStatus{
		ExitCode:       info.ExitCode,
		Signal:         info.Signal,
		ExitedBySignal: info.ExitedBySignal,
		StopRequested:  info.StopRequested,
		TimedOut:       info.TimedOut,
	}
	if info.WaitErr != nil {
		status.WaitError = info.WaitErr.Error()
	}
	return status
}
