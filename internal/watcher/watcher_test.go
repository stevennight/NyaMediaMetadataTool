package watcher

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func TestDebouncedFilesShareWatcherScanRun(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "watcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Processing.StableDelay = 10 * time.Millisecond
	cfg.Processing.StableChecks = 1
	w := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer w.stopAsync()

	mediaDir := t.TempDir()
	paths := []string{
		filepath.Join(mediaDir, "Series - S01E01.mkv"),
		filepath.Join(mediaDir, "Series - S01E02.mkv"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		w.debounceFile(context.Background(), path)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := st.ListScanRunSummaries(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].Source == "watcher" && runs[0].Total == 2 && runs[0].ScanFinishedAt != "" {
			if runs[0].Status != store.ScanRunStatusRunning || runs[0].Active != 2 {
				t.Fatalf("unexpected watcher run: %+v", runs[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher batch did not seal with both files")
}
