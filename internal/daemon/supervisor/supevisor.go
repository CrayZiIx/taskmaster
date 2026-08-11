package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
)

const (
	initialBackoff = 100 * time.Millisecond
	maximumBackoff = 5 * time.Second
)

var (
	ErrProgramNotFound        = errors.New("program not found")
	ErrSupervisorRunning      = errors.New("supervisor is already running")
	ErrSupervisorShuttingDown = errors.New("supervisor is shutting down")
	ErrInstanceStarting       = errors.New("process instance is starting")
)

type Supervisor struct {
	mu           sync.RWMutex
	programs     map[string]*programRuntime
	stopCh       chan struct{}
	shutdownDone chan struct{}
	shutdownOnce sync.Once
	shutdownErr  error
	running      bool
	shuttingDown bool
	monitorWG    sync.WaitGroup
}

type programRuntime struct {
	name      string
	config    config.ConfigurationProgram
	instances []*instanceRuntime
	opMu      sync.Mutex
}

type instanceRuntime struct {
	mu              sync.Mutex
	number          uint
	programName     string
	config          config.ConfigurationProgram
	process         *process.Process
	generation      uint64
	starting        bool
	state           process.State
	healthy         bool
	restartCount    uint
	restartDisabled bool
	manualStop      bool
	lastExit        process.ExitInfo
	lastError       error
}

type Snapshot struct {
	ShuttingDown bool
	Programs     []ProgramSnapshot
}

type ProgramSnapshot struct {
	Name      string
	Instances []InstanceSnapshot
}

type InstanceSnapshot struct {
	Name            string
	InstanceNumber  uint
	PID             int
	State           process.State
	Healthy         bool
	RestartCount    uint
	RestartDisabled bool
	LastExit        process.ExitInfo
	LastError       string
}

type ProcessExit struct {
	Name string
	Code int
}

type ExitClassification string

const (
	ExpectedExit   ExitClassification = "expected"
	UnexpectedExit ExitClassification = "unexpected"
)

func ClassifyExit(info process.ExitInfo, conf config.ConfigurationProgram) ExitClassification {
	if info.WaitErr != nil {
		return UnexpectedExit
	}
	if info.ExitedBySignal {
		for _, accepted := range conf.Journey.Exit.ExitSignals {
			if strings.EqualFold(strings.TrimSpace(accepted), info.Signal) {
				return ExpectedExit
			}
		}
		return UnexpectedExit
	}
	for _, accepted := range conf.Journey.Exit.ExitCodes {
		if accepted == info.ExitCode {
			return ExpectedExit
		}
	}
	return UnexpectedExit
}

func ShouldRestart(policy string, classification ExitClassification) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case string(config.RestartAlways):
		return true
	case string(config.RestartUnexpected):
		return classification == UnexpectedExit
	default:
		return false
	}
}

func New(cfg *config.ConfigurationFile) (*Supervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration must not be nil")
	}
	copy, err := cloneConfiguration(cfg)
	if err != nil {
		return nil, err
	}

	s := &Supervisor{
		programs:     make(map[string]*programRuntime, len(copy.Programs)),
		stopCh:       make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
	for name, programConfig := range copy.Programs {
		runtime := &programRuntime{name: name, config: programConfig}
		runtime.instances = make([]*instanceRuntime, programConfig.ProcessNb)
		for index := range runtime.instances {
			runtime.instances[index] = &instanceRuntime{
				number:      uint(index + 1),
				programName: name,
				config:      programConfig,
				state:       process.NOT_STARTED,
				lastExit:    process.ExitInfo{ExitCode: -1},
			}
		}
		s.programs[name] = runtime
	}
	return s, nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return ErrSupervisorShuttingDown
	}
	if s.running {
		s.mu.Unlock()
		return ErrSupervisorRunning
	}
	s.running = true
	programs := make([]*programRuntime, 0, len(s.programs))
	for _, runtime := range s.programs {
		programs = append(programs, runtime)
	}
	s.mu.Unlock()

	sort.Slice(programs, func(i, j int) bool { return programs[i].name < programs[j].name })
	for _, runtime := range programs {
		if runtime.config.Journey.AutoStart {
			_ = s.startProgram(runtime, false)
		}
	}

	select {
	case <-ctx.Done():
		return s.Shutdown()
	case <-s.shutdownDone:
		return s.shutdownError()
	}
}

func (s *Supervisor) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.shuttingDown = true
		close(s.stopCh)
		programs := make([]*programRuntime, 0, len(s.programs))
		for _, runtime := range s.programs {
			programs = append(programs, runtime)
		}
		s.mu.Unlock()

		var stopWG sync.WaitGroup
		var stopMu sync.Mutex
		var stopErr error
		for _, runtime := range programs {
			for _, instance := range runtime.instances {
				stopWG.Add(1)
				go func(instance *instanceRuntime) {
					defer stopWG.Done()
					if err := s.stopInstance(instance); err != nil {
						stopMu.Lock()
						stopErr = errors.Join(stopErr, err)
						stopMu.Unlock()
					}
				}(instance)
			}
		}
		stopWG.Wait()
		s.monitorWG.Wait()

		s.mu.Lock()
		s.shutdownErr = stopErr
		close(s.shutdownDone)
		s.mu.Unlock()
	})
	return s.shutdownError()
}

func (s *Supervisor) StartProgram(name string) error {
	runtime, err := s.program(name)
	if err != nil {
		return err
	}
	runtime.opMu.Lock()
	defer runtime.opMu.Unlock()
	return s.startProgram(runtime, true)
}

func (s *Supervisor) StopProgram(name string) error {
	runtime, err := s.program(name)
	if err != nil {
		return err
	}
	runtime.opMu.Lock()
	defer runtime.opMu.Unlock()

	var stopErr error
	for _, instance := range runtime.instances {
		if err := s.stopInstance(instance); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	return stopErr
}

func (s *Supervisor) RestartProgram(name string) error {
	runtime, err := s.program(name)
	if err != nil {
		return err
	}
	runtime.opMu.Lock()
	defer runtime.opMu.Unlock()

	var stopErr error
	for _, instance := range runtime.instances {
		if err := s.stopInstance(instance); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	if stopErr != nil {
		return stopErr
	}
	return s.startProgram(runtime, true)
}

func (s *Supervisor) Snapshot() Snapshot {
	s.mu.RLock()
	shuttingDown := s.shuttingDown
	programs := make([]*programRuntime, 0, len(s.programs))
	for _, runtime := range s.programs {
		programs = append(programs, runtime)
	}
	s.mu.RUnlock()

	sort.Slice(programs, func(i, j int) bool { return programs[i].name < programs[j].name })
	snapshot := Snapshot{ShuttingDown: shuttingDown, Programs: make([]ProgramSnapshot, 0, len(programs))}
	for _, runtime := range programs {
		programSnapshot := ProgramSnapshot{Name: runtime.name, Instances: make([]InstanceSnapshot, 0, len(runtime.instances))}
		for _, instance := range runtime.instances {
			instance.mu.Lock()
			state := instance.state
			pid := 0
			lastExit := instance.lastExit
			if instance.process != nil {
				state = instance.process.Status()
				pid = instance.process.PID()
				lastExit = instance.process.Result()
			}
			lastError := ""
			if instance.lastError != nil {
				lastError = instance.lastError.Error()
			}
			programSnapshot.Instances = append(programSnapshot.Instances, InstanceSnapshot{
				Name:            fmt.Sprintf("%s[%d]", runtime.name, instance.number),
				InstanceNumber:  instance.number,
				PID:             pid,
				State:           state,
				Healthy:         instance.healthy,
				RestartCount:    instance.restartCount,
				RestartDisabled: instance.restartDisabled,
				LastExit:        lastExit,
				LastError:       lastError,
			})
			instance.mu.Unlock()
		}
		snapshot.Programs = append(snapshot.Programs, programSnapshot)
	}
	return snapshot
}

func (s *Supervisor) startProgram(runtime *programRuntime, manual bool) error {
	if s.isShuttingDown() {
		return ErrSupervisorShuttingDown
	}
	var startErr error
	for _, instance := range runtime.instances {
		if err := s.startInstance(instance, manual); err != nil {
			startErr = errors.Join(startErr, err)
			if !errors.Is(err, ErrInstanceStarting) {
				s.handleStartFailure(instance)
			}
		}
	}
	return startErr
}

func (s *Supervisor) startInstance(instance *instanceRuntime, manual bool) error {
	if s.isShuttingDown() {
		return ErrSupervisorShuttingDown
	}

	instance.mu.Lock()
	if instance.starting {
		instance.mu.Unlock()
		return ErrInstanceStarting
	}
	if instance.process != nil {
		state := instance.process.Status()
		if state == process.STARTING {
			instance.mu.Unlock()
			return ErrInstanceStarting
		}
		if state == process.RUNNING || state == process.STOPPING {
			instance.mu.Unlock()
			return nil
		}
	}
	instance.starting = true
	instance.state = process.STARTING
	if manual {
		instance.restartCount = 0
		instance.restartDisabled = false
		instance.healthy = false
		instance.lastError = nil
		instance.manualStop = false
	}
	instance.generation++
	generation := instance.generation
	number := instance.number
	instance.mu.Unlock()

	p, err := process.NewInstance(instance.programName, number, instance.config)
	if err != nil {
		s.recordStartFailure(instance, generation, err)
		return err
	}

	instance.mu.Lock()
	if instance.generation != generation {
		instance.mu.Unlock()
		return ErrInstanceStarting
	}
	instance.process = p
	instance.starting = false
	instance.state = process.NOT_STARTED
	instance.mu.Unlock()

	if err := p.Start(); err != nil {
		s.recordProcessCompletion(instance, p, generation)
		return err
	}
	instance.mu.Lock()
	if instance.generation == generation {
		instance.state = process.RUNNING
		instance.lastError = nil
	}
	instance.mu.Unlock()

	s.monitorWG.Add(1)
	go s.monitorInstance(instance, p, generation, instance.config)
	return nil
}

func (s *Supervisor) monitorInstance(instance *instanceRuntime, p *process.Process, generation uint64, conf config.ConfigurationProgram) {
	defer s.monitorWG.Done()

	if conf.Journey.HealthTime == 0 {
		s.markHealthy(instance, generation)
	} else {
		timer := time.NewTimer(time.Duration(conf.Journey.HealthTime) * time.Millisecond)
		select {
		case <-p.Done():
			timer.Stop()
		case <-timer.C:
			s.markHealthy(instance, generation)
		case <-s.stopCh:
			timer.Stop()
		}
	}

	_ = p.Wait()
	s.recordProcessCompletion(instance, p, generation)
}

func (s *Supervisor) markHealthy(instance *instanceRuntime, generation uint64) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.generation != generation || instance.process == nil || instance.process.Status() != process.RUNNING {
		return
	}
	instance.healthy = true
	instance.restartCount = 0
	instance.restartDisabled = false
}

func (s *Supervisor) recordStartFailure(instance *instanceRuntime, generation uint64, err error) {
	instance.mu.Lock()
	if instance.generation != generation {
		instance.mu.Unlock()
		return
	}
	instance.starting = false
	instance.process = nil
	instance.state = process.START_FAILED
	instance.healthy = false
	instance.lastExit = process.ExitInfo{ExitCode: -1, WaitErr: err}
	instance.lastError = err
	instance.mu.Unlock()
}

func (s *Supervisor) recordProcessCompletion(instance *instanceRuntime, p *process.Process, generation uint64) {
	info := p.Result()
	instance.mu.Lock()
	if instance.generation != generation || instance.process != p {
		instance.mu.Unlock()
		return
	}
	manualStop := instance.manualStop
	instance.process = nil
	instance.starting = false
	instance.state = p.Status()
	instance.healthy = false
	instance.lastExit = info
	instance.lastError = info.WaitErr
	instance.mu.Unlock()

	if manualStop || s.isShuttingDown() {
		return
	}
	s.scheduleRestart(instance, generation, info)
}

func (s *Supervisor) handleStartFailure(instance *instanceRuntime) {
	instance.mu.Lock()
	generation := instance.generation
	info := instance.lastExit
	instance.mu.Unlock()
	s.scheduleRestart(instance, generation, info)
}

func (s *Supervisor) scheduleRestart(instance *instanceRuntime, generation uint64, info process.ExitInfo) {
	if s.isShuttingDown() {
		return
	}
	conf := instance.config
	classification := ClassifyExit(info, conf)
	if info.StopRequested || !ShouldRestart(conf.Journey.RestartPolicy.RestartCase, classification) {
		return
	}

	instance.mu.Lock()
	if instance.generation != generation || instance.manualStop || instance.restartDisabled {
		instance.mu.Unlock()
		return
	}
	if instance.restartCount >= conf.Journey.RestartPolicy.RestartNb {
		instance.restartDisabled = true
		instance.mu.Unlock()
		return
	}
	instance.restartCount++
	attempt := instance.restartCount
	instance.healthy = false
	instance.mu.Unlock()

	s.monitorWG.Add(1)
	go func() {
		defer s.monitorWG.Done()
		delay := restartDelay(attempt)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.stopCh:
			return
		}

		instance.mu.Lock()
		valid := instance.generation == generation && !instance.manualStop && !instance.restartDisabled && instance.process == nil
		instance.mu.Unlock()
		if !valid || s.isShuttingDown() {
			return
		}
		if err := s.startInstance(instance, false); err != nil {
			s.handleStartFailure(instance)
		}
	}()
}

func restartDelay(attempt uint) time.Duration {
	delay := initialBackoff
	for index := uint(1); index < attempt; index++ {
		if delay >= maximumBackoff/2 {
			return maximumBackoff
		}
		delay *= 2
	}
	if delay > maximumBackoff {
		return maximumBackoff
	}
	return delay
}

func (s *Supervisor) stopInstance(instance *instanceRuntime) error {
	instance.mu.Lock()
	instance.manualStop = true
	p := instance.process
	instance.mu.Unlock()
	if p == nil {
		return nil
	}

	for p.Status() == process.STARTING {
		time.Sleep(time.Millisecond)
	}

	var err error
	switch p.Status() {
	case process.RUNNING:
		err = p.Stop()
	case process.STOPPING:
		err = p.Wait()
	case process.STARTING:
		err = ErrInstanceStarting
	default:
		err = p.Wait()
	}
	s.recordProcessCompletion(instance, p, instanceGeneration(instance, p))
	return err
}

func instanceGeneration(instance *instanceRuntime, p *process.Process) uint64 {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.process == p {
		return instance.generation
	}
	return 0
}

func (s *Supervisor) program(name string) (*programRuntime, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.shuttingDown {
		return nil, ErrSupervisorShuttingDown
	}
	runtime, ok := s.programs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProgramNotFound, name)
	}
	return runtime, nil
}

func (s *Supervisor) isShuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shuttingDown
}

func (s *Supervisor) shutdownError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutdownErr
}

func cloneConfiguration(source *config.ConfigurationFile) (*config.ConfigurationFile, error) {
	copy := &config.ConfigurationFile{Programs: make(map[string]config.ConfigurationProgram, len(source.Programs))}
	for name, sourceProgram := range source.Programs {
		program := sourceProgram
		program.Command = append([]string(nil), sourceProgram.Command...)
		program.Journey.Exit.ExitCodes = append([]int(nil), sourceProgram.Journey.Exit.ExitCodes...)
		program.Journey.Exit.ExitSignals = append([]string(nil), sourceProgram.Journey.Exit.ExitSignals...)
		if sourceProgram.Environment != nil {
			program.Environment = make(map[string]string, len(sourceProgram.Environment))
			for key, value := range sourceProgram.Environment {
				program.Environment[key] = value
			}
		}
		if err := program.Normalize(); err != nil {
			return nil, fmt.Errorf("taskmaster.%s: %w", name, err)
		}
		copy.Programs[name] = program
	}
	if err := copy.Validate(); err != nil {
		return nil, err
	}
	return copy, nil
}
