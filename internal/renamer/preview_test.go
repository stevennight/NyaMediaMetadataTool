package renamer

import (
	"context"
	"encoding/json"
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
	parsed, ok := parseEpisode(path, config.Default())
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if parsed.show != "K" || parsed.year != "2012" || parsed.tmdbShowID != 12345 {
		t.Fatalf("unexpected parsed episode: %+v", parsed)
	}
}

func TestParseEpisodeSupportsNumericEpisodeOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "[Kamigami] Sword Art Online II - 01 [1920x1080 x264 AAC].mkv")
	parsed, ok := parseEpisode(path, config.Default())
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if parsed.episode != 1 {
		t.Fatalf("unexpected episode: %+v", parsed)
	}
	if parsed.show != "Sword Art Online II" {
		t.Fatalf("unexpected show: %q", parsed.show)
	}
	if parsed.releaseGroup != "Kamigami" {
		t.Fatalf("unexpected release group: %q", parsed.releaseGroup)
	}
}

func TestParseEpisodeRejectsFractionalEpisodeAsNumericFallback(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "[Kamigami] Sword Art Online II - 14.5 [1920x1080 x264 AAC].mkv")
	if _, ok := parseEpisode(path, config.Default()); ok {
		t.Fatal("expected fractional episode to be ignored")
	}
}

func TestPreviewItemRequestAcceptsFractionalEpisodeInput(t *testing.T) {
	t.Parallel()

	var input PreviewItemRequest
	if err := json.Unmarshal([]byte(`{"episode":14.5}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.Episode == nil || !input.Episode.Fractional || input.Episode.Value != 14 {
		t.Fatalf("unexpected episode input: %+v", input.Episode)
	}
}

func TestApplyTemplateSupportsReleaseGroup(t *testing.T) {
	t.Parallel()

	item := PreviewItem{Show: "刀剑神域", ShowOriginal: "ソードアート・オンライン", ReleaseGroup: "Kamigami", TMDBShowID: 45782, Season: 1, Episode: 1, Title: "Episode 1"}
	got := applyTemplate("[{releaseGroup}] {show} / {showOriginal} - {tmid} - S{season:00}E{episode:00} - {title}", item)
	want := "[Kamigami] 刀剑神域 / ソードアート・オンライン - 45782 - S01E01 - Episode 1"
	if got != want {
		t.Fatalf("applyTemplate() = %q, want %q", got, want)
	}
}

func TestApplyTemplateSupportsLocalizedShowAndTitle(t *testing.T) {
	t.Parallel()

	item := PreviewItem{
		Show:    "ソードアート・オンライン",
		Title:   "剣の世界",
		Season:  1,
		Episode: 1,
		showByLanguage: map[string]string{
			"zh-cn": "刀剑神域",
			"ja-jp": "ソードアート・オンライン",
		},
		titleByLanguage: map[string]string{
			"zh-cn": "剑的世界",
			"ja-jp": "剣の世界",
		},
	}
	got := applyTemplate("{show:zh-CN} {show:ja-JP} - {title:zh-CN} / {title:ja-JP}", item)
	want := "刀剑神域 ソードアート・オンライン - 剑的世界 / 剣の世界"
	if got != want {
		t.Fatalf("applyTemplate() = %q, want %q", got, want)
	}
}

func TestParseEpisodeSupportsCustomRegex(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Processing.EpisodePatterns = []string{`(?i)^.+?-(?P<episode>\d{2})$`}
	path := filepath.Join(`D:\Media`, "Anime_Title-12.mkv")
	parsed, ok := parseEpisode(path, cfg)
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if parsed.episode != 12 || parsed.season != 1 {
		t.Fatalf("unexpected parsed episode: %+v", parsed)
	}
}
