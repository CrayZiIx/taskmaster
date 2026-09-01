// Command example is a runnable demo of the tui package wired to a fake,
// in-memory command handler, so the shell can be exercised standalone
// before the real daemon side exists. Run with:
//
//	go run ./internal/tui/example
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/tui"
)

type fakeProcess struct {
	status string
	pid    int
}

func main() {
	started := time.Now()
	procs := map[string]*fakeProcess{
		"nginx":     {status: "RUNNING", pid: 4821},
		"vogsphere": {status: "STARTING", pid: 4830},
		"worker_2":  {status: "FATAL"},
	}

	handler := func(cmd string, args []string) (string, error) {
		switch cmd {
		case "status":
			names := make([]string, 0, len(procs))
			for name := range procs {
				names = append(names, name)
			}
			sort.Strings(names)

			rows := make([]tui.StatusRow, 0, len(names))
			for _, name := range names {
				p := procs[name]
				info := "exited too quickly (spawn error)"
				if p.status == "RUNNING" || p.status == "STARTING" {
					info = fmt.Sprintf("pid %d, uptime %s", p.pid, time.Since(started).Round(time.Second))
				}
				rows = append(rows, tui.StatusRow{Name: name, Status: p.status, Info: info})
			}
			return tui.FormatStatusTable(rows), nil

		case "start", "stop":
			if len(args) == 0 {
				return "", fmt.Errorf("usage: %s <name>", cmd)
			}
			p, ok := procs[args[0]]
			if !ok {
				return "", fmt.Errorf("no such process: %s", args[0])
			}
			if cmd == "start" {
				p.status = "RUNNING"
				return args[0] + ": started", nil
			}
			p.status = "STOPPED"
			return args[0] + ": stopped", nil

		case "reload":
			return "configuration reloaded", nil

		case "quit", "exit":
			return "", tui.ErrExit

		default:
			return "", fmt.Errorf("unknown command: %s", cmd)
		}
	}

	sh := tui.New(handler)
	defer sh.Close()
	if err := sh.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}
