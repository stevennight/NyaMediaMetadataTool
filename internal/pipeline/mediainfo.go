package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func GenerateMediaInfo(ctx context.Context, cfg config.Config, media store.MediaFile) (string, error) {
	if !cfg.Processing.EnableMediaInfo {
		return "", nil
	}

	outputPath := strings.TrimSuffix(media.Path, filepath.Ext(media.Path)) + "-mediainfo.json"
	if !cfg.Processing.OverwriteExisting {
		if _, err := os.Stat(outputPath); err == nil {
			return outputPath, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}

	var content []byte
	var err error
	if strings.TrimSpace(cfg.Tools.MediaInfo) != "" {
		content, err = runCommand(ctx, cfg.Tools.MediaInfo, "--Output=JSON", media.Path)
		if err == nil && len(strings.TrimSpace(string(content))) > 0 {
			return writeOutput(outputPath, content)
		}
	}

	if strings.TrimSpace(cfg.Tools.FFprobe) != "" {
		content, err = runCommand(ctx, cfg.Tools.FFprobe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", media.Path)
		if err == nil && len(strings.TrimSpace(string(content))) > 0 {
			return writeOutput(outputPath, content)
		}
	}

	if err == nil {
		return "", errors.New("no mediainfo output produced")
	}
	return "", err
}

func runCommand(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return runCommandTimeout(ctx, 30*time.Second, bin, args...)
}

func runCommandTimeout(ctx context.Context, timeout time.Duration, bin string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil, err
	}
	return output, nil
}

func writeOutput(path string, content []byte) (string, error) {
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
