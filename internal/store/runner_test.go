package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestIgnoreFailedTasksAndRetryIgnored(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()
	videoPath := filepath.Join(t.TempDir(), "Series - S01E01.mkv")
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, videoPath, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, "scan-1"); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FailTask(ctx, task.ID, "tmdb failed"); err != nil {
		t.Fatal(err)
	}

	count, err := st.IgnoreTasks(ctx, []int64{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 ignored task, got %d", count)
	}
	ignored, err := st.ListTasksFiltered(ctx, TaskListFilters{Status: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ignored.Items) != 1 || ignored.Items[0].Status != "ignored" || ignored.Items[0].ErrorSummary != "tmdb failed" {
		t.Fatalf("expected ignored task with original error, got %+v", ignored.Items)
	}

	count, err = st.RetryTasks(ctx, []int64{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 retried task, got %d", count)
	}
	retried, err := st.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != "pending" || retried.ErrorSummary != "" {
		t.Fatalf("expected pending retried task, got %+v", retried)
	}
}

func TestTaskCarriesOneTimeProcessingConfig(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()
	videoPath := filepath.Join(t.TempDir(), "Series - S01E01.mkv")
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, videoPath, info)
	if err != nil {
		t.Fatal(err)
	}
	processing := config.Default().Processing.OutputConfig()
	processing.Strategy = config.ProcessingStrategyForce
	processing.EnableBIF = false
	if err := st.EnqueueMediaTaskWithProcessing(ctx, mediaID, true, true, "scan-1", &processing); err != nil {
		t.Fatal(err)
	}

	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var stored config.OutputProcessingConfig
	if err := json.Unmarshal([]byte(task.ProcessingConfig), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Strategy != config.ProcessingStrategyForce || stored.EnableBIF {
		t.Fatalf("unexpected task processing snapshot: %+v", stored)
	}
}

func TestClaimScanScopeAllowsOwningTaskRetry(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()
	claimed, err := st.ClaimScanScope(ctx, "scan-1", "season-nfo", "show#S01", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("expected first task to claim scope")
	}

	claimed, err = st.ClaimScanScope(ctx, "scan-1", "season-nfo", "show#S01", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("expected owning task retry to reclaim scope")
	}

	claimed, err = st.ClaimScanScope(ctx, "scan-1", "season-nfo", "show#S01", 11)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("expected different task in same scan to skip claimed scope")
	}
}

func TestTaskStageSuccessTracksCompletedStages(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()

	ctx := context.Background()
	videoPath := filepath.Join(t.TempDir(), "Series - S01E01.mkv")
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, videoPath, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, "scan-1"); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}

	done, err := st.HasTaskStageSucceeded(ctx, task.ID, "bif")
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("stage should not start completed")
	}
	if err := st.MarkTaskStageSucceeded(ctx, task.ID, "bif"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTaskStageSucceeded(ctx, task.ID, "bif"); err != nil {
		t.Fatal(err)
	}
	done, err = st.HasTaskStageSucceeded(ctx, task.ID, "bif")
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("stage should be completed")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		st.Close()
		t.Fatal(err)
	}
	return st
}
