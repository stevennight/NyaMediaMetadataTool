package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultProcessingStrategyIsMissing(t *testing.T) {
	cfg := Default()
	if cfg.Processing.Strategy != ProcessingStrategyMissing {
		t.Fatalf("expected missing strategy, got %q", cfg.Processing.Strategy)
	}
}

func TestSaveRestrictsConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %o, want 600", got)
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

func TestLoadFiltersUnsupportedImageSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("scraping:\n  imageSources:\n    - tmdb\n    - tvdb\n    - fanart\n    - tmdb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Scraping.ImageSources, ",") != "tmdb,fanart" {
		t.Fatalf("unexpected image sources: %#v", cfg.Scraping.ImageSources)
	}
}

func TestUploadDefaultsAndYAMLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("upload:\n  enabled: true\n  quietPeriod: 45s\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Upload.Enabled {
		t.Fatal("upload should stay enabled")
	}
	if cfg.Upload.QuietPeriod.String() != "45s" {
		t.Fatalf("unexpected quiet period: %s", cfg.Upload.QuietPeriod)
	}
	if cfg.Upload.Concurrency != 1 || cfg.Upload.MaxAttempts != 3 {
		t.Fatalf("unexpected upload defaults: %#v", cfg.Upload)
	}
	if !containsString(cfg.Upload.IncludeTypes, "video") || !containsString(cfg.Upload.IncludeTypes, "nfo") {
		t.Fatalf("upload defaults should include media and metadata: %#v", cfg.Upload.IncludeTypes)
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "quietPeriod: 45s") {
		t.Fatalf("saved upload configuration is missing: %s", data)
	}
	if !strings.Contains(string(data), "includeTypes:") {
		t.Fatalf("saved upload types are missing: %s", data)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
