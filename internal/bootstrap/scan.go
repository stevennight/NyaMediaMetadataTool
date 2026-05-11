package bootstrap

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func SyncAndScan(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) error {
	if err := st.SyncWatchDirs(ctx, cfg.WatchDirs); err != nil {
		return err
	}

	for _, dir := range cfg.WatchDirs {
		if err := ScanWatchDir(ctx, cfg, st, logger, dir); err != nil {
			logger.Warn("bootstrap scan failed", "path", dir.Path, "error", err)
		}
	}
	return nil
}

func ScanWatchDir(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, dir config.WatchDir) error {
	if !dir.Enabled {
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

		if err := st.EnqueueMediaTask(ctx, mediaFileID); err != nil {
			logger.Warn("enqueue media task failed", "path", path, "error", err)
		}
		return nil
	})
}

func isStable(modifiedAt time.Time, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	return time.Since(modifiedAt) >= delay
}
