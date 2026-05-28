package episodeparse

import "testing"

func TestParseSeasonSeparatedEpisode(t *testing.T) {
	t.Parallel()

	result, ok := Parse("[LoliHouse] Enen no Shouboutai S3 - 13 [WebRip 1080p HEVC-10bit AAC SRTx2]")
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if result.Season != 3 || result.Episode != 13 {
		t.Fatalf("unexpected season/episode: %+v", result)
	}
	if result.Token != " S3 - 13 " {
		t.Fatalf("unexpected token: %q", result.Token)
	}
}

func TestParseSeasonSeparatedEpisodeWithUnderscores(t *testing.T) {
	t.Parallel()

	result, ok := Parse("[LoliHouse]_Enen_no_Shouboutai_S3_13_[WebRip_1080p_HEVC-10bit_AAC_SRTx2]")
	if !ok {
		t.Fatal("expected episode to parse")
	}
	if result.Season != 3 || result.Episode != 13 {
		t.Fatalf("unexpected season/episode: %+v", result)
	}
}

func TestParseRejectsEpisodeImmediatelyFollowedByP(t *testing.T) {
	t.Parallel()

	if _, ok := Parse("Show S3 - 1080p"); ok {
		t.Fatal("expected resolution-like token to be ignored")
	}
}

func TestParseKeepsFourDigitEpisodeNotFollowedByP(t *testing.T) {
	t.Parallel()

	result, ok := Parse("Show - S01E1201")
	if !ok {
		t.Fatal("expected four digit episode to parse")
	}
	if result.Season != 1 || result.Episode != 1201 {
		t.Fatalf("unexpected season/episode: %+v", result)
	}
}
