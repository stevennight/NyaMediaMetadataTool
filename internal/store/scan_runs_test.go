package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanRunSummaryWaitsForSealAndAllTasks(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()
	ctx := context.Background()

	if err := st.BeginScanRun(ctx, "scan-1", "manual", `D:\Media\Series`); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("episode-%d.mkv", index))
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mediaID, err := st.UpsertMediaFile(ctx, path, info)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, "scan-1"); err != nil {
			t.Fatal(err)
		}
	}

	run := requireScanRun(t, st, "scan-1")
	if run.Status != ScanRunStatusCollecting || run.Total != 3 || run.Active != 3 {
		t.Fatalf("unexpected collecting run: %+v", run)
	}

	completed, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteTask(ctx, completed.ID); err != nil {
		t.Fatal(err)
	}
	failed, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FailTask(ctx, failed.ID, "metadata lookup failed"); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(ctx, "scan-1", ""); err != nil {
		t.Fatal(err)
	}

	run = requireScanRun(t, st, "scan-1")
	if run.Status != ScanRunStatusRunning || run.Completed != 1 || run.Failed != 1 || run.Active != 1 {
		t.Fatalf("sealed run should remain active: %+v", run)
	}

	last, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteTask(ctx, last.ID); err != nil {
		t.Fatal(err)
	}

	run = requireScanRun(t, st, "scan-1")
	if run.Status != ScanRunStatusFailed || run.Completed != 2 || run.Failed != 1 || run.Active != 0 {
		t.Fatalf("unexpected terminal run: %+v", run)
	}
}

func TestScanRunSummaryDistinguishesEmptyAndScanFailure(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()
	ctx := context.Background()

	if err := st.BeginScanRun(ctx, "empty", "manual", "empty path"); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(ctx, "empty", ""); err != nil {
		t.Fatal(err)
	}
	if run := requireScanRun(t, st, "empty"); run.Status != ScanRunStatusEmpty {
		t.Fatalf("empty run status = %q", run.Status)
	}

	if err := st.BeginScanRun(ctx, "broken", "manual", "broken path"); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(ctx, "broken", "scan failed"); err != nil {
		t.Fatal(err)
	}
	if run := requireScanRun(t, st, "broken"); run.Status != ScanRunStatusFailed || run.ErrorSummary != "scan failed" {
		t.Fatalf("unexpected failed scan run: %+v", run)
	}
}

func TestMigrateSealsInterruptedScanRun(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()
	ctx := context.Background()
	if err := st.BeginScanRun(ctx, "interrupted", "manual", "media"); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	run := requireScanRun(t, st, "interrupted")
	if run.Status != ScanRunStatusFailed || run.ScanFinishedAt == "" || run.ErrorSummary == "" {
		t.Fatalf("interrupted run was not recovered: %+v", run)
	}
}

func TestRecentlyCompletedOldScanRunReturnsToLimitedSummary(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()
	ctx := context.Background()
	if err := st.BeginScanRun(ctx, "old-active", "manual", "old media"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, path, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, "old-active"); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(ctx, "old-active", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE scan_runs SET created_at = '2020-01-01 00:00:00', sealed_at = '2020-01-01 00:00:00' WHERE id = 'old-active'`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE tasks SET created_at = '2020-01-01 00:00:00', updated_at = '2020-01-01 00:00:00' WHERE scan_run_id = 'old-active'`); err != nil {
		t.Fatal(err)
	}

	for index, timestamp := range []string{"2024-01-01 00:00:00", "2025-01-01 00:00:00", "2026-01-01 00:00:00"} {
		id := fmt.Sprintf("recent-%d", index)
		if err := st.BeginScanRun(ctx, id, "manual", "recent media"); err != nil {
			t.Fatal(err)
		}
		if err := st.FinishScanRun(ctx, id, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `UPDATE scan_runs SET created_at = ?, sealed_at = ? WHERE id = ?`, timestamp, timestamp, id); err != nil {
			t.Fatal(err)
		}
	}

	runs, err := st.ListScanRunSummaries(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if scanRunSummaryContains(runs, "old-active") {
		t.Fatalf("old run unexpectedly present before completion: %+v", runs)
	}
	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}

	runs, err = st.ListScanRunSummaries(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 || runs[0].ID != "old-active" || runs[0].Status != ScanRunStatusCompleted {
		t.Fatalf("recently completed old run did not return to summaries: %+v", runs)
	}
}

func TestRecentlySealedOldScanRunReturnsToLimitedSummary(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	defer st.Close()
	ctx := context.Background()
	if err := st.BeginScanRun(ctx, "old-collecting", "manual", "old media"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, path, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, "old-collecting"); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE scan_runs SET created_at = '2020-01-01 00:00:00' WHERE id = 'old-collecting'`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE tasks SET created_at = '2020-01-01 00:00:00', updated_at = '2020-01-01 00:00:00' WHERE scan_run_id = 'old-collecting'`); err != nil {
		t.Fatal(err)
	}
	for index, timestamp := range []string{"2024-01-01 00:00:00", "2025-01-01 00:00:00", "2026-01-01 00:00:00"} {
		id := fmt.Sprintf("sealed-recent-%d", index)
		if err := st.BeginScanRun(ctx, id, "manual", "recent media"); err != nil {
			t.Fatal(err)
		}
		if err := st.FinishScanRun(ctx, id, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `UPDATE scan_runs SET created_at = ?, sealed_at = ? WHERE id = ?`, timestamp, timestamp, id); err != nil {
			t.Fatal(err)
		}
	}

	runs, err := st.ListScanRunSummaries(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if scanRunSummaryContains(runs, "old-collecting") {
		t.Fatalf("old collecting run unexpectedly present before sealing: %+v", runs)
	}
	if err := st.FinishScanRun(ctx, "old-collecting", ""); err != nil {
		t.Fatal(err)
	}
	runs, err = st.ListScanRunSummaries(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 || runs[0].ID != "old-collecting" || runs[0].Status != ScanRunStatusCompleted {
		t.Fatalf("recently sealed old run did not return to summaries: %+v", runs)
	}
}

func scanRunSummaryContains(runs []ScanRunSummary, id string) bool {
	for _, run := range runs {
		if run.ID == id {
			return true
		}
	}
	return false
}

func requireScanRun(t *testing.T, st *Store, id string) ScanRunSummary {
	t.Helper()
	runs, err := st.ListScanRunSummaries(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.ID == id {
			return run
		}
	}
	t.Fatalf("scan run %q not found in %+v", id, runs)
	return ScanRunSummary{}
}
