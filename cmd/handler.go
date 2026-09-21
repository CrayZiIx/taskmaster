package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/CrayZiIx/taskmaster/internal/daemon/backend"
	"github.com/CrayZiIx/taskmaster/internal/tui"
)

const helpText = `Available commands:
  help                         Show this help message.
  list                         List all configured program instances.
  status [program]             Show the status of all or one program.
  start <program>              Start a program.
  stop <program>               Stop a program gracefully.
  restart <program>            Restart a program.
  reload [config-path]         Reload and validate the configuration.
  quit                         Stop all programs and exit.
  exit                         Stop all programs and exit.
  shutdown                     Stop all programs and exit.
`

func commandHandler(ctx context.Context, daemon *backend.Backend) tui.CommandHandler {
	return func(command string, args []string) (string, error) {
		command = strings.ToLower(strings.TrimSpace(command))
		switch command {
		case "help":
			if len(args) != 0 {
				return "", fmt.Errorf("usage: help")
			}
			return helpText, nil
		case "list":
			if len(args) != 0 {
				return "", fmt.Errorf("usage: list")
			}
			response, err := daemon.Execute(ctx, backend.Request{Action: backend.ActionList})
			if err != nil {
				return "", err
			}
			return formatStatus(response), nil
		case "status":
			if len(args) > 1 {
				return "", fmt.Errorf("usage: status [program]")
			}
			request := backend.Request{Action: backend.ActionStatus}
			if len(args) == 1 {
				request.Program = args[0]
			}
			response, err := daemon.Execute(ctx, request)
			if err != nil {
				return "", err
			}
			return formatStatus(response), nil
		case "start", "stop", "restart":
			if len(args) != 1 {
				return "", fmt.Errorf("usage: %s <program>", command)
			}
			action := map[string]backend.Action{
				"start":   backend.ActionStart,
				"stop":    backend.ActionStop,
				"restart": backend.ActionRestart,
			}[command]
			if _, err := daemon.Execute(ctx, backend.Request{Action: action, Program: args[0]}); err != nil {
				if command == "stop" && errors.Is(err, backend.ErrProgramAlreadyStopped) {
					return "", fmt.Errorf("%s: already stopped", args[0])
				}
				return "", err
			}
			verb := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[command]
			return fmt.Sprintf("%s: %s", args[0], verb), nil
		case "reload":
			if len(args) > 1 {
				return "", fmt.Errorf("usage: reload [config-path]")
			}
			request := backend.Request{Action: backend.ActionReload}
			if len(args) == 1 {
				request.ConfigPath = args[0]
			}
			if _, err := daemon.Execute(ctx, request); err != nil {
				return "", err
			}
			return "configuration reloaded", nil
		case "shutdown", "quit", "exit":
			if len(args) != 0 {
				return "", fmt.Errorf("usage: %s", command)
			}
			if _, err := daemon.Execute(ctx, backend.Request{Action: backend.ActionShutdown}); err != nil {
				return "", err
			}
			return "", tui.ErrExit
		default:
			return "", fmt.Errorf("unknown command: %s", command)
		}
	}
}

func formatStatus(response backend.Response) string {
	rows := make([]tui.StatusRow, 0)
	for _, program := range response.Programs {
		for _, instance := range program.Instances {
			rows = append(rows, tui.StatusRow{
				Name:   instance.Name,
				Status: displayState(instance),
				Info:   formatInstanceInfo(instance),
			})
		}
	}
	return tui.FormatStatusTable(rows)
}

func displayState(instance backend.InstanceStatus) string {
	switch instance.State {
	case "running":
		return "RUNNING"
	case "starting":
		return "STARTING"
	case "stopping":
		return "STOPPING"
	case "not_started", "exited_without_error":
		return "STOPPED"
	case "exited_with_error", "start_failed":
		if instance.LastExit.StopRequested {
			return "STOPPED"
		}
		return "FATAL"
	default:
		return strings.ToUpper(instance.State)
	}
}

func formatInstanceInfo(instance backend.InstanceStatus) string {
	parts := make([]string, 0, 5)
	if instance.PID > 0 {
		parts = append(parts, "pid="+strconv.Itoa(instance.PID))
	}
	parts = append(parts, "healthy="+strconv.FormatBool(instance.Healthy))
	parts = append(parts, "restarts="+strconv.FormatUint(uint64(instance.RestartCount), 10))
	if instance.LastExit.ExitedBySignal {
		key := "signal"
		if instance.State == "exited_with_error" && !instance.LastExit.StopRequested {
			key = "unexpected_signal"
		}
		parts = append(parts, key+"="+instance.LastExit.Signal)
	} else if instance.LastExit.ExitCode >= 0 {
		key := "exit"
		if instance.State == "exited_with_error" && !instance.LastExit.StopRequested {
			key = "unexpected_exit"
		}
		parts = append(parts, key+"="+strconv.Itoa(instance.LastExit.ExitCode))
	}
	if instance.LastError != "" {
		parts = append(parts, "error="+instance.LastError)
	}
	return strings.Join(parts, " ")
}
