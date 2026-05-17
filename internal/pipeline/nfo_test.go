package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestParseEpisodeInfo(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\MAO - S01E03 - Episode.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Season != 1 || info.Episode != 3 {
		t.Fatalf("unexpected season/episode: %+v", info)
	}
	if info.Show != "MAO" {
		t.Fatalf("unexpected show query: %s", info.Show)
	}
}

func TestParseEpisodeInfoSupportsFourDigitEpisodes(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\名探偵コナン - S01E1201 - 第1201話.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Season != 1 || info.Episode != 1201 {
		t.Fatalf("unexpected season/episode: %+v", info)
	}
}

func TestParseEpisodeInfoReadsTMDBIDFromParentDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012) [tmdbid-12345]\Season 1\K - S01E01.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.TMDBShowID != 12345 {
		t.Fatalf("unexpected tmdb show id: %d", info.TMDBShowID)
	}
}

func TestParseEpisodeInfoReadsTMIDFromShowDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012) [tmid-12345]\Season 1\S01E01.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.TMDBShowID != 12345 {
		t.Fatalf("unexpected tmdb show id: %d", info.TMDBShowID)
	}
	if info.Show != "K" {
		t.Fatalf("unexpected show query: %s", info.Show)
	}
}

func TestParseEpisodeInfoIgnoresTMDBIDOutsideShowDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media [tmdbid-999]\K (2012)\Season 1\K - S01E01.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.TMDBShowID != 0 {
		t.Fatalf("unexpected tmdb show id: %d", info.TMDBShowID)
	}
}

func TestParseEpisodeInfoReadsYearFromParentDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012)\Season 1\K - S01E01.mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Year != "2012" {
		t.Fatalf("unexpected year: %s", info.Year)
	}
}

func TestCleanTMDBQueryRemovesDirectoryID(t *testing.T) {
	t.Parallel()

	got := cleanTMDBQuery("K (2012) [tmdbid-12345]")
	if got != "K" {
		t.Fatalf("unexpected cleaned query: %s", got)
	}
}

func TestParseEpisodeInfoRejectsPartialFourDigitEpisode(t *testing.T) {
	t.Parallel()

	if _, ok := parseEpisodeInfo(`D:\Media\名探偵コナン - S01E1201Extra.mkv`, config.Default()); ok {
		t.Fatal("expected episode token without boundary to be ignored")
	}
}

func TestParseEpisodeInfoSupportsNumericEpisodeOnly(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\[Kamigami] Sword Art Online II - 01 [1920x1080 x264 AAC Sub(Chs,Cht,Jap)].mkv`, config.Default())
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Episode != 1 {
		t.Fatalf("unexpected episode: %+v", info)
	}
}

func TestParseEpisodeInfoSupportsCustomRegex(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Processing.EpisodePatterns = []string{`(?i)^.+?-(?P<episode>\d{2})$`}
	info, ok := parseEpisodeInfo(`D:\Media\Anime_Title-12.mkv`, cfg)
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Episode != 12 || info.Season != 1 {
		t.Fatalf("unexpected episode info: %+v", info)
	}
}

func TestParseEpisodeInfoRejectsNonEpisode(t *testing.T) {
	t.Parallel()

	if _, ok := parseEpisodeInfo(`D:\Media\Movie 2024.mkv`, config.Default()); ok {
		t.Fatal("expected movie file to be ignored")
	}
}

func TestSimplifyRate(t *testing.T) {
	t.Parallel()

	got := simplifyRate("24000/1001")
	if !strings.HasPrefix(got, "23.976") {
		t.Fatalf("unexpected frame rate: %s", got)
	}
}

func TestMergeActorsAppendsWithoutDuplicates(t *testing.T) {
	t.Parallel()

	existing := []nfoActor{{Name: "A", Role: "Lead"}}
	incoming := []nfoActor{{Name: "A", Role: "Lead"}, {Name: "B", Role: "Support"}}

	got := mergeActors(existing, incoming, false)
	if len(got) != 2 {
		t.Fatalf("unexpected actor count: %d", len(got))
	}
	if got[0].Name != "A" || got[1].Name != "B" {
		t.Fatalf("unexpected actors: %+v", got)
	}
}

func TestMergeActorsOverwrite(t *testing.T) {
	t.Parallel()

	existing := []nfoActor{{Name: "A", Role: "Old"}}
	incoming := []nfoActor{{Name: "B", Role: "New"}}

	got := mergeActors(existing, incoming, true)
	if len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("unexpected actors after overwrite: %+v", got)
	}
}

func TestApplyTMDBShowAndSeasonImagesSkipsNetworkWhenTargetsExist(t *testing.T) {
	t.Parallel()

	showDir := t.TempDir()
	seasonDir := filepath.Join(showDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}

	result := NFOResult{Path: filepath.Join(seasonDir, "Show - S01E01.nfo")}
	for _, item := range seriesImageArtifacts(showDir, seasonDir, 1) {
		if err := os.WriteFile(item.Path, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.Processing.EnableImageTakeover = true
	cfg.Processing.OverwriteExisting = false
	cfg.Scraping.EnableTMDB = true
	cfg.Scraping.TMDBBaseURL = "http://127.0.0.1:1"
	cfg.Scraping.TMDBAPIKey = "test"
	cfg.Scraping.ImageSources = []string{"tmdb", "fanart"}

	applyTMDBShowAndSeasonImages(t.Context(), cfg, episodeInfo{Season: 1}, &result)

	if len(result.Images) != 5 {
		t.Fatalf("expected 5 skipped images, got %d", len(result.Images))
	}
	for _, image := range result.Images {
		if image.Status != "skipped" {
			t.Fatalf("expected skipped image, got %+v", image)
		}
	}
}

func TestClaimSeriesScopesDeduplicatesShowAndSeasonSeparately(t *testing.T) {
	t.Parallel()

	showDir := t.TempDir()
	season1Path := filepath.Join(showDir, "Season 1", "Show - S01E01.nfo")
	season2Path := filepath.Join(showDir, "Season 2", "Show - S02E01.nfo")
	seen := map[string]struct{}{}
	claim := func(_ context.Context, scopeType string, scopeKey string) (bool, error) {
		key := scopeType + "\x00" + scopeKey
		if _, ok := seen[key]; ok {
			return false, nil
		}
		seen[key] = struct{}{}
		return true, nil
	}

	showOK, seasonOK, err := claimSeriesNFOScope(t.Context(), claim, season1Path, episodeInfo{Season: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !showOK || !seasonOK {
		t.Fatalf("expected first episode to claim show and season, got show=%v season=%v", showOK, seasonOK)
	}

	showOK, seasonOK, err = claimSeriesNFOScope(t.Context(), claim, season1Path, episodeInfo{Season: 1})
	if err != nil {
		t.Fatal(err)
	}
	if showOK || seasonOK {
		t.Fatalf("expected duplicate season to skip both scopes, got show=%v season=%v", showOK, seasonOK)
	}

	showOK, seasonOK, err = claimSeriesNFOScope(t.Context(), claim, season2Path, episodeInfo{Season: 2})
	if err != nil {
		t.Fatal(err)
	}
	if showOK || !seasonOK {
		t.Fatalf("expected new season to claim only season scope, got show=%v season=%v", showOK, seasonOK)
	}
}
