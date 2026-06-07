package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultProcessingStrategyIsMissing(t *testing.T) {
	cfg := Default()
	if cfg.Processing.Strategy != ProcessingStrategyMissing {
		t.Fatalf("expected missing strategy, got %q", cfg.Processing.Strategy)
	}
}

func TestLoadMigratesLegacyOverwriteExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("processing:\n  overwriteExisting: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Processing.Strategy != ProcessingStrategyForce {
		t.Fatalf("expected force strategy, got %q", cfg.Processing.Strategy)
	}
	if cfg.Processing.OverwriteExisting {
		t.Fatal("legacy overwrite flag should be cleared after migration")
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "overwriteExisting") {
		t.Fatalf("saved config still contains legacy overwriteExisting field:\n%s", data)
	}
	if !strings.Contains(string(data), "strategy: force") {
		t.Fatalf("saved config does not contain force strategy:\n%s", data)
	}
}
