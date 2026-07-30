package mediapath

import "testing"

func TestWindowsPathOperations(t *testing.T) {
	input := `D:\Media\Anime\Show\Season 01\Show - S01E01.mkv`

	if got, want := Base(input), "Show - S01E01.mkv"; got != want {
		t.Fatalf("Base() = %q, want %q", got, want)
	}
	if got, want := Dir(input), `D:\Media\Anime\Show\Season 01`; got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
	if got, want := Ext(input), ".mkv"; got != want {
		t.Fatalf("Ext() = %q, want %q", got, want)
	}
	if got, want := Join(Dir(input), `..\Season 02\episode.mkv`), `D:\Media\Anime\Show\Season 02\episode.mkv`; got != want {
		t.Fatalf("Join() = %q, want %q", got, want)
	}
}

func TestPOSIXPathOperations(t *testing.T) {
	input := "/media/anime/Show/Season 01/Show - S01E01.mkv"

	if got, want := Base(input), "Show - S01E01.mkv"; got != want {
		t.Fatalf("Base() = %q, want %q", got, want)
	}
	if got, want := Dir(input), "/media/anime/Show/Season 01"; got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
	if got, want := Join(Dir(input), "../Season 02/episode.mkv"), "/media/anime/Show/Season 02/episode.mkv"; got != want {
		t.Fatalf("Join() = %q, want %q", got, want)
	}
}
