package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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

func TestLegacyGlobalUploadConfigurationIsNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("upload:\n  enabled: true\n  concurrency: 4\n  quietPeriod: 45s\n  maxAttempts: 7\n  includeTypes: [video, nfo]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyUpload == nil || !cfg.LegacyUpload.Enabled || cfg.LegacyUpload.Concurrency != 4 || cfg.LegacyUpload.QuietPeriod != 45*time.Second || cfg.LegacyUpload.MaxAttempts != 7 || strings.Join(cfg.LegacyUpload.IncludeTypes, ",") != "video,nfo" {
		t.Fatalf("legacy upload configuration was not captured: %#v", cfg.LegacyUpload)
	}
	jsonData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContainLegacyUpload(jsonData) || bytesContainLegacyUpload(yamlData) {
		t.Fatalf("legacy upload configuration leaked during serialization: json=%s yaml=%s", jsonData, yamlData)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "upload:") || strings.Contains(string(data), "quietPeriod:") {
		t.Fatalf("legacy global upload configuration should be removed: %s", data)
	}
}

func bytesContainLegacyUpload(data []byte) bool {
	value := string(data)
	return strings.Contains(value, "upload") || strings.Contains(value, "quietPeriod") || strings.Contains(value, "includeTypes")
}
