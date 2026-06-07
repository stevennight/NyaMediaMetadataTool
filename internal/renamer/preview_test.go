package renamer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestPreviewWorkerCountRespectsLowConcurrency(t *testing.T) {
	t.Parallel()

	if got := previewWorkerCount(1); got != 1 {
		t.Fatalf("previewWorkerCount(1) = %d, want 1", got)
	}
	if got := previewWorkerCount(9); got != 8 {
		t.Fatalf("previewWorkerCount(9) = %d, want 8", got)
	}
}

func TestPreviewEachProgressReportsTotalBeforeItems(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"Show - S01E01.mkv", "Show - S01E02.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("demo"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	total := -1
	items := 0
	err := PreviewEachProgress(context.Background(), config.Default(), PreviewRequest{Path: root}, func(value int) error {
		if items != 0 {
			t.Fatal("expected total before preview items")
		}
		total = value
		return nil
	}, func(PreviewItem) error {
		items++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || items != 2 {
		t.Fatalf("total=%d items=%d, want 2 and 2", total, items)
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

func TestParseEpisodeSupportsReleaseGroupShowSeasonAndEpisode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "[LoliHouse] Enen no Shouboutai S3 - 13 [WebRip 1080p HEVC-10bit AAC SRTx2].mkv")
	parsed, ok := parseEpisode(path, config.Default())
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if parsed.releaseGroup != "LoliHouse" || parsed.show != "Enen no Shouboutai" || parsed.season != 3 || parsed.episode != 13 {
		t.Fatalf("unexpected parsed episode: %+v", parsed)
	}
}

func TestParseEpisodeRejectsFractionalEpisodeAsNumericFallback(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "[Kamigami] Sword Art Online II - 14.5 [1920x1080 x264 AAC].mkv")
	if _, ok := parseEpisode(path, config.Default()); ok {
		t.Fatal("expected fractional episode to be ignored")
	}
}

func TestParseEpisodeKeepsReleaseGroupWhenEpisodeDoesNotParse(t *testing.T) {
	t.Parallel()

	path := filepath.Join(`D:\Media`, "[Kamigami] Sword Art Online II - 14.5 [1920x1080 x264 AAC].mkv")
	parsed, ok := parseEpisode(path, config.Default())
	if ok {
		t.Fatal("expected fractional episode to be ignored")
	}
	if parsed.releaseGroup != "Kamigami" {
		t.Fatalf("releaseGroup = %q, want Kamigami", parsed.releaseGroup)
	}
}

func TestIdentifyEpisodePrefersNFOIdentityOverFilename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seasonDir := filepath.Join(root, "Wrong Show", "Season 1")
	path := filepath.Join(seasonDir, "Wrong Show - S01E02.mkv")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<episodedetails><showtitle>Right Show</showtitle><season>3</season><episode>7</episode><uniqueid type="tmdb">12345</uniqueid></episodedetails>`
	if err := os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".nfo", []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, ok, source, _ := identifyEpisode(path, config.Default(), nil)
	if !ok {
		t.Fatal("expected NFO identity to be recognized")
	}
	if source != "nfo" || parsed.show != "Right Show" || parsed.season != 3 || parsed.episode != 7 || parsed.tmdbShowID != 12345 {
		t.Fatalf("unexpected NFO identity: source=%q parsed=%+v", source, parsed)
	}
}

func TestIdentifyEpisodeUsesTVShowNFOForSeriesIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seasonDir := filepath.Join(root, "Unknown", "Season 1")
	path := filepath.Join(seasonDir, "S01E04.mkv")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Unknown", "tvshow.nfo"), []byte(`<tvshow><title>Right Show</title><year>2024</year><tmdbid>67890</tmdbid></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, ok, source, _ := identifyEpisode(path, config.Default(), nil)
	if !ok {
		t.Fatal("expected filename episode identity to be recognized")
	}
	if source != "nfo" || parsed.show != "Right Show" || parsed.year != "2024" || parsed.tmdbShowID != 67890 {
		t.Fatalf("unexpected tvshow NFO identity: source=%q parsed=%+v", source, parsed)
	}
}

func TestIdentifyEpisodeUsesPatternBeforeBuiltInFilenameValues(t *testing.T) {
	t.Parallel()

	pattern, err := compileMatchPattern(`^\[(?P<group>[^\]]+)\]\s*(?P<show>.+?)\s*-\s*(?P<episode>\d+)`)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "[Custom] Right Show - 07.mkv")
	parsed, ok, source, variables := identifyEpisode(path, config.Default(), pattern)
	if !ok {
		t.Fatal("expected pattern episode to be recognized")
	}
	if source != "pattern" || parsed.show != "Right Show" || parsed.episode != 7 || variables["group"] != "Custom" {
		t.Fatalf("unexpected pattern identity: source=%q parsed=%+v variables=%v", source, parsed, variables)
	}
}

func TestApplyTemplateSupportsCustomVariables(t *testing.T) {
	t.Parallel()

	item := PreviewItem{Show: "Show", Season: 1, Episode: 2, Variables: map[string]string{"group": "Custom"}}
	got := applyTemplate("[{group}] {show} - S{season:00}E{episode:00}", item)
	if got != "[Custom] Show - S01E02" {
		t.Fatalf("unexpected custom variable template: %q", got)
	}
}

func TestApplyTemplateSupportsConditionalSegments(t *testing.T) {
	t.Parallel()

	template := "{show}{if:releaseGroup| - {releaseGroup}| - 未知字幕组}"
	withGroup := applyTemplate(template, PreviewItem{Show: "Show", ReleaseGroup: "Group"})
	if withGroup != "Show - Group" {
		t.Fatalf("conditional with value = %q", withGroup)
	}
	withoutGroup := applyTemplate(template, PreviewItem{Show: "Show"})
	if withoutGroup != "Show - 未知字幕组" {
		t.Fatalf("conditional else = %q", withoutGroup)
	}
}

func TestApplyTemplateConditionalSegmentSupportsEmptyElseAndCustomVariable(t *testing.T) {
	t.Parallel()

	template := "{show}{if:edition| [{edition}]|}"
	withEdition := applyTemplate(template, PreviewItem{Show: "Show", Variables: map[string]string{"edition": "Director"}})
	if withEdition != "Show [Director]" {
		t.Fatalf("custom conditional with value = %q", withEdition)
	}
	withoutEdition := applyTemplate(template, PreviewItem{Show: "Show"})
	if withoutEdition != "Show" {
		t.Fatalf("custom conditional empty else = %q", withoutEdition)
	}
}

func TestCompileMatchPatternRejectsInvalidRE2(t *testing.T) {
	t.Parallel()

	if _, err := compileMatchPattern(`(?<=Show)\d+`); err == nil {
		t.Fatal("expected unsupported lookbehind to fail")
	}
}

func TestBuildPreviewDoesNotUseNFOTitleAsMetadataFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "Show - S01E01 - Filename Title.mkv")
	if err := os.WriteFile(path, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<episodedetails><title>NFO Title</title><showtitle>Show</showtitle><season>1</season><episode>1</episode></episodedetails>`
	if err := os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".nfo", []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}

	item := buildPreviewItem(context.Background(), config.Default(), nil, path, DefaultTemplate, nil, previewOverrides{})
	if item.Title == "NFO Title" {
		t.Fatal("expected NFO title not to be used as metadata fallback")
	}
	if item.MetadataSource != "tmdb-unavailable" || item.Status != "warning" {
		t.Fatalf("unexpected metadata status: %+v", item)
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

func TestPreviewSingleKeepsInputReleaseGroup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Episode 14.5.mkv")
	item, err := PreviewSingle(context.Background(), config.Default(), PreviewItemRequest{
		Path:         path,
		Template:     "{show} - S{season:00}E{episode:00} - {title} - {releaseGroup}",
		Show:         "Show",
		Title:        "Title",
		ReleaseGroup: "Kamigami",
		Season:       &inputInt{Value: 0},
		Episode:      &inputInt{Value: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ReleaseGroup != "Kamigami" {
		t.Fatalf("ReleaseGroup = %q, want Kamigami", item.ReleaseGroup)
	}
	if item.NewName != "Show - S00E12 - Title - Kamigami.mkv" {
		t.Fatalf("NewName = %q, want release group in rendered name", item.NewName)
	}
}

func TestPreviewUsesInputReleaseGroupOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "[Original] Show - 01.mkv")
	if err := os.WriteFile(path, []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Preview(context.Background(), config.Default(), PreviewRequest{
		Path:         path,
		Template:     "{show} - S{season:00}E{episode:00} - {releaseGroup}",
		ReleaseGroup: "Override",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	if result.Items[0].ReleaseGroup != "Override" {
		t.Fatalf("ReleaseGroup = %q, want Override", result.Items[0].ReleaseGroup)
	}
	if result.Items[0].NewName != "Show - S01E01 - Override.mkv" {
		t.Fatalf("NewName = %q, want release group override in rendered name", result.Items[0].NewName)
	}
}

func TestPreviewSingleKeepsAbsoluteManualTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "Episode 01.mkv")
	manualTarget := filepath.Join(root, "Show", "Season 1", "Show - S01E01 - Title")
	item, err := PreviewSingle(context.Background(), config.Default(), PreviewItemRequest{
		Path:    path,
		NewName: manualTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.RenderedTarget != manualTarget {
		t.Fatalf("RenderedTarget = %q, want %q", item.RenderedTarget, manualTarget)
	}
	if item.NewPath != manualTarget+".mkv" {
		t.Fatalf("NewPath = %q, want %q", item.NewPath, manualTarget+".mkv")
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
