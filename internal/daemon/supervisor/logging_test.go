package supervisor

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/CrayZiIx/taskmaster/internal/daemon/config"
	"github.com/CrayZiIx/taskmaster/internal/daemon/process"
)

type capturedLog struct {
	level   slog.Level
	message string
	attrs   map[string]slog.Value
}

type captureStore struct {
	mu      sync.Mutex
	records []capturedLog
}

type captureHandler struct {
	store *captureStore
	attrs []slog.Attr
}

func newCaptureLogger() (*slog.Logger, *captureStore) {
	store := &captureStore{}
	return slog.New(&captureHandler{store: store}), store
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := append([]slog.Attr(nil), h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	values := make(map[string]slog.Value, len(attrs))
	for _, attr := range attrs {
		if attr.Key != "" {
			values[attr.Key] = attr.Value
		}
	}
	h.store.mu.Lock()
	h.store.records = append(h.store.records, capturedLog{level: record.Level, message: record.Message, attrs: values})
	h.store.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append([]slog.Attr(nil), h.attrs...)
	combined = append(combined, attrs...)
	return &captureHandler{store: h.store, attrs: combined}
}

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func (s *captureStore) has(message string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.message == message {
			return true
		}
	}
	return false
}

func (s *captureStore) find(message string) (capturedLog, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.message == message {
			return record, true
		}
	}
	return capturedLog{}, false
}

func (s *captureStore) all() []capturedLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedLog(nil), s.records...)
}

func waitForLog(t *testing.T, store *captureStore, message string) capturedLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record, ok := store.find(message); ok {
			return record
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("log message %q was not captured", message)
	return capturedLog{}
}

func TestSupervisorLogsLifecycleEvents(t *testing.T) {
	logger, logs := newCaptureLogger()
	s, err := NewWithLogger(testConfiguration(testProgram([]string{"/bin/sleep", "30"}, 1, false, config.RestartNever, 0, 0)), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })

	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		return snapshot.Programs[0].Instances[0].State == process.RUNNING
	})
	if err := s.StopProgram("worker"); err != nil {
		t.Fatalf("StopProgram() error = %v", err)
	}
	if !logs.has("program start requested") || !logs.has("process started") || !logs.has("program stop requested") || !logs.has("process stopped") {
		t.Fatalf("lifecycle log messages missing: %+v", logs.all())
	}
	started, ok := logs.find("process started")
	if !ok || started.attrs["program"].String() != "worker" || started.attrs["instance"].String() != "worker[1]" || started.attrs["pid"].Int64() <= 0 {
		t.Fatalf("started log = %+v, want program, instance and PID", started)
	}

	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !logs.has("supervisor shutdown requested") || !logs.has("supervisor shutdown complete") {
		t.Fatalf("shutdown log messages missing: %+v", logs.all())
	}
}

func TestSupervisorLogsUnexpectedExit(t *testing.T) {
	logger, logs := newCaptureLogger()
	s, err := NewWithLogger(testConfiguration(testProgram([]string{"/bin/sh", "-c", "kill -TERM $$"}, 1, false, config.RestartNever, 0, 0)), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		state := snapshot.Programs[0].Instances[0].State
		return state == process.EXITED_WERROR
	})
	record := waitForLog(t, logs, "process exited unexpectedly")
	if record.level != slog.LevelWarn || record.attrs["signal"].String() != "SIGTERM" {
		t.Fatalf("unexpected exit log = %+v, want WARN with SIGTERM", record)
	}
}

func TestSupervisorLogsStartFailure(t *testing.T) {
	logger, logs := newCaptureLogger()
	program := testProgram([]string{"/bin/echo", "never"}, 1, false, config.RestartNever, 0, 0)
	program.Workdir = "/path/that/does/not/exist"
	s, err := NewWithLogger(testConfiguration(program), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	if err := s.StartProgram("worker"); err == nil {
		t.Fatal("StartProgram() expected an error")
	}
	record, ok := logs.find("process start failed")
	if !ok || record.level != slog.LevelError || record.attrs["program"].String() != "worker" {
		t.Fatalf("start failure log = %+v, want ERROR with worker", record)
	}
}

func TestSupervisorLogsReloadChanges(t *testing.T) {
	logger, logs := newCaptureLogger()
	s, err := NewWithLogger(testConfiguration(testProgram([]string{"/bin/echo", "old"}, 1, false, config.RestartNever, 0, 0)), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	next := testConfiguration(testProgram([]string{"/bin/echo", "new"}, 1, false, config.RestartNever, 0, 0))
	next.Programs["new"] = next.Programs["worker"]
	delete(next.Programs, "worker")
	if err := s.Reload(next); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !logs.has("program removed by configuration reload") || !logs.has("program added by configuration reload") || !logs.has("configuration reload applied") {
		t.Fatalf("reload log messages missing: %+v", logs.all())
	}
}

func TestSupervisorLogsRestartScheduleAndLimit(t *testing.T) {
	logger, logs := newCaptureLogger()
	program := testProgram([]string{"/bin/sh", "-c", "exit 7"}, 1, false, config.RestartAlways, 1, 1000)
	s, err := NewWithLogger(testConfiguration(program), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })
	if err := s.StartProgram("worker"); err != nil {
		t.Fatalf("StartProgram() error = %v", err)
	}
	waitForSnapshot(t, s, func(snapshot Snapshot) bool {
		instance := snapshot.Programs[0].Instances[0]
		return instance.RestartDisabled
	})
	scheduled := waitForLog(t, logs, "process restart scheduled")
	if scheduled.level != slog.LevelWarn || scheduled.attrs["attempt"].Uint64() != 1 {
		t.Fatalf("restart scheduled log = %+v, want WARN attempt=1", scheduled)
	}
	_ = waitForLog(t, logs, "process restart disabled; restart limit reached")
}

func TestSupervisorLogsUnknownProgramErrors(t *testing.T) {
	logger, logs := newCaptureLogger()
	s, err := NewWithLogger(testConfiguration(testProgram([]string{"/bin/sleep", "30"}, 1, false, config.RestartNever, 0, 0)), logger)
	if err != nil {
		t.Fatalf("NewWithLogger() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown() })

	if err := s.StartProgram("missing"); err == nil {
		t.Fatal("StartProgram() expected an error")
	}
	if err := s.StopProgram("missing"); err == nil {
		t.Fatal("StopProgram() expected an error")
	}
	if err := s.RestartProgram("missing"); err == nil {
		t.Fatal("RestartProgram() expected an error")
	}
	for _, message := range []string{"program start failed", "program stop failed", "program restart failed"} {
		record := waitForLog(t, logs, message)
		if record.level != slog.LevelError || record.attrs["program"].String() != "missing" {
			t.Fatalf("%s log = %+v, want ERROR with missing program", message, record)
		}
	}
}

func testConfiguration(program config.ConfigurationProgram) *config.ConfigurationFile {
	return &config.ConfigurationFile{Programs: map[string]config.ConfigurationProgram{"worker": program}}
}

func TestCaptureLoggerIsSafeForConcurrentRecords(t *testing.T) {
	logger, logs := newCaptureLogger()
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				logger.Info("concurrent record", "value", time.Now().UnixNano())
			}
		}()
	}
	group.Wait()
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.records) != 160 {
		t.Fatalf("captured records = %d, want 160", len(logs.records))
	}
}
