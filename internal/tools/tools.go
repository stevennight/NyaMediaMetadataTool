package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/executil"
)

type Status struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Error     string `json:"error"`
	CheckedAt string `json:"checkedAt"`
}

type checkDefinition struct {
	name string
	path string
	args []string
}

func checkDefinitions(cfg config.ToolsConfig) []checkDefinition {
	return []checkDefinition{
		{name: "ffmpeg", path: cfg.FFmpeg, args: []string{"-version"}},
		{name: "ffprobe", path: cfg.FFprobe, args: []string{"-version"}},
		{name: "mediainfo", path: cfg.MediaInfo, args: []string{"--Version"}},
	}
}

func IsCurrentStatusSet(statuses []Status) bool {
	definitions := checkDefinitions(config.ToolsConfig{})
	if len(statuses) != len(definitions) {
		return false
	}

	expected := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		expected[definition.name] = struct{}{}
	}
	for _, status := range statuses {
		if _, ok := expected[status.Name]; !ok {
			return false
		}
		delete(expected, status.Name)
	}
	return len(expected) == 0
}

func CheckAll(ctx context.Context, cfg config.ToolsConfig) []Status {
	checks := checkDefinitions(cfg)
	statuses := make([]Status, 0, len(checks))
	for _, check := range checks {
		statuses = append(statuses, Check(ctx, check.name, check.path, check.args...))
	}
	return statuses
}

func Check(ctx context.Context, name string, path string, args ...string) Status {
	status := Status{
		Name:      name,
		Path:      path,
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	if strings.TrimSpace(path) == "" {
		status.Error = "tool path is empty"
		return status
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := executil.CommandContext(checkCtx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
			status.Error = "tool check timed out"
			return status
		}
		status.Error = err.Error()
		if len(output) > 0 {
			status.Error += ": " + firstLine(string(output))
		}
		return status
	}

	status.Available = true
	status.Version = firstLine(string(output))
	return status
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	line, _, _ := strings.Cut(value, "\n")
	return strings.TrimSpace(line)
}
