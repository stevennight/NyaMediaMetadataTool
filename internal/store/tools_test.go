package store

import (
	"context"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/tools"
)

func TestSaveToolStatusesReplacesRemovedTools(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := st.SaveToolStatuses(ctx, []tools.Status{
		{Name: "ffmpeg"},
		{Name: "ffprobe"},
		{Name: "mkvextract"},
		{Name: "mediainfo"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveToolStatuses(ctx, []tools.Status{
		{Name: "ffmpeg"},
		{Name: "ffprobe"},
		{Name: "mediainfo"},
	}); err != nil {
		t.Fatal(err)
	}

	statuses, err := st.ListToolStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 {
		t.Fatalf("got %d tool statuses, want 3: %#v", len(statuses), statuses)
	}
	for _, status := range statuses {
		if status.Name == "mkvextract" {
			t.Fatal("removed mkvextract status was retained")
		}
	}
}
