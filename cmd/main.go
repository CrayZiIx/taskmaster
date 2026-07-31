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

	p, err := process.New("sleep", cfg.Programs["sleep"])
	if err != nil {
		fmt.Println(err)
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
	fmt.Printf("%v\n", p)
}
