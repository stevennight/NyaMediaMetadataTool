package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/pipeline"
	"NyaMediaMetadataTool/internal/store"
)

type Runner struct {
	cfg    config.Config
	store  *store.Store
	logger *slog.Logger
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, store: st, logger: logger}
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

		if err := r.processTask(ctx, task); err != nil {
			r.logger.Warn("process task failed", "taskID", task.ID, "error", err)
			_ = r.store.AddTaskLog(ctx, task.ID, "error", "task failed", err.Error())
			_ = r.store.FailTask(ctx, task.ID, err.Error())
			continue
		}
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "task completed", "")
		_ = r.store.CompleteTask(ctx, task.ID)
	}
}

func (r *Runner) processTask(ctx context.Context, task store.Task) error {
	if task.MediaFileID == nil {
		return errors.New("task has no media file id")
	}
	media, err := r.store.GetMediaFileByID(ctx, *task.MediaFileID)
	if err != nil {
		return err
	}
	_ = r.store.AddTaskLog(ctx, task.ID, "info", "processing started", media.Path)

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate mediainfo", "")
	mediaInfoPath, err := pipeline.GenerateMediaInfo(ctx, r.cfg, media)
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

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "extract subtitles", "")
	subtitles, err := pipeline.GenerateSubtitles(ctx, r.cfg, media)
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

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate bif", "")
	bifPath, err := pipeline.GenerateBIF(ctx, r.cfg, media)
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

	_ = r.store.AddTaskLog(ctx, task.ID, "info", "generate nfo", "")
	nfoResult, err := pipeline.GenerateNFO(ctx, r.cfg, media)
	if err != nil {
		return err
	}
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
			_ = r.store.AddTaskLog(ctx, task.ID, "info", "tmdb "+nfoResult.TMDBStatus, tmdbLogDetail(nfoResult))
		}
	} else {
		_ = r.store.AddTaskLog(ctx, task.ID, "info", "nfo skipped", "")
	}
	return r.store.TouchMediaProcessed(ctx, media.ID)
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
