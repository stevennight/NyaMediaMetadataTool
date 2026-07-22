package watcher

import (
	"context"
	"errors"
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
	cfg         config.Config
	store       *store.Store
	logger      *slog.Logger
	allowed     map[string]struct{}
	mu          sync.Mutex
	timers      map[string]*time.Timer
	timerRuns   map[string]string
	runPending  map[string]int
	runErrors   map[string][]error
	activeRunID string
	reloadCh    chan reloadRequest
	asyncWG     sync.WaitGroup
	stopping    bool
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
	return &Watcher{
		cfg:        cfg,
		store:      st,
		logger:     logger,
		allowed:    allowed,
		timers:     make(map[string]*time.Timer),
		timerRuns:  make(map[string]string),
		runPending: make(map[string]int),
		runErrors:  make(map[string][]error),
		reloadCh:   make(chan reloadRequest),
	}
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
	defer w.stopAsync()

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
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				w.startAsync(func() { w.recoverFromOverflow(ctx) })
			}
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
			dir, err := w.store.FindWatchDirForPath(ctx, event.Name)
			if err != nil || !dir.Recursive {
				return
			}
			_ = w.addWatchDirs(fsw, watched, event.Name, true)
			w.startAsync(func() { w.scheduleDirectory(ctx, event.Name) })
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
	if w.stopping {
		w.mu.Unlock()
		return
	}
	if timer, ok := w.timers[path]; ok {
		timer.Stop()
	} else {
		if w.activeRunID == "" {
			candidate := bootstrap.NewScanRunID()
			if err := w.store.BeginScanRun(ctx, candidate, "watcher", filepath.Dir(path)); err != nil {
				w.logger.Warn("begin watcher scan run failed", "path", path, "error", err)
			} else {
				w.activeRunID = candidate
			}
		}
		w.timerRuns[path] = w.activeRunID
		if w.activeRunID != "" {
			w.runPending[w.activeRunID]++
		}
	}
	var scheduledTimer *time.Timer
	scheduledTimer = time.AfterFunc(w.cfg.Processing.StableDelay, func() {
		w.mu.Lock()
		if w.timers[path] != scheduledTimer {
			w.mu.Unlock()
			return
		}
		scanRunID := w.timerRuns[path]
		delete(w.timers, path)
		delete(w.timerRuns, path)
		if w.stopping {
			sealRun, scanErr := w.releaseScanRunLocked(scanRunID, nil)
			w.mu.Unlock()
			if sealRun {
				w.finishWatcherScanRun(scanRunID, scanErr)
			}
			return
		}
		w.asyncWG.Add(1)
		w.mu.Unlock()
		defer w.asyncWG.Done()
		itemErr := w.scheduleFile(ctx, path, scanRunID)
		w.completeScanRunItem(scanRunID, itemErr)
	})
	w.timers[path] = scheduledTimer
	w.mu.Unlock()
}

func (w *Watcher) startAsync(run func()) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping {
		return false
	}
	w.asyncWG.Add(1)
	go func() {
		defer w.asyncWG.Done()
		run()
	}()
	return true
}

func (w *Watcher) stopAsync() {
	w.mu.Lock()
	w.stopping = true
	sealRuns := make(map[string]error)
	for path, timer := range w.timers {
		timer.Stop()
		delete(w.timers, path)
		scanRunID := w.timerRuns[path]
		delete(w.timerRuns, path)
		if sealRun, scanErr := w.releaseScanRunLocked(scanRunID, nil); sealRun {
			sealRuns[scanRunID] = scanErr
		}
	}
	w.mu.Unlock()
	for scanRunID, scanErr := range sealRuns {
		w.finishWatcherScanRun(scanRunID, scanErr)
	}
	w.asyncWG.Wait()
}

func (w *Watcher) recoverFromOverflow(ctx context.Context) {
	dirs, err := w.store.ListWatchDirs(ctx)
	if err != nil {
		w.logger.Warn("reload directories after watcher overflow failed", "error", err)
		return
	}
	for _, dir := range dirs {
		if ctx.Err() != nil {
			return
		}
		if !dir.Enabled || !dir.WatchEnabled {
			continue
		}
		cfgDir := config.WatchDir{Path: dir.Path, Recursive: dir.Recursive, Enabled: true, WatchEnabled: true}
		if err := bootstrap.ScanWatchDir(ctx, w.cfg, w.store, w.logger, cfgDir, bootstrap.ScanOptions{InheritProcessing: true}); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Warn("rescan after watcher overflow failed", "path", dir.Path, "error", err)
		}
	}
}

func (w *Watcher) cancelTimersOutside(activeRoots []string) {
	w.mu.Lock()
	sealRuns := make(map[string]error)
	for path, timer := range w.timers {
		if isUnderAnyRoot(path, activeRoots) {
			continue
		}
		timer.Stop()
		delete(w.timers, path)
		scanRunID := w.timerRuns[path]
		delete(w.timerRuns, path)
		if sealRun, scanErr := w.releaseScanRunLocked(scanRunID, nil); sealRun {
			sealRuns[scanRunID] = scanErr
		}
	}
	w.mu.Unlock()
	for scanRunID, scanErr := range sealRuns {
		w.finishWatcherScanRun(scanRunID, scanErr)
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

func (w *Watcher) scheduleFile(ctx context.Context, path string, scanRunID string) error {
	if hasIgnoreFileInAncestors(filepath.Dir(path)) {
		return nil
	}

	checks := w.cfg.Processing.StableChecks
	if checks <= 0 {
		checks = 1
	}
	var info os.FileInfo
	for i := 0; i < checks; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var err error
		info, err = os.Stat(path)
		if err != nil || info.IsDir() {
			return nil
		}
		if time.Since(info.ModTime()) >= w.cfg.Processing.StableDelay {
			break
		}
		if i == checks-1 {
			w.debounceFile(ctx, path)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.cfg.Processing.StableDelay):
		}
	}

	mediaFileID, err := w.store.UpsertMediaFile(ctx, path, info)
	if err != nil {
		w.logger.Warn("watch upsert media file failed", "path", path, "error", err)
		return err
	}
	strategy := w.cfg.Processing.Strategy
	if dir, findErr := w.store.FindWatchDirForPath(ctx, path); findErr == nil && !dir.UseGlobalProcessing {
		strategy = dir.Processing.Strategy
	}
	options := bootstrap.ScanOptionsFromStrategy(strategy)
	var enqueueErr error
	if scanRunID == "" {
		enqueueErr = w.store.EnqueueMediaTaskWithOptions(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly)
	} else {
		enqueueErr = w.store.EnqueueMediaTaskWithScanRun(ctx, mediaFileID, options.OverwriteExisting, options.Force || options.MissingOnly, scanRunID)
	}
	if enqueueErr != nil {
		w.logger.Warn("watch enqueue media task failed", "path", path, "error", enqueueErr)
	}
	return enqueueErr
}

func (w *Watcher) completeScanRunItem(scanRunID string, itemErr error) {
	if scanRunID == "" {
		return
	}
	w.mu.Lock()
	sealRun, scanErr := w.releaseScanRunLocked(scanRunID, itemErr)
	w.mu.Unlock()
	if sealRun {
		w.finishWatcherScanRun(scanRunID, scanErr)
	}
}

func (w *Watcher) releaseScanRunLocked(scanRunID string, itemErr error) (bool, error) {
	if scanRunID == "" {
		return false, nil
	}
	pendingCount, ok := w.runPending[scanRunID]
	if !ok {
		return false, nil
	}
	if itemErr != nil {
		w.runErrors[scanRunID] = append(w.runErrors[scanRunID], itemErr)
	}
	pending := pendingCount - 1
	if pending > 0 {
		w.runPending[scanRunID] = pending
		return false, nil
	}
	delete(w.runPending, scanRunID)
	if w.activeRunID == scanRunID {
		w.activeRunID = ""
	}
	scanErr := errors.Join(w.runErrors[scanRunID]...)
	delete(w.runErrors, scanRunID)
	return true, scanErr
}

func (w *Watcher) finishWatcherScanRun(scanRunID string, scanErr error) {
	if scanRunID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errorSummary := ""
	if scanErr != nil {
		errorSummary = scanErr.Error()
	}
	if err := w.store.FinishScanRun(ctx, scanRunID, errorSummary); err != nil {
		w.logger.Warn("finish watcher scan run failed", "scanRunID", scanRunID, "error", err)
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
