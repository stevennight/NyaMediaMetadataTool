package metadataaudit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestRunUsesSeasonNFOAndSkipsSeasonZero(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seasonDir := filepath.Join(root, "Season 01")
	if err := os.Mkdir(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "tvshow.nfo"), `<tvshow><title>Test Show</title><uniqueid type="tmdb">123</uniqueid></tvshow>`)
	writeFile(t, filepath.Join(seasonDir, "season.nfo"), `<season><episodeguide><episodecount>3</episodecount></episodeguide></season>`)
	writeFile(t, filepath.Join(seasonDir, "Test Show S01E01.mkv"), "")
	writeFile(t, filepath.Join(seasonDir, "Test Show S01E03.mkv"), "")
	writeFile(t, filepath.Join(seasonDir, "Test Show S00E01.mkv"), "")

	report, err := Run(context.Background(), Options{Root: root, Config: config.Default()})
	if err != nil {
		t.Fatal(err)
	}
	if report.TMDBShowID != 123 || report.ShowTitle != "Test Show" {
		t.Fatalf("unexpected show metadata: %#v", report)
	}
	if len(report.LocalEpisodes) != 2 {
		t.Fatalf("expected 2 non-special episodes, got %d", len(report.LocalEpisodes))
	}
	if len(report.SeasonReports) != 1 {
		t.Fatalf("expected 1 season report, got %d", len(report.SeasonReports))
	}
	season := report.SeasonReports[0]
	if season.ExpectedCount != 3 || len(season.MissingEpisodes) != 1 || season.MissingEpisodes[0] != 2 {
		t.Fatalf("unexpected season report: %#v", season)
	}
}

func TestCompareEmbyDetectsMetadataDifferences(t *testing.T) {
	t.Parallel()

	issues := compareEmby(LocalShow{}, nil, []LocalEpisode{{
		Season:      1,
		Episode:     1,
		Path:        `D:\TV\Show S01E01.mkv`,
		Title:       "Local Title",
		Plot:        "Local plot",
		HasImage:    true,
		ProviderIDs: map[string]string{"tmdb": "11"},
	}}, embyEpisode{}, nil, []embyEpisode{{
		Name:              "Emby Title",
		Path:              `/media/Show S01E01.mkv`,
		Overview:          "Emby plot",
		IndexNumber:       1,
		ParentIndexNumber: 1,
		ProviderIDs:       map[string]string{"Tmdb": "22"},
	}})

	fields := map[string]bool{}
	for _, issue := range issues {
		fields[issue.Field] = true
	}
	for _, field := range []string{"title", "plot", "tmdb", "image"} {
		if !fields[field] {
			t.Fatalf("missing issue %q in %#v", field, issues)
		}
	}
}

func TestCompareEmbyAllowsSTRMProxyExtension(t *testing.T) {
	t.Parallel()

	issues := compareEmby(LocalShow{}, nil, []LocalEpisode{{
		Season:  1,
		Episode: 1,
		Path:    `D:\TV\ナルト - S01E01 - 参上！うずまきナルト.mkv`,
	}}, embyEpisode{}, nil, []embyEpisode{{
		Name:              "参上！うずまきナルト",
		Path:              `/remote/ナルト - S01E01 - 参上！うずまきナルト.strm`,
		IndexNumber:       1,
		ParentIndexNumber: 1,
	}})
	for _, issue := range issues {
		if issue.Field == "file" {
			t.Fatalf("did not expect file issue for strm proxy: %#v", issues)
		}
	}
}

func TestApplyMissingEpisodesDoesNotAssumeOneBasedWhenCountOnlyMatches(t *testing.T) {
	t.Parallel()

	existing := map[int]bool{}
	for episode := 53; episode <= 104; episode++ {
		existing[episode] = true
	}
	report := SeasonReport{Season: 2, ExpectedCount: 52, ExpectedSource: "season.nfo", ExistingCount: 52, ExistingEpisodes: setToSortedInts(existing)}
	applyMissingEpisodes(&report, existing)
	if len(report.MissingEpisodes) != 0 {
		t.Fatalf("unexpected missing episodes: %#v", report.MissingEpisodes)
	}
	if report.Note == "" {
		t.Fatalf("expected explanatory note")
	}
}

func TestApplyMissingEpisodesUsesExactTMDBNumbers(t *testing.T) {
	t.Parallel()

	existing := map[int]bool{53: true, 55: true}
	report := SeasonReport{Season: 2, ExpectedCount: 3, ExpectedSource: "tmdb", ExpectedEpisodes: []int{53, 54, 55}, ExistingCount: 2, ExistingEpisodes: setToSortedInts(existing)}
	applyMissingEpisodes(&report, existing)
	if len(report.MissingEpisodes) != 1 || report.MissingEpisodes[0] != 54 {
		t.Fatalf("unexpected missing episodes: %#v", report.MissingEpisodes)
	}
}

func TestParseEmbyItemURLFromWebHashRoute(t *testing.T) {
	t.Parallel()

	baseURL, itemID, err := ParseEmbyItemURL("https://emby.nyatori.com/proxy/remote/web/index.html#!/item?id=662&serverId=4600c6480cd142b488dfa2af027aa8cc&context=tvshows")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "https://emby.nyatori.com/proxy/remote" {
		t.Fatalf("unexpected base URL: %q", baseURL)
	}
	if itemID != "662" {
		t.Fatalf("unexpected item id: %q", itemID)
	}
}

func TestParseEmbyItemURLFromQuery(t *testing.T) {
	t.Parallel()

	baseURL, itemID, err := ParseEmbyItemURL("https://emby.example.com/web/index.html?id=abc")
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "https://emby.example.com" {
		t.Fatalf("unexpected base URL: %q", baseURL)
	}
	if itemID != "abc" {
		t.Fatalf("unexpected item id: %q", itemID)
	}
}

func TestEmbyBaseCandidatesIncludeAPIPrefixFallback(t *testing.T) {
	t.Parallel()

	candidates := embyBaseCandidates("https://emby.nyatori.com/proxy/remote")
	want := []string{"https://emby.nyatori.com/proxy/remote", "https://emby.nyatori.com/proxy/remote/emby"}
	if len(candidates) != len(want) {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Fatalf("unexpected candidates: %#v", candidates)
		}
	}
}

func TestEmbyRequestURLUsesProvidedBase(t *testing.T) {
	t.Parallel()

	requestURL, err := embyRequestURL("https://emby.example.com/emby", "/Items/662", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := requestURL.String(); got != "https://emby.example.com/emby/Items/662?api_key=key" {
		t.Fatalf("unexpected request URL: %q", got)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
