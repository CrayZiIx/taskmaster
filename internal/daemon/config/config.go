package config

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "internal/daemon/config/default-config.yaml"

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
	Stop          StopType          `yaml:"stop"`
}

type RestartCase string

const (
	RestartAlways     RestartCase = "always"
	RestartNever      RestartCase = "never"
	RestartUnexpected RestartCase = "unexpected"
)

type RestartPolicyType struct {
	RestartCase string `yaml:"restart-case"`
	RestartNb   uint   `yaml:"restart-nb"`
}

type ExitType struct {
	ExitCodes   []int    `yaml:"code"`
	ExitSignals []string `yaml:"signal"`
	Timeout     uint     `yaml:"timeout-ms"`
}

type StopType struct {
	Signal string `yaml:"signal"`
}

type OutputType struct {
	Stdout string `yaml:"stdout"`
	Stderr string `yaml:"stderr"`
}

// LoadConfig loads, applies defaults to, and validates a configuration file.
func LoadConfig(filePath string) (*ConfigurationFile, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("configuration path must not be empty")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open configuration %q: %w", filePath, err)
	}
	defer file.Close()

	cfg, err := ParseConfig(file)
	if err != nil {
		return nil, fmt.Errorf("load configuration %q: %w", filePath, err)
	}
	return cfg, nil
}

// ParseConfig parses a single YAML document, applies defaults, and validates
// the resulting configuration.
func ParseConfig(reader io.Reader) (*ConfigurationFile, error) {
	var raw rawConfigurationFile
	if err := decodeYAML(reader, &raw); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	cfg, err := raw.configuration()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// decodeConfig is kept as the syntax-only decoder used by the original unit
// tests and by callers that need the YAML shape before applying defaults.
func decodeConfig(reader io.Reader) (*ConfigurationFile, error) {
	var cfg ConfigurationFile
	if err := decodeYAML(reader, &cfg); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}
	return &cfg, nil
}

func decodeYAML(reader io.Reader, destination any) error {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	// A configuration file is one document. Silently accepting a second
	// document makes reloads and deployment mistakes unnecessarily ambiguous.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not supported")
		}
		return err
	}
	return nil
}

// Normalize applies defaults to a configuration assembled by Go code. Files
// use the raw representation below so explicit zero values can still be
// rejected instead of being mistaken for omitted fields.
func (c *ConfigurationFile) Normalize() error {
	if c == nil {
		return fmt.Errorf("configuration must not be nil")
	}
	for name, program := range c.Programs {
		program.applyDefaults()
		c.Programs[name] = program
	}
	return c.Validate()
}

func (c *ConfigurationFile) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration must not be nil")
	}
	if len(c.Programs) == 0 {
		return fmt.Errorf("taskmaster must define at least one program")
	}
	for name, program := range c.Programs {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("taskmaster contains an empty program name")
		}
		if err := program.validate("taskmaster." + name); err != nil {
			return err
		}
	}
	return nil
}

func (p *ConfigurationProgram) applyDefaults() {
	if p.ProcessNb == 0 {
		p.ProcessNb = 1
	}
	if p.Umask == "" {
		p.Umask = "022"
	}
	if p.Journey.RestartPolicy.RestartCase == "" {
		p.Journey.RestartPolicy.RestartCase = string(RestartNever)
	}
	if p.Journey.Exit.ExitCodes == nil {
		p.Journey.Exit.ExitCodes = []int{0}
	}
	if p.Journey.Exit.Timeout == 0 {
		p.Journey.Exit.Timeout = 5000
	}
	if p.Journey.Stop.Signal == "" {
		p.Journey.Stop.Signal = "SIGTERM"
	}
}

// Normalize applies defaults and validates a program assembled by Go code.
func (p *ConfigurationProgram) Normalize() error {
	if p == nil {
		return fmt.Errorf("program configuration must not be nil")
	}
	p.applyDefaults()
	p.Journey.RestartPolicy.RestartCase = strings.ToLower(strings.TrimSpace(p.Journey.RestartPolicy.RestartCase))
	for index, signal := range p.Journey.Exit.ExitSignals {
		p.Journey.Exit.ExitSignals[index] = strings.ToUpper(strings.TrimSpace(signal))
	}
	return p.validate("program")
}

func (p ConfigurationProgram) validate(path string) error {
	if len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "" {
		return fmt.Errorf("%s.cmd: must contain a usable executable", path)
	}
	if _, err := exec.LookPath(p.Command[0]); err != nil {
		return fmt.Errorf("%s.cmd: executable %q is not usable: %w", path, p.Command[0], err)
	}
	for index, argument := range p.Command {
		if strings.IndexByte(argument, 0) >= 0 {
			return fmt.Errorf("%s.cmd[%d]: must not contain a NUL byte", path, index)
		}
	}
	if p.ProcessNb == 0 {
		return fmt.Errorf("%s.process-nb: must be at least 1", path)
	}
	if _, err := ParseUmask(p.Umask); err != nil {
		return fmt.Errorf("%s.umask: %w", path, err)
	}
	if _, err := SignalNumber(p.Journey.Stop.Signal); err != nil {
		return fmt.Errorf("%s.journey.stop.signal: %w", path, err)
	}

	if p.Workdir != "" && strings.IndexByte(p.Workdir, 0) >= 0 {
		return fmt.Errorf("%s.workdir: must not contain a NUL byte", path)
	}
	if err := p.validateOutput(path + ".output"); err != nil {
		return err
	}
	if err := validateEnvironment(path+".env", p.Environment); err != nil {
		return err
	}

	policy := RestartCase(strings.ToLower(strings.TrimSpace(p.Journey.RestartPolicy.RestartCase)))
	switch policy {
	case RestartAlways, RestartNever, RestartUnexpected:
	default:
		return fmt.Errorf("%s.journey.restart-policy.restart-case: must be one of always, never, unexpected", path)
	}
	if policy == RestartNever && p.Journey.RestartPolicy.RestartNb > 0 {
		return fmt.Errorf("%s.journey.restart-policy.restart-nb: must be 0 when restart-case is never", path)
	}

	for index, code := range p.Journey.Exit.ExitCodes {
		if code < 0 || code > 255 {
			return fmt.Errorf("%s.journey.exit.code[%d]: must be between 0 and 255", path, index)
		}
	}
	for index, signal := range p.Journey.Exit.ExitSignals {
		if err := ValidateSignal(signal); err != nil {
			return fmt.Errorf("%s.journey.exit.signal[%d]: %w", path, index, err)
		}
	}
	return nil
}

func (p ConfigurationProgram) validateOutput(path string) error {
	for field, destination := range map[string]string{
		"stdout": p.Output.Stdout,
		"stderr": p.Output.Stderr,
	} {
		if strings.IndexByte(destination, 0) >= 0 {
			return fmt.Errorf("%s.%s: must not contain a NUL byte", path, field)
		}
	}
	return nil
}

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateEnvironment(path string, environment map[string]string) error {
	for key, value := range environment {
		if !environmentKeyPattern.MatchString(key) {
			return fmt.Errorf("%s.%s: invalid environment variable name", path, key)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("%s.%s: value must not contain a NUL byte", path, key)
		}
	}
	return nil
}

// ParseUmask parses the conventional three- or four-digit octal umask.
func ParseUmask(value string) (uint32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("must be an octal value between 000 and 0777")
	}
	if len(value) > 4 || (len(value) == 4 && value[0] != '0') {
		return 0, fmt.Errorf("must be an octal value between 000 and 0777")
	}
	var mask uint32
	for _, digit := range value {
		if digit < '0' || digit > '7' {
			return 0, fmt.Errorf("must be an octal value between 000 and 0777")
		}
		mask = mask*8 + uint32(digit-'0')
	}
	if mask > 0777 {
		return 0, fmt.Errorf("must be an octal value between 000 and 0777")
	}
	return mask, nil
}

var validSignals = map[string]struct{}{
	"SIGHUP": {}, "SIGINT": {}, "SIGQUIT": {}, "SIGILL": {}, "SIGTRAP": {},
	"SIGABRT": {}, "SIGBUS": {}, "SIGFPE": {}, "SIGKILL": {}, "SIGUSR1": {},
	"SIGSEGV": {}, "SIGUSR2": {}, "SIGPIPE": {}, "SIGALRM": {}, "SIGTERM": {},
	"SIGCHLD": {}, "SIGCONT": {}, "SIGSTOP": {}, "SIGTSTP": {}, "SIGTTIN": {},
	"SIGTTOU": {}, "SIGURG": {}, "SIGXCPU": {}, "SIGXFSZ": {}, "SIGVTALRM": {},
	"SIGPROF": {}, "SIGWINCH": {}, "SIGIO": {}, "SIGPWR": {}, "SIGSYS": {},
}

func ValidateSignal(value string) error {
	if _, ok := validSignals[strings.ToUpper(strings.TrimSpace(value))]; !ok {
		return fmt.Errorf("must be a valid signal name such as SIGTERM")
	}
	return nil
}

// SignalNumber converts a supported graceful-stop signal name to its Unix
// signal number.
func SignalNumber(value string) (syscall.Signal, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SIGHUP":
		return syscall.SIGHUP, nil
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGQUIT":
		return syscall.SIGQUIT, nil
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGUSR1":
		return syscall.SIGUSR1, nil
	case "SIGUSR2":
		return syscall.SIGUSR2, nil
	case "SIGCONT":
		return syscall.SIGCONT, nil
	case "SIGTSTP":
		return syscall.SIGTSTP, nil
	default:
		return 0, fmt.Errorf("must be a supported stop signal such as SIGTERM")
	}
}

// Manager provides an atomic configuration snapshot. Reload parses and
// validates the candidate before replacing the active snapshot, so a bad
// reload cannot disrupt the currently running supervisor configuration.
type Manager struct {
	mu      sync.RWMutex
	current *ConfigurationFile
}

func NewManager(current *ConfigurationFile) (*Manager, error) {
	if current == nil {
		return nil, fmt.Errorf("configuration must not be nil")
	}
	copy := cloneConfiguration(current)
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	return &Manager{current: copy}, nil
}

func (m *Manager) Current() *ConfigurationFile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfiguration(m.current)
}

func (m *Manager) Reload(reader io.Reader) (*ConfigurationFile, error) {
	next, err := ParseConfig(reader)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.current = cloneConfiguration(next)
	m.mu.Unlock()
	return cloneConfiguration(next), nil
}

func cloneConfiguration(source *ConfigurationFile) *ConfigurationFile {
	if source == nil {
		return nil
	}
	clone := &ConfigurationFile{Programs: make(map[string]ConfigurationProgram, len(source.Programs))}
	for name, program := range source.Programs {
		program.Command = append([]string(nil), program.Command...)
		program.Journey.Exit.ExitCodes = append([]int(nil), program.Journey.Exit.ExitCodes...)
		program.Journey.Exit.ExitSignals = append([]string(nil), program.Journey.Exit.ExitSignals...)
		if program.Environment != nil {
			program.Environment = make(map[string]string, len(program.Environment))
			for key, value := range source.Programs[name].Environment {
				program.Environment[key] = value
			}
		}
		clone.Programs[name] = program
	}
	return clone
}

type rawConfigurationFile struct {
	Programs map[string]rawConfigurationProgram `yaml:"taskmaster"`
}

type rawConfigurationProgram struct {
	Command     []string          `yaml:"cmd"`
	Workdir     *string           `yaml:"workdir"`
	Umask       *string           `yaml:"umask"`
	Journey     *rawJourney       `yaml:"journey"`
	ProcessNb   *uint             `yaml:"process-nb"`
	Output      *rawOutput        `yaml:"output"`
	Environment map[string]string `yaml:"env"`
}

type rawJourney struct {
	AutoStart     *bool             `yaml:"autostart"`
	HealthTime    *uint             `yaml:"health-time"`
	RestartPolicy *rawRestartPolicy `yaml:"restart-policy"`
	Exit          *rawExit          `yaml:"exit"`
	Stop          *rawStop          `yaml:"stop"`
}

type rawRestartPolicy struct {
	RestartCase *string `yaml:"restart-case"`
	RestartNb   *uint   `yaml:"restart-nb"`
}

type rawExit struct {
	ExitCodes   []int    `yaml:"code"`
	ExitSignals []string `yaml:"signal"`
	Timeout     *uint    `yaml:"timeout-ms"`
}

type rawStop struct {
	Signal *string `yaml:"signal"`
}

type rawOutput struct {
	Stdout *string `yaml:"stdout"`
	Stderr *string `yaml:"stderr"`
}

func (r rawConfigurationFile) configuration() (*ConfigurationFile, error) {
	if len(r.Programs) == 0 {
		return nil, fmt.Errorf("taskmaster must define at least one program")
	}
	cfg := &ConfigurationFile{Programs: make(map[string]ConfigurationProgram, len(r.Programs))}
	for name, raw := range r.Programs {
		program, err := raw.program("taskmaster." + name)
		if err != nil {
			return nil, err
		}
		cfg.Programs[name] = program
	}
	return cfg, nil
}

func (r rawConfigurationProgram) program(path string) (ConfigurationProgram, error) {
	program := ConfigurationProgram{Command: append([]string(nil), r.Command...), Environment: r.Environment}
	program.applyDefaults()
	if r.Workdir != nil {
		program.Workdir = *r.Workdir
	}
	if r.Umask != nil {
		program.Umask = *r.Umask
	}
	if r.ProcessNb != nil {
		if *r.ProcessNb == 0 {
			return ConfigurationProgram{}, fmt.Errorf("%s.process-nb: must be at least 1", path)
		}
		program.ProcessNb = *r.ProcessNb
	}
	if r.Output != nil {
		if r.Output.Stdout != nil {
			program.Output.Stdout = *r.Output.Stdout
		}
		if r.Output.Stderr != nil {
			program.Output.Stderr = *r.Output.Stderr
		}
	}
	if r.Journey != nil {
		if r.Journey.AutoStart != nil {
			program.Journey.AutoStart = *r.Journey.AutoStart
		}
		if r.Journey.HealthTime != nil {
			program.Journey.HealthTime = *r.Journey.HealthTime
		}
		if r.Journey.RestartPolicy != nil {
			if r.Journey.RestartPolicy.RestartCase != nil {
				program.Journey.RestartPolicy.RestartCase = *r.Journey.RestartPolicy.RestartCase
			}
			if r.Journey.RestartPolicy.RestartNb != nil {
				program.Journey.RestartPolicy.RestartNb = *r.Journey.RestartPolicy.RestartNb
			}
		}
		if r.Journey.Exit != nil {
			if r.Journey.Exit.ExitCodes != nil {
				program.Journey.Exit.ExitCodes = append([]int(nil), r.Journey.Exit.ExitCodes...)
			}
			if r.Journey.Exit.ExitSignals != nil {
				program.Journey.Exit.ExitSignals = append([]string(nil), r.Journey.Exit.ExitSignals...)
			}
			if r.Journey.Exit.Timeout != nil {
				program.Journey.Exit.Timeout = *r.Journey.Exit.Timeout
			}
		}
		if r.Journey.Stop != nil && r.Journey.Stop.Signal != nil {
			program.Journey.Stop.Signal = *r.Journey.Stop.Signal
		}
	}
	if err := program.validate(path); err != nil {
		return ConfigurationProgram{}, err
	}
	program.Journey.RestartPolicy.RestartCase = strings.ToLower(strings.TrimSpace(program.Journey.RestartPolicy.RestartCase))
	for index, signal := range program.Journey.Exit.ExitSignals {
		program.Journey.Exit.ExitSignals[index] = strings.ToUpper(strings.TrimSpace(signal))
	}
	return program, nil
}
