package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

type Watcher struct {
	cfg     config.Config
	store   *store.Store
	logger  *slog.Logger
	allowed map[string]struct{}
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *Watcher {
	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}
	return &Watcher{cfg: cfg, store: st, logger: logger, allowed: allowed}
}

func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	for _, dir := range w.cfg.WatchDirs {
		if !dir.Enabled {
			continue
		}
		if err := w.addWatchDirs(fsw, dir.Path, dir.Recursive); err != nil {
			w.logger.Warn("add watcher failed", "path", dir.Path, "error", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, fsw, event)
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("watcher error", "error", err)
		}
	}
}

func (w *Watcher) addWatchDirs(fsw *fsnotify.Watcher, root string, recursive bool) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && !recursive {
			return filepath.SkipDir
		}
		return fsw.Add(path)
	})
}

func (w *Watcher) handleEvent(ctx context.Context, fsw *fsnotify.Watcher, event fsnotify.Event) {
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			_ = w.addWatchDirs(fsw, event.Name, true)
			return
		}
	}

	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return
	}
	if _, ok := w.allowed[strings.ToLower(filepath.Ext(event.Name))]; !ok {
		return
	}

	go w.scheduleFile(ctx, event.Name)
}

func (w *Watcher) scheduleFile(ctx context.Context, path string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(w.cfg.Processing.StableDelay):
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	if time.Since(info.ModTime()) < w.cfg.Processing.StableDelay {
		return
	}

	mediaFileID, err := w.store.UpsertMediaFile(ctx, path, info)
	if err != nil {
		w.logger.Warn("watch upsert media file failed", "path", path, "error", err)
		return
	}
	if err := w.store.EnqueueMediaTask(ctx, mediaFileID); err != nil {
		w.logger.Warn("watch enqueue media task failed", "path", path, "error", err)
	}
}
