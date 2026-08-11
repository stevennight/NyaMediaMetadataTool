package appcore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"NyaMediaMetadataTool/internal/api"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/runner"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
	"NyaMediaMetadataTool/internal/watcher"
)

type Service struct {
	Config     config.Config
	ConfigPath string
	Store      *store.Store
	Watcher    *watcher.Watcher
	Runner     *runner.Runner
	Uploads    *upload.Manager
	API        *api.Server

	logger *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup

	stopOnce  sync.Once
	closeOnce sync.Once
	closeErr  error
}

type ActiveWork struct {
	Tasks      int `json:"tasks"`
	Uploads    int `json:"uploads"`
	Requests   int `json:"requests"`
	Mutations  int `json:"mutations"`
	Background int `json:"background"`
}

func Start(parent context.Context, configPath string, logger *slog.Logger) (*Service, error) {
	if parent == nil {
		parent = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	cleanupStore := true
	defer func() {
		if cleanupStore {
			_ = db.Close()
		}
	}()

	ctx := context.Background()
	if err := db.Migrate(ctx, cfg.LegacyUpload); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	uploadRuntime, err := db.GetUploadRuntimeOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("load upload runtime options: %w", err)
	}
	if err := db.ResetRunningTasks(ctx); err != nil {
		return nil, fmt.Errorf("reset running tasks: %w", err)
	}
	if err := db.ResetRunningUploadWork(ctx); err != nil {
		return nil, fmt.Errorf("reset running upload work: %w", err)
	}
	if err := db.ResetProcessingUploadNotifications(ctx); err != nil {
		return nil, fmt.Errorf("reset processing upload notifications: %w", err)
	}
	if err := db.DisableWatchDirScanOnStart(ctx); err != nil {
		return nil, fmt.Errorf("disable watch dir scan on start: %w", err)
	}
	dirs, err := db.ListWatchDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("load watch dirs: %w", err)
	}
	cfg.WatchDirs = watchDirsFromStore(dirs)

	serviceCtx, cancel := context.WithCancel(parent)
	watcherService := watcher.New(cfg, db, logger)
	uploadManager := upload.NewWithOptions(upload.Options{
		Concurrency: uploadRuntime.Concurrency,
		QuietPeriod: uploadRuntime.QuietPeriod,
		MaxAttempts: uploadRuntime.MaxAttempts,
	}, db, logger)
	taskRunner := runner.New(cfg, db, logger, uploadManager)

	service := &Service{
		Config:     cfg,
		ConfigPath: configPath,
		Store:      db,
		Watcher:    watcherService,
		Runner:     taskRunner,
		Uploads:    uploadManager,
		logger:     logger,
		cancel:     cancel,
	}
	service.API = api.NewServerWithContext(serviceCtx, cfg, configPath, db, taskRunner, watcherService, logger, uploadManager)
	service.run("watcher", watcherService.Run, serviceCtx)
	service.run("task runner", taskRunner.Run, serviceCtx)
	service.run("upload manager", uploadManager.Run, serviceCtx)

	cleanupStore = false
	return service, nil
}

func (s *Service) Handler() http.Handler {
	return s.API
}

func (s *Service) ActiveWork(ctx context.Context) (ActiveWork, error) {
	tasks, err := s.Store.CountTasksByStatuses(ctx, "pending", "running")
	if err != nil {
		return ActiveWork{}, err
	}
	uploads, err := s.Store.CountUploadTargetsByStatuses(ctx, "waiting", "pending", "running")
	if err != nil {
		return ActiveWork{}, err
	}
	activity := s.API.Activity()
	return ActiveWork{
		Tasks:      tasks,
		Uploads:    uploads,
		Requests:   activity.InFlight,
		Mutations:  activity.ActiveMutations,
		Background: activity.Background,
	}, nil
}

func (s *Service) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.API.BeginClose()
	s.stopOnce.Do(s.cancel)
	if err := s.API.Close(ctx); err != nil {
		return err
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	s.closeOnce.Do(func() {
		s.closeErr = s.Store.Close()
	})
	return s.closeErr
}

func (s *Service) run(name string, run func(context.Context) error, ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			s.logger.Error(name+" stopped", "error", err)
		}
	}()
}

func watchDirsFromStore(dirs []store.WatchDir) []config.WatchDir {
	result := make([]config.WatchDir, 0, len(dirs))
	for _, dir := range dirs {
		result = append(result, config.WatchDir{
			Path:         dir.Path,
			Recursive:    dir.Recursive,
			Enabled:      dir.Enabled,
			WatchEnabled: dir.WatchEnabled,
			ScanOnStart:  dir.ScanOnStart,
		})
	}
	return result
}
