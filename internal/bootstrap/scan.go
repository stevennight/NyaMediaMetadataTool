package bootstrap

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

const ignoreFileName = ".ignore"

type ScanOptions struct {
	OverwriteExisting bool
	Force             bool
	MissingOnly       bool
	ScanRunID         string
}

func SyncAndScan(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) error {
	for _, dir := range cfg.WatchDirs {
		if err := ScanWatchDir(ctx, cfg, st, logger, dir, ScanOptions{OverwriteExisting: cfg.Processing.OverwriteExisting}); err != nil {
			logger.Warn("bootstrap scan failed", "path", dir.Path, "error", err)
		}
	}
	return nil
}

func ScanWatchDir(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, dir config.WatchDir, options ScanOptions) error {
	if !dir.Enabled {
		return nil
	}
	if options.ScanRunID == "" {
		options.ScanRunID = newScanRunID()
	}
	if hasIgnoreFileInAncestors(dir.Path) {
		return nil
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}

	return filepath.WalkDir(dir.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if hasIgnoreFile(path) {
				return filepath.SkipDir
			}
			if path != dir.Path && !dir.Recursive {
				return filepath.SkipDir
			}
			return nil
		}

		if _, ok := allowed[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		if !isStable(info.ModTime(), cfg.Processing.StableDelay) {
			logger.Info("skip unstable file during bootstrap", "path", path)
			return nil
		}

		mediaFileID, err := st.UpsertMediaFile(ctx, path, info)
		if err != nil {
			logger.Warn("upsert media file failed", "path", path, "error", err)
			return nil
		}

		if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly, options.ScanRunID); err != nil {
			logger.Warn("enqueue media task failed", "path", path, "error", err)
		}
		return nil
	})
}

func ScanPath(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, path string, options ScanOptions) error {
	if options.ScanRunID == "" {
		options.ScanRunID = newScanRunID()
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return ScanWatchDir(ctx, cfg, st, logger, config.WatchDir{Path: path, Recursive: true, Enabled: true}, options)
	}
	if hasIgnoreFileInAncestors(filepath.Dir(path)) {
		return nil
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}
	if _, ok := allowed[strings.ToLower(filepath.Ext(path))]; !ok {
		return nil
	}
	mediaFileID, err := st.UpsertMediaFile(ctx, path, info)
	if err != nil {
		return err
	}
	return st.EnqueueMediaTaskWithScanRun(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly, options.ScanRunID)
}

func newScanRunID() string {
	return fmt.Sprintf("scan-%d", time.Now().UTC().UnixNano())
}

func hasIgnoreFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ignoreFileName))
	return err == nil
}

func hasIgnoreFileInAncestors(dir string) bool {
	for {
		if hasIgnoreFile(dir) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func isStable(modifiedAt time.Time, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	return time.Since(modifiedAt) >= delay
}
