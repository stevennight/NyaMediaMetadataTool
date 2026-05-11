package bootstrap

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

var episodePattern = regexp.MustCompile(`(?i)s\d{1,2}e\d{1,4}\b`)
var seasonDirPattern = regexp.MustCompile(`(?i)^(season\s*\d{1,2}|s\d{1,2}|第\s*\d{1,2}\s*季)$`)

type ScanOptions struct {
	OverwriteExisting bool
	Force             bool
	MissingOnly       bool
}

func SyncAndScan(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) error {
	if err := st.SyncWatchDirs(ctx, cfg.WatchDirs); err != nil {
		return err
	}

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

		if options.MissingOnly && !hasMissingOutputs(cfg, path) {
			return nil
		}

		if err := st.EnqueueMediaTaskWithOptions(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly); err != nil {
			logger.Warn("enqueue media task failed", "path", path, "error", err)
		}
		return nil
	})
}

func ScanPath(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, path string, options ScanOptions) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return ScanWatchDir(ctx, cfg, st, logger, config.WatchDir{Path: path, Recursive: true, Enabled: true}, options)
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}
	if _, ok := allowed[strings.ToLower(filepath.Ext(path))]; !ok {
		return nil
	}
	if options.MissingOnly && !hasMissingOutputs(cfg, path) {
		return nil
	}
	mediaFileID, err := st.UpsertMediaFile(ctx, path, info)
	if err != nil {
		return err
	}
	return st.EnqueueMediaTaskWithOptions(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly)
}

func hasMissingOutputs(cfg config.Config, mediaPath string) bool {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	if cfg.Processing.EnableMediaInfo && missing(base+"-mediainfo.json") {
		return true
	}
	if cfg.Processing.EnableBIF && missing(base+"-"+strconv.Itoa(cfg.Processing.BIFWidth)+"-"+strconv.Itoa(cfg.Processing.BIFInterval)+".bif") {
		return true
	}
	if cfg.Processing.EnableNFO {
		if missing(base + ".nfo") {
			return true
		}
		if episodePattern.MatchString(filepath.Base(mediaPath)) {
			if missing(filepath.Join(showBaseDir(base), "tvshow.nfo")) || missing(filepath.Join(filepath.Dir(mediaPath), "season.nfo")) {
				return true
			}
		}
	}
	return false
}

func missing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

func showBaseDir(basePath string) string {
	dir := filepath.Dir(basePath)
	if seasonDirPattern.MatchString(filepath.Base(dir)) {
		return filepath.Dir(dir)
	}
	return dir
}

func isStable(modifiedAt time.Time, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	return time.Since(modifiedAt) >= delay
}
