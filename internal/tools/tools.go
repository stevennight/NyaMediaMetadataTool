package tools

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

type Status struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Error     string `json:"error"`
	CheckedAt string `json:"checkedAt"`
}

func CheckAll(ctx context.Context, cfg config.ToolsConfig) []Status {
	checks := []struct {
		name string
		path string
		args []string
	}{
		{name: "ffmpeg", path: cfg.FFmpeg, args: []string{"-version"}},
		{name: "ffprobe", path: cfg.FFprobe, args: []string{"-version"}},
		{name: "mkvextract", path: cfg.MKVExtract, args: []string{"--version"}},
		{name: "mediainfo", path: cfg.MediaInfo, args: []string{"--Version"}},
	}

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

	cmd := exec.CommandContext(checkCtx, path, args...)
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
