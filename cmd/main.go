package main

import (
	"fmt"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/monitoring"
)

func main() {

	cfg, err := config.LoadConfigFile()
	if err != nil {
		fmt.Println(err)
		return
	}

	var processes []monitoring.Process

	for name, program := range cfg.Programs {
		fmt.Printf("loading config for process : %v\n", name)
		p, err := monitoring.NewProcess(name)
		p.Config = program
		p.LoadConfig()
		p.LoadEnv()
		// to do: looks how to define those
		// tmpCmd.Stdout = program.Output.StdOut
		// tmpCmd.stderr = progam.Output.StdErr
		if program.Journey.AutoStart {
			err = p.ExecCommand()
			if err != nil {
				panic(err)
			}
			fmt.Printf("prog exited success: %v & exit code %d\n", p.Command.ProcessState.Success(), p.GetExitCode())
		}
		fmt.Printf("config loaded : %v\n", p)
		processes = append(processes, *p)
	}
}
