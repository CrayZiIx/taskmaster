package main

import (
	"fmt"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
)

func main() {

	cfg, err := config.LoadConfigFile()
	if err != nil {
		fmt.Println(err)
		return
	}

	p := process.New("ls", cfg.Programs["ls"])
	p.Start()
	p.Wait()
	fmt.Println(p.ExitCode)
}
