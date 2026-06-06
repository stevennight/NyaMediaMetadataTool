package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func TestSyncAndScanEnqueuesStableMediaFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	videoPath := filepath.Join(root, "Series", "Episode01.mkv")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(videoPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true, ScanOnStart: true}}
	cfg.Processing.StableDelay = time.Second

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := SyncAndScan(context.Background(), cfg, st, logger); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != "pending" {
		t.Fatalf("expected pending task, got %q", tasks[0].Status)
	}
}

func TestSyncAndScanSkipsUnstableMediaFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	videoPath := filepath.Join(root, "Episode02.mkv")
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true, ScanOnStart: true}}
	cfg.Processing.StableDelay = time.Hour

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := SyncAndScan(context.Background(), cfg, st, logger); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestSyncAndScanSkipsIgnoredDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ignoredVideoPath := filepath.Join(root, "Series", "Extras", "Episode03.mkv")
	visibleVideoPath := filepath.Join(root, "Episode04.mkv")
	if err := os.MkdirAll(filepath.Dir(ignoredVideoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Series", ".ignore"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoredVideoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(visibleVideoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(ignoredVideoPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(visibleVideoPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true, ScanOnStart: true}}
	cfg.Processing.StableDelay = time.Second

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := SyncAndScan(context.Background(), cfg, st, logger); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestScanPathSkipsChildOfIgnoredDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seasonDir := filepath.Join(root, "Series", "Season 1")
	videoPath := filepath.Join(seasonDir, "Series - S01E01.mkv")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Series", ".ignore"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := ScanPath(context.Background(), cfg, st, logger, seasonDir, ScanOptions{}); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestScanPathMissingOnlyEnqueuesProcessedMedia(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	videoPath := filepath.Join(root, "Series - S01E01.mkv")
	if err := os.WriteFile(videoPath, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(videoPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	mediaFileID, err := st.UpsertMediaFile(context.Background(), videoPath, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchMediaProcessed(context.Background(), mediaFileID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := ScanPath(context.Background(), cfg, st, logger, videoPath, ScanOptions{MissingOnly: true}); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].OverwriteExisting {
		t.Fatal("missing-only task should not overwrite existing outputs")
	}
}

func TestScanWatchDirAssignsOneScanRunToDirectoryTasks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "Series", "Series - S01E01.mkv"),
		filepath.Join(root, "Series", "Series - S01E02.mkv"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("demo"), 0o644); err != nil {
			t.Fatal(err)
		}
		oldTime := time.Now().Add(-2 * time.Minute)
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Processing.StableDelay = time.Second
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := ScanWatchDir(context.Background(), cfg, st, logger, config.WatchDir{Path: root, Recursive: true, Enabled: true, ScanOnStart: true}, ScanOptions{OverwriteExisting: true}); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ScanRunID == "" || tasks[1].ScanRunID == "" {
		t.Fatal("expected scan run id on all tasks")
	}
	if tasks[0].ScanRunID != tasks[1].ScanRunID {
		t.Fatalf("expected same scan run id, got %q and %q", tasks[0].ScanRunID, tasks[1].ScanRunID)
	}
	if !tasks[0].OverwriteExisting || !tasks[1].OverwriteExisting {
		t.Fatal("expected overwrite strategy to be preserved on file tasks")
	}
}

func TestScanOptionsFromStrategy(t *testing.T) {
	t.Parallel()

	missing := ScanOptionsFromStrategy(config.ProcessingStrategyMissing)
	if missing.OverwriteExisting || !missing.MissingOnly || missing.Force {
		t.Fatalf("unexpected missing strategy options: %+v", missing)
	}

	force := ScanOptionsFromStrategy(config.ProcessingStrategyForce)
	if !force.OverwriteExisting || !force.Force || force.MissingOnly {
		t.Fatalf("unexpected force strategy options: %+v", force)
	}
}
