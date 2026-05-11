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
