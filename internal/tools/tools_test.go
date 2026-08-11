package tools

import "testing"

func TestIsCurrentStatusSet(t *testing.T) {
	current := []Status{
		{Name: "ffmpeg"},
		{Name: "ffprobe"},
		{Name: "mediainfo"},
	}
	if !IsCurrentStatusSet(current) {
		t.Fatal("expected current tool statuses to be accepted")
	}

	stale := append(append([]Status(nil), current[:2]...), Status{Name: "mkvextract"})
	if IsCurrentStatusSet(stale) {
		t.Fatal("expected removed tool status to be rejected")
	}
}
