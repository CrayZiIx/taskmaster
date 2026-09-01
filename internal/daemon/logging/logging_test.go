package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesRunLogWithMetadata(t *testing.T) {
	baseDirectory := t.TempDir()
	runLogger, err := Open(baseDirectory, "config/taskmaster.yaml")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if runLogger.FilePath() == "" {
		t.Fatal("FilePath() returned an empty path")
	}
	entries, err := os.ReadDir(baseDirectory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || filepath.Dir(runLogger.FilePath()) != filepath.Join(baseDirectory, entries[0].Name()) {
		t.Fatalf("run directory = %q, entries = %#v", filepath.Dir(runLogger.FilePath()), entries)
	}
	if err := runLogger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := os.Open(runLogger.FilePath())
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("log file has no metadata record: %v", scanner.Err())
	}
	var metadata map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &metadata); err != nil {
		t.Fatalf("decode metadata record: %v", err)
	}
	if metadata["event"] != "run_started" || metadata["config_path"] != "config/taskmaster.yaml" {
		t.Fatalf("metadata = %#v, want run_started and config path", metadata)
	}
}

func TestRunLoggerKeepsTheSameFileAcrossEvents(t *testing.T) {
	runLogger, err := Open(t.TempDir(), "first.yaml")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	filePath := runLogger.FilePath()
	runLogger.Logger().Info("configuration reload applied", "event", "configuration_reloaded", "config_path", "second.yaml")
	if err := runLogger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var records []map[string]any
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode log record: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log file: %v", err)
	}
	if len(records) != 2 || records[1]["event"] != "configuration_reloaded" || records[1]["config_path"] != "second.yaml" {
		t.Fatalf("records = %#v, want metadata and reload in one file", records)
	}
}

func TestOpenRejectsFileAsLogDirectory(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-directory")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	filePath := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := Open(filePath, "config.yaml"); err == nil {
		t.Fatal("Open() expected an error for a file log directory")
	}
}

func TestRunLoggerCloseIsIdempotent(t *testing.T) {
	runLogger, err := Open(t.TempDir(), "config.yaml")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := runLogger.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := runLogger.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
