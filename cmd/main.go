package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
)

func main() {

	configPath := config.DefaultConfigPath
	if len(os.Args) > 1 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	programNames := make([]string, 0, len(cfg.Programs))
	for name := range cfg.Programs {
		programNames = append(programNames, name)
	}
	sort.Strings(programNames)
	programName := programNames[0]

	p, err := process.New(programName, cfg.Programs[programName])
	if err != nil {
		fmt.Println(err)
		return
	}
	err = p.Start()
	if err != nil {
		fmt.Println("process start:", err)
	}
	// if err = p.Stop(); err != nil {
	// 	fmt.Printf("process %v: %v\n", p.Name, err)
	// }
	if err = p.Wait(); err != nil {
		fmt.Printf("process %v: %v\n", p.Name, err)
	}
	// fmt.Printf("%v\n", p)
}
