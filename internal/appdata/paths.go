package appdata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"NyaMediaMetadataTool/internal/config"
)

const directoryName = "NyaMediaMetadataTool"

type Paths struct {
	Root     string `json:"root"`
	Config   string `json:"config"`
	Database string `json:"database"`
	Logs     string `json:"logs"`
}

func DefaultPaths() (Paths, error) {
	if override := os.Getenv("NYAMMD_DATA_DIR"); override != "" {
		root, err := filepath.Abs(override)
		if err != nil {
			return Paths{}, err
		}
		return pathsForRoot(root), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root, err := rootFor(runtime.GOOS, os.Getenv, home)
	if err != nil {
		return Paths{}, err
	}
	return pathsForRoot(root), nil
}

func pathsForRoot(root string) Paths {
	return Paths{
		Root:     root,
		Config:   filepath.Join(root, "config.yaml"),
		Database: filepath.Join(root, "nyamedia.db"),
		Logs:     filepath.Join(root, "logs"),
	}
}

func Ensure(paths Paths) error {
	return EnsureContext(context.Background(), paths)
}

// EnsureContext prepares the desktop data directory and allows a first-run
// legacy database snapshot to be canceled by its caller.
func EnsureContext(ctx context.Context, paths Paths) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if paths.Root == "" || paths.Config == "" || paths.Database == "" {
		return errors.New("desktop data paths are incomplete")
	}
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return err
	}
	if err := MigrateLegacyContext(ctx, paths); err != nil {
		return fmt.Errorf("migrate legacy application data: %w", err)
	}
	if _, err := os.Stat(paths.Config); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cfg := config.Default()
	cfg.Database.Path = paths.Database
	autoDetectTools(&cfg.Tools)
	return config.Save(paths.Config, cfg)
}

func rootFor(goos string, getenv func(string) string, home string) (string, error) {
	if home == "" {
		return "", errors.New("user home directory is empty")
	}
	switch goos {
	case "windows":
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, directoryName), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", directoryName), nil
	default:
		base := getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "nya-media-metadata-tool"), nil
	}
}

func autoDetectTools(tools *config.ToolsConfig) {
	tools.FFmpeg = findTool("ffmpeg")
	tools.FFprobe = findTool("ffprobe")
	tools.MediaInfo = findTool("mediainfo")
}

func findTool(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		return absolute
	}
	return path
}
