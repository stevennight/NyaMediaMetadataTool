package renamer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestTargetPathFromTemplatePreservesFilenameSuffixBeforeSourceExtension(t *testing.T) {
	source := `D:\Download\test\episode.mp4`
	target := targetPathFromTemplate(source, `D:\Download\test\Show - S01E01 - Title.cantonese`)
	want := filepath.Clean(`D:\Download\test\Show - S01E01 - Title.cantonese.mp4`)

	if target != want {
		t.Fatalf("targetPathFromTemplate() = %q, want %q", target, want)
	}
}

func TestTargetPathFromTemplateDeduplicatesSourceExtension(t *testing.T) {
	source := `D:\Download\test\episode.mp4`
	target := targetPathFromTemplate(source, `D:\Download\test\Show - S01E01 - Title.mp4`)
	want := filepath.Clean(`D:\Download\test\Show - S01E01 - Title.mp4`)

	if target != want {
		t.Fatalf("targetPathFromTemplate() = %q, want %q", target, want)
	}
}

func TestFinalizeItemReturnsRenderedTargetBeforeSourceDirectoryJoin(t *testing.T) {
	source := filepath.Join(t.TempDir(), "episode.mp4")
	item := PreviewItem{Show: "Show", Season: 1, Episode: 1, Title: "Title"}

	finalizeItem(source, `{show} - S{season:00}E{episode:00} - {title}[cantonese]`, &item)

	want := "Show - S01E01 - Title[cantonese]"
	if item.RenderedTarget != want {
		t.Fatalf("RenderedTarget = %q, want %q", item.RenderedTarget, want)
	}
	if item.NewPath != filepath.Join(filepath.Dir(source), want+".mp4") {
		t.Fatalf("NewPath = %q, want joined source directory", item.NewPath)
	}
}

func TestPreviewSkipsChildOfIgnoredDirectory(t *testing.T) {
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

	result, err := Preview(context.Background(), config.Default(), PreviewRequest{Path: seasonDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(result.Items))
	}
}

func TestParseEpisodeUsesShowDirectoryMetadata(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "K (2012) [tmid-12345]", "Season 1", "S01E01.mkv")
	parsed, ok := parseEpisode(path)
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if parsed.show != "K" || parsed.year != "2012" || parsed.tmdbShowID != 12345 {
		t.Fatalf("unexpected parsed episode: %+v", parsed)
	}
}
