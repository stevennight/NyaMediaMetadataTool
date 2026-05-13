package renamer

import (
	"path/filepath"
	"testing"
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
