package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
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
	ErrProgramAlreadyStopped  = errors.New("program is already stopped")
)

type Supervisor struct {
	mu           sync.RWMutex
	controlMu    sync.Mutex
	programs     map[string]*programRuntime
	stopCh       chan struct{}
	shutdownDone chan struct{}
	shutdownOnce sync.Once
	shutdownErr  error
	running      bool
	shuttingDown bool
	monitorWG    sync.WaitGroup
	logger       *slog.Logger
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
	return NewWithLogger(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// NewWithLogger creates a supervisor using logger for lifecycle and error
// events. A nil logger uses slog.Default().
func NewWithLogger(cfg *config.ConfigurationFile, logger *slog.Logger) (*Supervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	copy, err := cloneConfiguration(cfg)
	if err != nil {
		return nil, err
	}

	s := &Supervisor{
		programs:     make(map[string]*programRuntime, len(copy.Programs)),
		stopCh:       make(chan struct{}),
		shutdownDone: make(chan struct{}),
		logger:       logger.With("component", "supervisor"),
	}
	for name, programConfig := range copy.Programs {
		s.programs[name] = newProgramRuntime(name, programConfig)
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
	s.logger.Info("supervisor started", "program_count", len(programs))
	s.controlMu.Lock()
	for _, runtime := range programs {
		if runtime.config.Journey.AutoStart {
			if err := s.startProgram(runtime, false); err != nil {
				s.logger.Error("program autostart failed", "program", runtime.name, "error", err)
			}
		}
	}
	s.controlMu.Unlock()

	select {
	case <-ctx.Done():
		return s.Shutdown()
	case <-s.shutdownDone:
		return s.shutdownError()
	}
}

func (s *Supervisor) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.logger.Info("supervisor shutdown requested")
		s.controlMu.Lock()
		defer s.controlMu.Unlock()

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
		if stopErr != nil {
			s.logger.Error("supervisor shutdown failed", "error", stopErr)
		} else {
			s.logger.Info("supervisor shutdown complete")
		}
	})
	return s.shutdownError()
}

func (s *Supervisor) StartProgram(name string) error {
	s.logger.Info("program start requested", "program", name)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	runtime, err := s.program(name)
	if err != nil {
		s.logger.Error("program start failed", "program", name, "error", err)
		return err
	}
	runtime.opMu.Lock()
	defer runtime.opMu.Unlock()
	err = s.startProgram(runtime, true)
	if err != nil {
		s.logger.Error("program start failed", "program", name, "error", err)
	}
	return err
}

func (s *Supervisor) StopProgram(name string) error {
	s.logger.Info("program stop requested", "program", name)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	runtime, err := s.program(name)
	if err != nil {
		s.logger.Error("program stop failed", "program", name, "error", err)
		return err
	}
	runtime.opMu.Lock()
	defer runtime.opMu.Unlock()

	var stopErr error
	active := false
	for _, instance := range runtime.instances {
		if instanceIsActive(instance) {
			active = true
		}
		if err := s.stopInstance(instance); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	if stopErr == nil && !active {
		s.logger.Warn("program stop ignored; already stopped", "program", name)
		return fmt.Errorf("%w: %s", ErrProgramAlreadyStopped, name)
	}
	if stopErr != nil {
		s.logger.Error("program stop failed", "program", name, "error", stopErr)
	}
	return stopErr
}

func (s *Supervisor) RestartProgram(name string) error {
	s.logger.Info("program restart requested", "program", name)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	runtime, err := s.program(name)
	if err != nil {
		s.logger.Error("program restart failed", "program", name, "error", err)
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
		s.logger.Error("program restart stop phase failed", "program", name, "error", stopErr)
		return stopErr
	}
	err = s.startProgram(runtime, true)
	if err != nil {
		s.logger.Error("program restart failed", "program", name, "error", err)
	}
	return err
}

// Reload atomically reconciles the supervisor with a validated configuration.
// Running processes are preserved when their process-launch configuration is
// compatible. Changed launch settings restart active instances, removed
// programs are stopped, and new instances are created for additions.
func (s *Supervisor) Reload(cfg *config.ConfigurationFile) error {
	copy, err := cloneConfiguration(cfg)
	if err != nil {
		s.logger.Error("configuration reload rejected", "error", err)
		return err
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.isShuttingDown() {
		s.logger.Warn("configuration reload ignored; supervisor is shutting down")
		return ErrSupervisorShuttingDown
	}

	s.mu.RLock()
	existing := make(map[string]*programRuntime, len(s.programs))
	for name, runtime := range s.programs {
		existing[name] = runtime
	}
	s.mu.RUnlock()

	names := make([]string, 0, len(existing))
	for name := range existing {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		existing[name].opMu.Lock()
	}
	defer func() {
		for index := len(names) - 1; index >= 0; index-- {
			existing[names[index]].opMu.Unlock()
		}
	}()

	var reconcileErr error
	for _, name := range names {
		runtime := existing[name]
		next, stillConfigured := copy.Programs[name]
		if !stillConfigured {
			s.logger.Info("program removed by configuration reload", "program", name)
			reconcileErr = errors.Join(reconcileErr, s.stopProgramRuntime(runtime))
			continue
		}
		reconcileErr = errors.Join(reconcileErr, s.reconcileProgram(runtime, next))
	}

	added := make([]*programRuntime, 0)
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return errors.Join(reconcileErr, ErrSupervisorShuttingDown)
	}
	for name := range existing {
		if _, ok := copy.Programs[name]; !ok {
			delete(s.programs, name)
		}
	}
	for name, programConfig := range copy.Programs {
		if _, ok := existing[name]; ok {
			continue
		}
		runtime := newProgramRuntime(name, programConfig)
		s.programs[name] = runtime
		added = append(added, runtime)
		s.logger.Info("program added by configuration reload", "program", name)
	}
	s.mu.Unlock()

	sort.Slice(added, func(i, j int) bool { return added[i].name < added[j].name })
	for _, runtime := range added {
		if runtime.config.Journey.AutoStart {
			reconcileErr = errors.Join(reconcileErr, s.startProgram(runtime, false))
		}
	}
	if reconcileErr != nil {
		s.logger.Error("configuration reload completed with errors", "error", reconcileErr)
	} else {
		s.logger.Info("configuration reload applied", "program_count", len(copy.Programs))
	}
	return reconcileErr
}

func (s *Supervisor) Snapshot() Snapshot {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

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

func newProgramRuntime(name string, programConfig config.ConfigurationProgram) *programRuntime {
	runtime := &programRuntime{name: name, config: programConfig}
	runtime.instances = make([]*instanceRuntime, programConfig.ProcessNb)
	for index := range runtime.instances {
		runtime.instances[index] = newInstanceRuntime(name, uint(index+1), programConfig)
	}
	return runtime
}

func newInstanceRuntime(programName string, number uint, programConfig config.ConfigurationProgram) *instanceRuntime {
	return &instanceRuntime{
		number:      number,
		programName: programName,
		config:      programConfig,
		state:       process.NOT_STARTED,
		lastExit:    process.ExitInfo{ExitCode: -1},
	}
}

func (s *Supervisor) startProgram(runtime *programRuntime, manual bool) error {
	if s.isShuttingDown() {
		return ErrSupervisorShuttingDown
	}
	return s.startInstances(runtime, runtime.instances, manual)
}

func (s *Supervisor) startInstances(runtime *programRuntime, instances []*instanceRuntime, manual bool) error {
	var startErr error
	for _, instance := range instances {
		if err := s.startInstance(instance, manual); err != nil {
			startErr = errors.Join(startErr, err)
			if !errors.Is(err, ErrInstanceStarting) {
				s.handleStartFailure(instance)
			}
		}
	}
	return startErr
}

func (s *Supervisor) stopProgramRuntime(runtime *programRuntime) error {
	var stopErr error
	for _, instance := range runtime.instances {
		stopErr = errors.Join(stopErr, s.stopInstance(instance))
	}
	return stopErr
}

func (s *Supervisor) reconcileProgram(runtime *programRuntime, next config.ConfigurationProgram) error {
	previous := runtime.config
	wasActive := runtimeHasActiveInstance(runtime)
	launchChanged := !sameLaunchConfiguration(previous, next)
	toStart := make([]*instanceRuntime, 0)
	var reconcileErr error

	if launchChanged {
		s.logger.Info("program launch configuration changed; restarting instances", "program", runtime.name)
		for _, instance := range runtime.instances {
			if err := s.stopInstance(instance); err != nil {
				reconcileErr = errors.Join(reconcileErr, err)
			}
			toStart = append(toStart, instance)
		}
	}

	if next.ProcessNb < uint(len(runtime.instances)) {
		for index := len(runtime.instances) - 1; index >= int(next.ProcessNb); index-- {
			if err := s.stopInstance(runtime.instances[index]); err != nil {
				reconcileErr = errors.Join(reconcileErr, err)
			}
		}
		runtime.instances = runtime.instances[:next.ProcessNb]
	}
	if next.ProcessNb > uint(len(runtime.instances)) {
		for number := uint(len(runtime.instances) + 1); number <= next.ProcessNb; number++ {
			instance := newInstanceRuntime(runtime.name, number, next)
			runtime.instances = append(runtime.instances, instance)
			toStart = append(toStart, instance)
		}
	}

	if !previous.Journey.AutoStart && next.Journey.AutoStart && !launchChanged {
		for _, instance := range runtime.instances {
			toStart = appendUniqueInstance(toStart, instance)
		}
	}
	for _, instance := range runtime.instances {
		instance.mu.Lock()
		instance.config = next
		instance.mu.Unlock()
	}
	runtime.config = next

	if !wasActive && !next.Journey.AutoStart {
		return reconcileErr
	}
	return errors.Join(reconcileErr, s.startInstances(runtime, toStart, true))
}

func appendUniqueInstance(instances []*instanceRuntime, candidate *instanceRuntime) []*instanceRuntime {
	for _, instance := range instances {
		if instance == candidate {
			return instances
		}
	}
	return append(instances, candidate)
}

func runtimeHasActiveInstance(runtime *programRuntime) bool {
	for _, instance := range runtime.instances {
		instance.mu.Lock()
		active := instance.process != nil && (instance.process.Status() == process.STARTING || instance.process.Status() == process.RUNNING || instance.process.Status() == process.STOPPING)
		instance.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

func sameLaunchConfiguration(left, right config.ConfigurationProgram) bool {
	return reflect.DeepEqual(left.Command, right.Command) &&
		left.Workdir == right.Workdir &&
		left.Umask == right.Umask &&
		reflect.DeepEqual(left.Output, right.Output) &&
		reflect.DeepEqual(left.Environment, right.Environment)
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
	instanceConfig := instance.config
	instance.mu.Unlock()

	p, err := process.NewInstance(instance.programName, number, instanceConfig)
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
	s.logger.Info("process started", "program", instance.programName, "instance", p.Name, "pid", p.PID(), "generation", generation, "manual", manual)

	s.monitorWG.Add(1)
	go s.monitorInstance(instance, p, generation, instanceConfig)
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
	programName := instance.programName
	instanceName := fmt.Sprintf("%s[%d]", programName, instance.number)
	instance.mu.Unlock()
	s.logger.Error("process start failed", "program", programName, "instance", instanceName, "generation", generation, "error", err)
}

func (s *Supervisor) recordProcessCompletion(instance *instanceRuntime, p *process.Process, generation uint64) {
	info := p.Result()
	instance.mu.Lock()
	if instance.generation != generation || instance.process != p {
		instance.mu.Unlock()
		return
	}
	manualStop := instance.manualStop
	conf := instance.config
	state := p.Status()
	programName := instance.programName
	instanceName := p.Name
	instance.process = nil
	instance.starting = false
	instance.state = state
	instance.healthy = false
	instance.lastExit = info
	instance.lastError = info.WaitErr
	instance.mu.Unlock()

	attrs := []any{"program", programName, "instance", instanceName, "generation", generation, "state", state, "exit_code", info.ExitCode}
	if info.Signal != "" {
		attrs = append(attrs, "signal", info.Signal)
	}
	if info.TimedOut {
		attrs = append(attrs, "timed_out", true)
	}
	if state == process.START_FAILED {
		s.logger.Error("process start failed", append(attrs, "error", info.WaitErr)...)
	} else if info.WaitErr != nil {
		s.logger.Error("process wait failed", append(attrs, "error", info.WaitErr)...)
	} else if manualStop || info.StopRequested {
		s.logger.Info("process stopped", attrs...)
	} else if ClassifyExit(info, conf) == ExpectedExit {
		s.logger.Info("process exited", attrs...)
	} else {
		s.logger.Warn("process exited unexpectedly", attrs...)
	}

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
	instance.mu.Lock()
	conf := instance.config
	instance.mu.Unlock()
	classification := ClassifyExit(info, conf)
	if info.StopRequested {
		s.logger.Debug("process restart skipped; stop was requested", "program", instance.programName, "instance", instanceName(instance), "reason", "manual_stop")
		return
	}
	if !ShouldRestart(conf.Journey.RestartPolicy.RestartCase, classification) {
		s.logger.Debug("process restart skipped by policy", "program", instance.programName, "instance", instanceName(instance), "classification", classification, "policy", conf.Journey.RestartPolicy.RestartCase)
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
		s.logger.Warn("process restart disabled; restart limit reached", "program", instance.programName, "instance", instanceName(instance), "restart_count", conf.Journey.RestartPolicy.RestartNb)
		return
	}
	instance.restartCount++
	attempt := instance.restartCount
	instance.healthy = false
	instance.mu.Unlock()

	delay := restartDelay(attempt)
	s.logger.Warn("process restart scheduled", "program", instance.programName, "instance", instanceName(instance), "classification", classification, "attempt", attempt, "delay", delay)
	s.monitorWG.Add(1)
	go func() {
		defer s.monitorWG.Done()
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

func instanceIsActive(instance *instanceRuntime) bool {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.starting {
		return true
	}
	if instance.process == nil {
		return false
	}
	state := instance.process.Status()
	return state == process.STARTING || state == process.RUNNING || state == process.STOPPING
}

func instanceGeneration(instance *instanceRuntime, p *process.Process) uint64 {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	if instance.process == p {
		return instance.generation
	}
	return 0
}

func instanceName(instance *instanceRuntime) string {
	return fmt.Sprintf("%s[%d]", instance.programName, instance.number)
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
	if source == nil {
		return nil, fmt.Errorf("configuration must not be nil")
	}
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
