package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/pipeline"
	"NyaMediaMetadataTool/internal/store"
)

const maxTaskAttempts = 3

type Runner struct {
	cfg    config.Config
	store  *store.Store
	logger *slog.Logger
	mu     sync.Mutex
	active map[int64]context.CancelFunc
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, store: st, logger: logger, active: make(map[int64]context.CancelFunc)}
}

func (r *Runner) CancelRunningTasks() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := len(r.active)
	for _, cancel := range r.active {
		cancel()
	}
	return count
}

func (r *Runner) Run(ctx context.Context) error {
	workers := r.cfg.Processing.Concurrency
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go r.worker(ctx)
	}
	<-ctx.Done()
	return nil
}

func (r *Runner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := r.store.ClaimNextPendingTask(ctx)
		if err != nil {
			if errors.Is(err, store.ErrNoPendingTask) {
				time.Sleep(2 * time.Second)
				continue
			}
			r.logger.Warn("claim task failed", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		taskCtx, cancel := context.WithCancel(ctx)
		r.trackTask(task.ID, cancel)
		err = r.processTask(taskCtx, task)
		cancel()
		r.untrackTask(task.ID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				continue
			}
			if canceled, checkErr := r.store.IsTaskCanceled(ctx, task.ID); checkErr == nil && canceled {
				continue
			}
			r.logger.Warn("process task failed", "taskID", task.ID, "error", err)
			_ = r.store.AddTaskLog(ctx, task.ID, "error", "task failed", err.Error())
			if task.Attempts < maxTaskAttempts {
				_ = r.store.AddTaskLog(ctx, task.ID, "warning", "task retry scheduled", fmt.Sprintf("attempt %d/%d", task.Attempts+1, maxTaskAttempts))
				if retryErr := r.store.RetryTask(ctx, task.ID, err.Error()); retryErr == nil {
					continue
				} else {
					r.logger.Warn("retry task failed", "taskID", task.ID, "error", retryErr)
				}
			}
			_ = r.store.FailTask(ctx, task.ID, err.Error())
			continue
		}
		if canceled, checkErr := r.store.IsTaskCanceled(ctx, task.ID); checkErr == nil && canceled {
			continue
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "task completed", "")
		_ = r.store.CompleteTask(ctx, task.ID)
	}
}

func (r *Runner) trackTask(taskID int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[taskID] = cancel
}

func (r *Runner) untrackTask(taskID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, taskID)
}

func (r *Runner) processTask(ctx context.Context, task store.Task) error {
	if task.MediaFileID == nil {
		return errors.New("task has no media file id")
	}
	cfg := r.cfg
	media, err := r.store.GetMediaFileByID(ctx, *task.MediaFileID)
	if err != nil {
		return err
	}
	if dir, findErr := r.store.FindWatchDirForPath(ctx, media.Path); findErr == nil && !dir.UseGlobalProcessing {
		cfg.Processing.ApplyOutputConfig(dir.Processing)
	}
	cfg.Processing.OverwriteExisting = task.OverwriteExisting
	failures := make([]string, 0)
	_ = r.store.AddTaskLog(ctx, task.ID, "info", "processing started", media.Path)

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate mediainfo", "")
	mediaInfoPath, err := r.generateMediaInfo(ctx, task, cfg, media)
	if err != nil {
		return err
	}
	if mediaInfoPath != "" {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "mediainfo", mediaInfoPath, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "mediainfo generated", mediaInfoPath)
	} else {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "mediainfo skipped", "")
	}
	if !r.shouldSkipSucceededStage(ctx, task, "mediainfo") {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "mediainfo"); err != nil {
			return err
		}
	}

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "extract subtitles", "")
	subtitles, err := r.generateSubtitles(ctx, task, cfg, media)
	if err != nil {
		return err
	}
	for _, subtitlePath := range subtitles {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "subtitle", subtitlePath, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "subtitle generated", subtitlePath)
	}
	if len(subtitles) == 0 {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "no subtitles generated", "")
	}
	if !r.shouldSkipSucceededStage(ctx, task, "subtitles") {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "subtitles"); err != nil {
			return err
		}
	}

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate bif", "")
	bifPath, err := r.generateBIF(ctx, task, cfg, media)
	if err != nil {
		return err
	}
	if bifPath != "" {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "bif", bifPath, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "bif generated", bifPath)
	} else {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "bif skipped", "")
	}
	if !r.shouldSkipSucceededStage(ctx, task, "bif") {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "bif"); err != nil {
			return err
		}
	}

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate nfo", "")
	nfoResult, err := r.generateNFO(ctx, task, cfg, media)
	if err != nil {
		r.addProcessLogs(ctx, task.ID, nfoResult.TMDBLogs)
		if nfoResult.TMDBStatus != "" {
			_ = r.store.AddTaskLog(ctx, task.ID, tmdbLogLevel(nfoResult.TMDBStatus), "tmdb "+nfoResult.TMDBStatus, tmdbLogDetail(nfoResult))
		}
		return err
	}
	r.addProcessLogs(ctx, task.ID, nfoResult.TMDBLogs)
	if nfoResult.Path != "" {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "nfo", nfoResult.Path, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "nfo generated", nfoResult.Path)
		if nfoResult.ThumbPath != "" {
			if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "thumb", nfoResult.ThumbPath, "generated"); err != nil {
				return err
			}
			_ = r.store.AddTaskLog(ctx, task.ID, "info", "thumb generated", nfoResult.ThumbPath)
		}
		if nfoResult.TMDBStatus != "" {
			_ = r.store.AddTaskLog(ctx, task.ID, tmdbLogLevel(nfoResult.TMDBStatus), "tmdb "+nfoResult.TMDBStatus, tmdbLogDetail(nfoResult))
		}
		for _, failure := range nfoResult.Failures {
			_ = r.store.AddTaskLog(ctx, task.ID, "error", "nfo artifact failed", failure)
			failures = append(failures, failure)
		}
	} else {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "nfo skipped", "")
	}
	if !r.shouldSkipSucceededStage(ctx, task, "nfo") && len(nfoResult.Failures) == 0 {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "nfo"); err != nil {
			return err
		}
	}

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate series nfo", "")
	seriesResult, err := r.generateSeriesNFO(ctx, task, cfg, media)
	r.addProcessLogs(ctx, task.ID, seriesResult.Logs)
	if err != nil {
		return err
	}
	if seriesResult.ShowNFOPath != "" {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "tvshow-nfo", seriesResult.ShowNFOPath, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "tvshow nfo generated", seriesResult.ShowNFOPath)
	}
	if seriesResult.SeasonNFOPath != "" {
		if err := r.store.SaveArtifact(ctx, media.ID, task.ID, "season-nfo", seriesResult.SeasonNFOPath, "generated"); err != nil {
			return err
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "season nfo generated", seriesResult.SeasonNFOPath)
	}
	if !r.shouldSkipSucceededStage(ctx, task, "series-nfo") {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "series-nfo"); err != nil {
			return err
		}
	}

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate series images", "")
	imageResult, err := r.generateSeriesImages(ctx, task, cfg, media)
	r.addProcessLogs(ctx, task.ID, imageResult.Logs)
	if err != nil {
		return err
	}
	for _, image := range imageResult.Images {
		if image.Status == "generated" {
			if err := r.store.SaveArtifact(ctx, media.ID, task.ID, image.Type, image.Path, "generated"); err != nil {
				return err
			}
		}
		level := "info"
		if image.Status == "failed" {
			level = "error"
			failures = append(failures, image.Detail)
		}
		detail := image.Path
		if image.Detail != "" {
			detail = image.Detail
		}
		_ = r.store.AddTaskLog(ctx, task.ID, level, image.Type+" "+image.Status, detail)
	}
	if !r.shouldSkipSucceededStage(ctx, task, "series-images") && !hasFailedImage(imageResult.Images) {
		if err := r.store.MarkTaskStageSucceeded(ctx, task.ID, "series-images"); err != nil {
			return err
		}
	}
	if len(failures) > 0 {
		return errors.New("artifact generation failed: " + strings.Join(failures, "; "))
	}
	return r.store.TouchMediaProcessed(ctx, media.ID)
}

func (r *Runner) generateMediaInfo(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) (string, error) {
	if r.shouldSkipSucceededStage(ctx, task, "mediainfo") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "mediainfo skipped", "already generated by previous attempt")
		return "", nil
	}
	path, err := pipeline.GenerateMediaInfo(ctx, cfg, media)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (r *Runner) generateSubtitles(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) ([]string, error) {
	if r.shouldSkipSucceededStage(ctx, task, "subtitles") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "subtitles skipped", "already generated by previous attempt")
		return nil, nil
	}
	return pipeline.GenerateSubtitles(ctx, cfg, media)
}

func (r *Runner) generateBIF(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) (string, error) {
	if r.shouldSkipSucceededStage(ctx, task, "bif") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "bif skipped", "already generated by previous attempt")
		return "", nil
	}
	path, err := pipeline.GenerateBIF(ctx, cfg, media)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (r *Runner) generateNFO(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) (pipeline.NFOResult, error) {
	if r.shouldSkipSucceededStage(ctx, task, "nfo") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "nfo skipped", "already generated by previous attempt")
		return pipeline.NFOResult{}, nil
	}
	result, err := pipeline.GenerateNFO(ctx, cfg, media)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runner) generateSeriesNFO(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) (pipeline.SeriesResult, error) {
	if r.shouldSkipSucceededStage(ctx, task, "series-nfo") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "series nfo skipped", "already generated by previous attempt")
		return pipeline.SeriesResult{}, nil
	}
	return pipeline.GenerateSeriesNFOWithScopeClaim(ctx, cfg, media, r.scanScopeClaim(task.ScanRunID, task.ID))
}

func (r *Runner) generateSeriesImages(ctx context.Context, task store.Task, cfg config.Config, media store.MediaFile) (pipeline.ImageResult, error) {
	if r.shouldSkipSucceededStage(ctx, task, "series-images") {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "series images skipped", "already generated by previous attempt")
		return pipeline.ImageResult{}, nil
	}
	return pipeline.GenerateSeriesImagesWithScopeClaim(ctx, cfg, media, r.scanScopeClaim(task.ScanRunID, task.ID))
}

func (r *Runner) shouldSkipSucceededStage(ctx context.Context, task store.Task, stage string) bool {
	if task.Attempts <= 1 {
		return false
	}
	succeeded, err := r.store.HasTaskStageSucceeded(ctx, task.ID, stage)
	if err != nil || !succeeded {
		return false
	}
	artifacts, err := r.store.ListArtifactsByTask(ctx, task.ID)
	if err != nil {
		return false
	}
	for _, artifact := range artifacts {
		if artifactStage(artifact.Type) != stage {
			continue
		}
		if artifact.Path != "" && !fileExists(artifact.Path) {
			return false
		}
	}
	return true
}

func artifactStage(artifactType string) string {
	switch artifactType {
	case "mediainfo":
		return "mediainfo"
	case "bif":
		return "bif"
	case "nfo", "thumb":
		return "nfo"
	case "subtitle":
		return "subtitles"
	case "tvshow-nfo", "season-nfo":
		return "series-nfo"
	case "poster", "fanart", "clearlogo", "clearart", "season-poster":
		return "series-images"
	default:
		return ""
	}
}

func hasFailedImage(images []pipeline.ImageArtifact) bool {
	for _, image := range images {
		if image.Status == "failed" {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r *Runner) addProcessLogs(ctx context.Context, taskID int64, logs []pipeline.ProcessLog) {
	for _, log := range logs {
		level := strings.TrimSpace(log.Level)
		if level == "" {
			level = "info"
		}
		_ = r.store.AddTaskLog(ctx, taskID, level, log.Message, log.Detail)
	}
}

func (r *Runner) scanScopeClaim(scanRunID string, taskID int64) pipeline.SeriesScopeClaimFunc {
	return func(ctx context.Context, scopeType string, scopeKey string) (bool, error) {
		return r.store.ClaimScanScope(ctx, scanRunID, scopeType, scopeKey, taskID)
	}
}

func tmdbLogLevel(status string) string {
	if status == "failed" {
		return "error"
	}
	return "info"
}

func tmdbLogDetail(result pipeline.NFOResult) string {
	if result.TMDBDetail != "" {
		return result.TMDBDetail
	}
	if result.TMDBShowName != "" || result.TMDBEpisode != "" {
		return result.TMDBShowName + " / " + result.TMDBEpisode
	}
	return ""
}
