package runner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

type recordingPublisher struct {
	task  store.Task
	media store.MediaFile
	calls int
}

func (p *recordingPublisher) RecordMediaProcessed(_ context.Context, task store.Task, media store.MediaFile) error {
	p.task = task
	p.media = media
	p.calls++
	return nil
}

func TestSuccessfulMetadataProcessingQueuesPublication(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "Example - S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := st.UpsertMediaFile(ctx, mediaPath, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueMediaTaskWithOptions(ctx, mediaID, false, true); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Processing.EnableMediaInfo = false
	cfg.Processing.EnableSubtitles = false
	cfg.Processing.EnableBIF = false
	cfg.Processing.EnableNFO = false
	cfg.Processing.EnableImageTakeover = false
	publisher := &recordingPublisher{}
	runner := New(cfg, st, slog.Default(), publisher)
	if err := runner.processTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || publisher.task.ID != task.ID || publisher.media.ID != mediaID {
		t.Fatalf("unexpected publication handoff: %#v", publisher)
	}
}
