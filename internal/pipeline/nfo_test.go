package pipeline

import (
	"strings"
	"testing"
)

func TestParseEpisodeInfo(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\MAO - S01E03 - Episode.mkv`)
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

	info, ok := parseEpisodeInfo(`D:\Media\名探偵コナン - S01E1201 - 第1201話.mkv`)
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.Season != 1 || info.Episode != 1201 {
		t.Fatalf("unexpected season/episode: %+v", info)
	}
}

func TestParseEpisodeInfoReadsTMDBIDFromParentDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012) [tmdbid-12345]\Season 1\K - S01E01.mkv`)
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.TMDBShowID != 12345 {
		t.Fatalf("unexpected tmdb show id: %d", info.TMDBShowID)
	}
}

func TestParseEpisodeInfoReadsTMIDFromShowDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012) [tmid-12345]\Season 1\S01E01.mkv`)
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

	info, ok := parseEpisodeInfo(`D:\Media [tmdbid-999]\K (2012)\Season 1\K - S01E01.mkv`)
	if !ok {
		t.Fatal("expected episode info to parse")
	}
	if info.TMDBShowID != 0 {
		t.Fatalf("unexpected tmdb show id: %d", info.TMDBShowID)
	}
}

func TestParseEpisodeInfoReadsYearFromParentDirectory(t *testing.T) {
	t.Parallel()

	info, ok := parseEpisodeInfo(`D:\Media\K (2012)\Season 1\K - S01E01.mkv`)
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

	if _, ok := parseEpisodeInfo(`D:\Media\名探偵コナン - S01E1201Extra.mkv`); ok {
		t.Fatal("expected episode token without boundary to be ignored")
	}
}

func TestParseEpisodeInfoRejectsNonEpisode(t *testing.T) {
	t.Parallel()

	if _, ok := parseEpisodeInfo(`D:\Media\Movie 2024.mkv`); ok {
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
