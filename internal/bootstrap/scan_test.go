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
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true}}
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
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true}}
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
	cfg.WatchDirs = []config.WatchDir{{Path: root, Recursive: true, Enabled: true}}
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
