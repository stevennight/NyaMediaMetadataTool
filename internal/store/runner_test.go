package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
