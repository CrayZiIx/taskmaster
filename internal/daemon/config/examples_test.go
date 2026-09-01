package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExampleConfigurations(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "..", ".docs", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		entry := entry
		t.Run(entry.Name(), func(t *testing.T) {
			cfg, err := LoadConfig(filepath.Join(examplesDir, entry.Name()))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if len(cfg.Programs) == 0 {
				t.Fatal("configuration contains no programs")
			}
		})
	}
}
