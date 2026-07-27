package main

import (
	"fmt"
	"os/exec"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Println(err)
		return
	}

	for name, program := range cfg.Programs {
		tmpCmd := exec.Command(program.Command[0], program.Command[1:]...)
		tmpOut, err := tmpCmd.Output()
		if err != nil {
			fmt.Printf("exec: %s, error = %s\n", program.Command, err)
			return
		}
		fmt.Println(">", name)
		fmt.Println(string(tmpOut))
	}
}
