package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
)

func TestSupervisorCreatesInstancesLazily(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sleep", "30"}, 2, false, config.RestartNever, 0, 0))

	snapshot := s.Snapshot()
	if len(snapshot.Programs) != 1 || len(snapshot.Programs[0].Instances) != 2 {
		t.Fatalf("initial Snapshot() = %+v, want one program with two instances", snapshot)
	}
	for _, instance := range snapshot.Programs[0].Instances {
		if instance.State != process.NOT_STARTED || instance.PID != 0 {
			t.Fatalf("lazy instance = %+v, want not_started without PID", instance)
		}
	}

	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return allInstances(snapshot, func(instance InstanceSnapshot) bool {
			return instance.State == process.RUNNING && instance.PID > 0
		})
	})

	if err := s.StopProgram("worker"); err != nil {
		t.Fatalf("StopProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return allInstances(snapshot, func(instance InstanceSnapshot) bool {
			return instance.State == process.EXITED || instance.State == process.EXITED_WERROR
		})
	})
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestSupervisorAutostartsAndShutsDownWithContext(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sleep", "30"}, 1, true, config.RestartAlways, 3, 0))
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- s.Run(ctx) }()

	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return len(snapshot.Programs) == 1 && snapshot.Programs[0].Instances[0].State == process.RUNNING
	})
	cancel()
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	snapshot := s.Snapshot()
	if !snapshot.ShuttingDown {
		t.Fatal("Snapshot().ShuttingDown = false, want true")
	}
	if state := snapshot.Programs[0].Instances[0].State; state != process.EXITED && state != process.EXITED_WERROR {
		t.Fatalf("instance state after shutdown = %q, want terminal state", state)
	}
}

func TestSupervisorRestartProgramReplacesInstances(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sleep", "30"}, 1, false, config.RestartNever, 0, 0))
	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return snapshot.Programs[0].Instances[0].State == process.RUNNING
	})
	firstPID := s.Snapshot().Programs[0].Instances[0].PID

	if err := s.RestartProgram("worker"); err != nil {
		t.Fatalf("RestartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		instance := snapshot.Programs[0].Instances[0]
		return instance.State == process.RUNNING && instance.PID != 0 && instance.PID != firstPID
	})
	if err := s.StopProgram("worker"); err != nil {
		t.Fatalf("StopProgram() error = %v", err)
	}
}

func TestSupervisorPoliciesAndRestartBudget(t *testing.T) {
	tests := []struct {
		name         string
		command      []string
		policy       config.RestartCase
		restartNb    uint
		wantRestarts uint
		wantDisabled bool
	}{
		{
			name:         "never does not restart",
			command:      []string{"/bin/sh", "-c", "exit 7"},
			policy:       config.RestartNever,
			restartNb:    0,
			wantRestarts: 0,
		},
		{
			name:         "unexpected ignores expected exit code",
			command:      []string{"/bin/sh", "-c", "exit 0"},
			policy:       config.RestartUnexpected,
			restartNb:    3,
			wantRestarts: 0,
		},
		{
			name:         "always consumes budget",
			command:      []string{"/bin/sh", "-c", "exit 7"},
			policy:       config.RestartAlways,
			restartNb:    2,
			wantRestarts: 2,
			wantDisabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestSupervisor(t, testProgram(tt.command, 1, true, tt.policy, tt.restartNb, 1000))
			runDone := runTestSupervisor(t, s)
			waitForSnapshot(t, s, func(snapshot Snapshot) bool {
				instance := snapshot.Programs[0].Instances[0]
				terminal := instance.State == process.EXITED || instance.State == process.EXITED_WERROR || instance.State == process.START_FAILED
				return terminal && instance.RestartCount == tt.wantRestarts && (!tt.wantDisabled || instance.RestartDisabled)
			})
			instance := s.Snapshot().Programs[0].Instances[0]
			if instance.RestartCount != tt.wantRestarts || instance.RestartDisabled != tt.wantDisabled {
				t.Fatalf("instance = %+v, want restart count %d disabled=%v", instance, tt.wantRestarts, tt.wantDisabled)
			}
			if err := s.Shutdown(); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}
			if err := <-runDone; err != nil {
				t.Fatalf("Run() error = %v", err)
			}
		})
	}
}

func TestSupervisorTracksRestartBudgetPerInstance(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sh", "-c", "exit 7"}, 2, true, config.RestartAlways, 1, 1000))
	runDone := runTestSupervisor(t, s)
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return allInstances(snapshot, func(instance InstanceSnapshot) bool {
			return instance.RestartCount == 1 && instance.RestartDisabled
		})
	})
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorMarksHealthyAfterHealthTime(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sleep", "2"}, 1, true, config.RestartAlways, 2, 25))
	runDone := runTestSupervisor(t, s)
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		instance := snapshot.Programs[0].Instances[0]
		return instance.State == process.RUNNING && instance.Healthy
	})
	instance := s.Snapshot().Programs[0].Instances[0]
	if instance.RestartCount != 0 {
		t.Fatalf("RestartCount = %d, want 0 after health", instance.RestartCount)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorRetriesStartFailures(t *testing.T) {
	program := testProgram([]string{"/bin/echo", "never"}, 1, true, config.RestartAlways, 1, 1000)
	program.Workdir = "/path/that/does/not/exist"
	s := newTestSupervisor(t, program)
	runDone := runTestSupervisor(t, s)
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		instance := snapshot.Programs[0].Instances[0]
		return instance.State == process.START_FAILED && instance.RestartDisabled
	})
	instance := s.Snapshot().Programs[0].Instances[0]
	if instance.LastError == "" || instance.RestartCount != 1 {
		t.Fatalf("instance = %+v, want start error and one retry", instance)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestSupervisorClassifiesExitSignals(t *testing.T) {
	conf := testProgram([]string{"/bin/echo", "ok"}, 1, false, config.RestartUnexpected, 1, 0)
	conf.Journey.Exit.ExitCodes = []int{0}
	conf.Journey.Exit.ExitSignals = []string{"SIGTERM"}

	if got := ClassifyExit(process.ExitInfo{ExitCode: 0}, conf); got != ExpectedExit {
		t.Fatalf("code classification = %q, want expected", got)
	}
	if got := ClassifyExit(process.ExitInfo{ExitedBySignal: true, Signal: "SIGTERM"}, conf); got != ExpectedExit {
		t.Fatalf("accepted signal classification = %q, want expected", got)
	}
	if got := ClassifyExit(process.ExitInfo{ExitedBySignal: true, Signal: "SIGKILL"}, conf); got != UnexpectedExit {
		t.Fatalf("unaccepted signal classification = %q, want unexpected", got)
	}
	if got := ClassifyExit(process.ExitInfo{WaitErr: errors.New("start failed")}, conf); got != UnexpectedExit {
		t.Fatalf("start failure classification = %q, want unexpected", got)
	}
	if !ShouldRestart(string(config.RestartAlways), ExpectedExit) {
		t.Fatal("always policy should restart expected exits")
	}
	if ShouldRestart(string(config.RestartUnexpected), ExpectedExit) {
		t.Fatal("unexpected policy should not restart expected exits")
	}
}

func TestSupervisorSnapshotsAreSafeToReadConcurrently(t *testing.T) {
	s := newTestSupervisor(t, testProgram([]string{"/bin/sleep", "1"}, 2, false, config.RestartNever, 0, 0))
	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}

	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 100 {
				_ = s.Snapshot()
			}
		}()
	}
	group.Wait()
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func testProgram(command []string, processNb uint, autostart bool, policy config.RestartCase, restartNb uint, healthTime uint) config.ConfigurationProgram {
	return config.ConfigurationProgram{
		Command:   command,
		ProcessNb: processNb,
		Output:    config.OutputType{Stdout: "/dev/null", Stderr: "/dev/null"},
		Journey: config.JourneyType{
			AutoStart:  autostart,
			HealthTime: healthTime,
			RestartPolicy: config.RestartPolicyType{
				RestartCase: string(policy),
				RestartNb:   restartNb,
			},
			Exit: config.ExitType{ExitCodes: []int{0}, Timeout: 100},
			Stop: config.StopType{Signal: "SIGTERM"},
		},
	}
}

func newTestSupervisor(t *testing.T, program config.ConfigurationProgram) *Supervisor {
	t.Helper()
	s, err := New(&config.ConfigurationFile{Programs: map[string]config.ConfigurationProgram{"worker": program}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	return s
}

func runTestSupervisor(t *testing.T, s *Supervisor) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	return done
}

func waitForSnapshot(t *testing.T, s *Supervisor, condition func(Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition(s.Snapshot()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not reached; final snapshot = %+v", s.Snapshot())
}

func allInstances(snapshot Snapshot, condition func(InstanceSnapshot) bool) bool {
	if len(snapshot.Programs) == 0 {
		return false
	}
	for _, instance := range snapshot.Programs[0].Instances {
		if !condition(instance) {
			return false
		}
	}
	return true
}
