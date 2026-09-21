// Package logging creates the daemon log for one Taskmaster run.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultDirectory = "logs"
	logFileName      = "daemon.log"
	logDirectoryMode = 0o755
	logFileMode      = 0o644
)

// RunLogger owns the log file shared by the daemon for one process run.
// Reloads keep using the same logger and therefore the same file.
type RunLogger struct {
	logger   *slog.Logger
	filePath string
	file     *os.File

	closeOnce sync.Once
	closeErr  error
}

// Open creates a new timestamped run directory below baseDirectory and
// writes the run metadata as the first JSON log record.
func Open(baseDirectory, configPath string) (*RunLogger, error) {
	if strings.TrimSpace(baseDirectory) == "" {
		return nil, fmt.Errorf("log directory must not be empty")
	}
	if err := os.MkdirAll(baseDirectory, logDirectoryMode); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", baseDirectory, err)
	}

	startedAt := time.Now().UTC()
	runDirectoryName := fmt.Sprintf("%s-%d", startedAt.Format("20060102T150405.000000000Z"), os.Getpid())
	runDirectory := filepath.Join(baseDirectory, runDirectoryName)
	if err := os.Mkdir(runDirectory, logDirectoryMode); err != nil {
		return nil, fmt.Errorf("create log run directory %q: %w", runDirectory, err)
	}

	filePath := filepath.Join(runDirectory, logFileName)
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, logFileMode)
	if err != nil {
		_ = os.Remove(runDirectory)
		return nil, fmt.Errorf("create daemon log %q: %w", filePath, err)
	}

	runLogger := &RunLogger{
		logger:   slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug})),
		filePath: filePath,
		file:     file,
	}
	runLogger.logger.Info("taskmaster run started",
		"event", "run_started",
		"config_path", configPath,
		"pid", os.Getpid(),
		"started_at", startedAt.Format(time.RFC3339Nano),
	)
	return runLogger, nil
}

// Logger returns the slog logger for this run.
func (r *RunLogger) Logger() *slog.Logger {
	return r.logger
}

// FilePath returns the path of the daemon log file.
func (r *RunLogger) FilePath() string {
	return r.filePath
}

// Close closes the run log. It is safe to call multiple times.
func (r *RunLogger) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.closeErr = r.file.Close()
	})
	return r.closeErr
}
