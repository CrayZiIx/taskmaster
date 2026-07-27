package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type ConfigurationFile struct {
	Programs map[string]ConfigurationProgram `yaml:"taskmaster"`
}

type ConfigurationProgram struct {
	Command     []string          `yaml:"cmd"`
	Workdir     string            `yaml:"workdir"`
	Umask       string            `yaml:"umask"`
	Journey     JourneyType       `yaml:"journey"`
	ProcessNb   uint              `yaml:"process-nb"`
	Output      OutputType        `yaml:"output"`
	Environment map[string]string `yaml:"env"`
}

type JourneyType struct {
	AutoStart     bool              `yaml:"autostart"`
	HealthTime    uint              `yaml:"health-time"`
	RestartPolicy RestartPolicyType `yaml:"restart-policy"`
	Exit          ExitType          `yaml:"exit"`
}

type RestartPolicyType struct {
	RestartCase string `yaml:"restart-case"` // do enum
	RestartNb   uint   `yaml:"restart-nb"`
}

type ExitType struct {
	ExitCodes   []int    `yaml:"code"`
	ExitSignals []string `yaml:"signal"`
	Timeout     uint     `yaml:"timeout-ms"`
}

type OutputType struct {
	StdOut string `yaml:"stdout"`
	StdErr string `yaml:"stderr"`
}

func LoadConfig() (*ConfigurationFile, error) {

	filePath := "internal/daemon/config/default-config.yaml"
	if len(os.Args) > 1 && os.Args[1] != "" {
		filePath = os.Args[1]
		// we could also define the default path by finding the first .yaml inside the directory where the prog is start
	}
	// get config file as prog args
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("os.Open: %w\n", err)
	}
	defer file.Close()

	return decodeConfig(file)
}

func decodeConfig(reader io.Reader) (*ConfigurationFile, error) {
	var cfg ConfigurationFile

	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("os.Decode: %w\n", err)
	}

	return &cfg, nil
}
