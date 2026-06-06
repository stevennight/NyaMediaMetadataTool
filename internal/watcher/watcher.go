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

	"NyaMediaMetadataTool/internal/bootstrap"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

type Watcher struct {
	cfg      config.Config
	store    *store.Store
	logger   *slog.Logger
	allowed  map[string]struct{}
	mu       sync.Mutex
	timers   map[string]*time.Timer
	reloadCh chan reloadRequest
}

type reloadRequest struct {
	done chan error
}

const ignoreFileName = ".ignore"

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *Watcher {
	allowed := make(map[string]struct{}, len(cfg.Processing.Extensions))
	for _, ext := range cfg.Processing.Extensions {
		allowed[strings.ToLower(ext)] = struct{}{}
	}
	return &Watcher{cfg: cfg, store: st, logger: logger, allowed: allowed, timers: map[string]*time.Timer{}, reloadCh: make(chan reloadRequest)}
}

func (w *Watcher) ReloadWatchDirs(ctx context.Context) error {
	request := reloadRequest{done: make(chan error, 1)}
	select {
	case w.reloadCh <- request:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	watched := map[string]struct{}{}
	if err := w.reloadWatchDirs(ctx, fsw, watched); err != nil {
		w.logger.Warn("initial watcher reload failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-w.reloadCh:
			request.done <- w.reloadWatchDirs(ctx, fsw, watched)
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, fsw, watched, event)
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("watcher error", "error", err)
		}
	}
}

func (w *Watcher) reloadWatchDirs(ctx context.Context, fsw *fsnotify.Watcher, watched map[string]struct{}) error {
	dirs, err := w.store.ListWatchDirs(ctx)
	if err != nil {
		return err
	}

	desired := map[string]struct{}{}
	activeRoots := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.WatchEnabled {
			continue
		}
		activeRoots = append(activeRoots, dir.Path)
		if err := collectWatchDirs(desired, dir.Path, dir.Recursive); err != nil {
			w.logger.Warn("collect watch dirs failed", "path", dir.Path, "error", err)
		}
	}

	for path := range watched {
		if _, ok := desired[path]; ok {
			continue
		}
		if err := fsw.Remove(path); err != nil {
			w.logger.Warn("remove watcher failed", "path", path, "error", err)
		}
		delete(watched, path)
	}

	for path := range desired {
		if _, ok := watched[path]; ok {
			continue
		}
		if err := fsw.Add(path); err != nil {
			w.logger.Warn("add watcher failed", "path", path, "error", err)
			continue
		}
		watched[path] = struct{}{}
	}

	w.cancelTimersOutside(activeRoots)
	return nil
}

func collectWatchDirs(result map[string]struct{}, root string, recursive bool) error {
	if hasIgnoreFileInAncestors(root) {
		return nil
	}
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
		result[path] = struct{}{}
		return nil
	})
}

func (w *Watcher) addWatchDirs(fsw *fsnotify.Watcher, watched map[string]struct{}, root string, recursive bool) error {
	dirs := map[string]struct{}{}
	if err := collectWatchDirs(dirs, root, recursive); err != nil {
		return err
	}
	for path := range dirs {
		if _, ok := watched[path]; ok {
			continue
		}
		if err := fsw.Add(path); err != nil {
			w.logger.Warn("add watcher failed", "path", path, "error", err)
			continue
		}
		watched[path] = struct{}{}
	}
	return nil
}

func (w *Watcher) handleEvent(ctx context.Context, fsw *fsnotify.Watcher, watched map[string]struct{}, event fsnotify.Event) {
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if hasIgnoreFile(event.Name) {
				return
			}
			_ = w.addWatchDirs(fsw, watched, event.Name, true)
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
	if hasIgnoreFileInAncestors(root) {
		return
	}

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

func (w *Watcher) cancelTimersOutside(activeRoots []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, timer := range w.timers {
		if isUnderAnyRoot(path, activeRoots) {
			continue
		}
		timer.Stop()
		delete(w.timers, path)
	}
}

func isUnderAnyRoot(path string, roots []string) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
			return true
		}
	}
	return false
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
	strategy := w.cfg.Processing.Strategy
	if dir, findErr := w.store.FindWatchDirForPath(ctx, path); findErr == nil && !dir.UseGlobalProcessing {
		strategy = dir.Processing.Strategy
	}
	options := bootstrap.ScanOptionsFromStrategy(strategy)
	if err := w.store.EnqueueMediaTaskWithOptions(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly); err != nil {
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
