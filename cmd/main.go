package main

import (
	"fmt"
	"os"

	"github.com/CrayZiIx/taskmaster/internal/deamon/config"
)

func main() {
	fmt.Println(">taskmaster:")

	filePath := ""
	if len(os.Args) > 1 && os.Args[1] != "" {
		filePath = os.Args[1]
	}

	cfg, err := config.LoadConfig(filePath)
	if err != nil {
		fmt.Println(err)
		return
	}

	for name, program := range cfg.Programs {
		fmt.Println(name)
		fmt.Println(program.Command)
	}
}
