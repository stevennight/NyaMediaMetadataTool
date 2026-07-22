package bootstrap

import (
	"context"
	"errors"
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
	Processing        *config.OutputProcessingConfig
	InheritProcessing bool
}

func SyncAndScan(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) error {
	for _, dir := range cfg.WatchDirs {
		if err := ScanWatchDir(ctx, cfg, st, logger, dir, ScanOptionsFromStrategy(cfg.Processing.Strategy)); err != nil {
			logger.Warn("bootstrap scan failed", "path", dir.Path, "error", err)
		}
	}
	return nil
}

func ScanOptionsFromStrategy(strategy string) ScanOptions {
	if strings.TrimSpace(strategy) == config.ProcessingStrategyForce {
		return ScanOptions{OverwriteExisting: true, Force: true}
	}
	return ScanOptions{MissingOnly: true}
}

func ScanWatchDir(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, dir config.WatchDir, options ScanOptions) (scanErr error) {
	if !dir.ScanOnStart {
		return nil
	}
	ownedRun := options.ScanRunID == ""
	if options.ScanRunID == "" {
		options.ScanRunID = NewScanRunID()
	}
	if ownedRun {
		if err := st.BeginScanRun(ctx, options.ScanRunID, "scan", dir.Path); err != nil {
			return fmt.Errorf("begin scan run: %w", err)
		}
		defer func() {
			scanErr = finishOwnedScanRun(ctx, st, options.ScanRunID, scanErr)
		}()
	}
	if hasIgnoreFileInAncestors(dir.Path) {
		return nil
	}

	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}

	var fileErrors []error
	walkErr := filepath.WalkDir(dir.Path, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if path == dir.Path {
				return err
			}
			fileErrors = append(fileErrors, fmt.Errorf("scan %q: %w", path, err))
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
			fileErrors = append(fileErrors, fmt.Errorf("inspect %q: %w", path, err))
			return nil
		}

		if !isStable(info.ModTime(), cfg.Processing.StableDelay) {
			logger.Info("skip unstable file during bootstrap", "path", path)
			return nil
		}

		mediaFileID, err := st.UpsertMediaFile(ctx, path, info)
		if err != nil {
			logger.Warn("upsert media file failed", "path", path, "error", err)
			fileErrors = append(fileErrors, fmt.Errorf("upsert %q: %w", path, err))
			return nil
		}

		if err := enqueueScannedMedia(ctx, cfg, st, path, mediaFileID, options); err != nil {
			logger.Warn("enqueue media task failed", "path", path, "error", err)
			fileErrors = append(fileErrors, fmt.Errorf("enqueue %q: %w", path, err))
		}
		return nil
	})
	return errors.Join(walkErr, errors.Join(fileErrors...))
}

func ScanPath(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger, path string, options ScanOptions) (scanErr error) {
	ownedRun := options.ScanRunID == ""
	if options.ScanRunID == "" {
		options.ScanRunID = NewScanRunID()
	}
	if ownedRun {
		if err := st.BeginScanRun(ctx, options.ScanRunID, "scan", path); err != nil {
			return fmt.Errorf("begin scan run: %w", err)
		}
		defer func() {
			scanErr = finishOwnedScanRun(ctx, st, options.ScanRunID, scanErr)
		}()
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return ScanWatchDir(ctx, cfg, st, logger, config.WatchDir{Path: path, Recursive: true, Enabled: true, ScanOnStart: true}, options)
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
	return enqueueScannedMedia(ctx, cfg, st, path, mediaFileID, options)
}

func enqueueScannedMedia(ctx context.Context, cfg config.Config, st *store.Store, path string, mediaFileID int64, options ScanOptions) error {
	if options.InheritProcessing {
		processing := cfg.Processing.OutputConfig()
		if dir, err := st.FindWatchDirForPath(ctx, path); err == nil && !dir.UseGlobalProcessing {
			processing = dir.Processing
		}
		resolved := ScanOptionsFromStrategy(processing.Strategy)
		options.OverwriteExisting = resolved.OverwriteExisting
		options.Force = resolved.Force
		options.MissingOnly = resolved.MissingOnly
	}
	return st.EnqueueMediaTaskWithProcessing(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly, options.ScanRunID, options.Processing)
}

func NewScanRunID() string {
	return fmt.Sprintf("scan-%d", time.Now().UTC().UnixNano())
}

func finishOwnedScanRun(ctx context.Context, st *store.Store, scanRunID string, scanErr error) error {
	errorSummary := ""
	if scanErr != nil {
		errorSummary = scanErr.Error()
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := st.FinishScanRun(finishCtx, scanRunID, errorSummary); err != nil {
		return errors.Join(scanErr, fmt.Errorf("finish scan run: %w", err))
	}
	return scanErr
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
