package pipeline

import "testing"

func TestSubtitleOutputPath(t *testing.T) {
	t.Parallel()

	stream := ffprobeStream{
		Tags: map[string]string{
			"language": "chi",
			"title":    "简体",
		},
		Disposition: ffprobeDisposition{Default: 1},
	}

	got := subtitleOutputPath(`D:\Media\Show S01E01.mkv`, stream, "srt")
	want := `D:\Media\Show S01E01.chi.简体.default.srt`
	if got != want {
		t.Fatalf("unexpected path\nwant: %s\ngot:  %s", want, got)
	}
}

func TestSubtitleStrategySkipsImageSubtitles(t *testing.T) {
	t.Parallel()

	if _, ok := subtitleStrategy("hdmv_pgs_subtitle"); ok {
		t.Fatal("expected image subtitle codec to be skipped")
	}
}
