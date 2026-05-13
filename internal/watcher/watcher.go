package watcher

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	mu      sync.Mutex
	timers  map[string]*time.Timer
}

const ignoreFileName = ".ignore"

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *Watcher {
	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}
	return &Watcher{cfg: cfg, store: st, logger: logger, allowed: allowed, timers: map[string]*time.Timer{}}
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
		if hasIgnoreFile(path) {
			return filepath.SkipDir
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
			if hasIgnoreFile(event.Name) {
				return
			}
			_ = w.addWatchDirs(fsw, event.Name, true)
			go w.scheduleDirectory(ctx, event.Name)
			return
		}
	}

	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return
	}
	if _, ok := w.allowed[strings.ToLower(filepath.Ext(event.Name))]; !ok {
		return
	}

	w.debounceFile(ctx, event.Name)
}

func (w *Watcher) scheduleDirectory(ctx context.Context, root string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(w.cfg.Processing.StableDelay):
	}

	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if hasIgnoreFile(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := w.allowed[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}
		w.debounceFile(ctx, path)
		return nil
	})
}

func (w *Watcher) debounceFile(ctx context.Context, path string) {
	w.mu.Lock()
	if timer, ok := w.timers[path]; ok {
		timer.Stop()
	}
	w.timers[path] = time.AfterFunc(w.cfg.Processing.StableDelay, func() {
		w.mu.Lock()
		delete(w.timers, path)
		w.mu.Unlock()
		w.scheduleFile(ctx, path)
	})
	w.mu.Unlock()
}

func (w *Watcher) scheduleFile(ctx context.Context, path string) {
	if hasIgnoreFileInAncestors(filepath.Dir(path)) {
		return
	}

	checks := w.cfg.Processing.StableChecks
	if checks <= 0 {
		checks = 1
	}
	var info os.FileInfo
	for i := 0; i < checks; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var err error
		info, err = os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		if time.Since(info.ModTime()) >= w.cfg.Processing.StableDelay {
			break
		}
		if i == checks-1 {
			w.debounceFile(ctx, path)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.cfg.Processing.StableDelay):
		}
	}

	mediaFileID, err := w.store.UpsertMediaFile(ctx, path, info)
	if err != nil {
		w.logger.Warn("watch upsert media file failed", "path", path, "error", err)
		return
	}
	if err := w.store.EnqueueMediaTaskWithOverwrite(ctx, mediaFileID, w.cfg.Processing.OverwriteExisting); err != nil {
		w.logger.Warn("watch enqueue media task failed", "path", path, "error", err)
	}
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
