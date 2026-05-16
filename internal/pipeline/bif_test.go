package pipeline

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestBIFOutputPath(t *testing.T) {
	t.Parallel()

	got := bifOutputPath(`D:\Media\Show S01E01.mkv`, 320, 10)
	want := `D:\Media\Show S01E01-320-10.bif`
	if got != want {
		t.Fatalf("unexpected path\nwant: %s\ngot:  %s", want, got)
	}
}

func TestWriteBIF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	frame1 := filepath.Join(dir, "frame-000001.jpg")
	frame2 := filepath.Join(dir, "frame-000002.jpg")
	if err := os.WriteFile(frame1, []byte{0xff, 0xd8, 0xff, 0xd9}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frame2, []byte{0xff, 0xd8, 0x00, 0xd9}, 0o644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(dir, "sample.bif")
	if err := writeBIF(output, 10, []string{frame1, frame2}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 64 {
		t.Fatalf("bif too small: %d", len(content))
	}
	if string(content[:8]) != string(bifMagic) {
		t.Fatal("unexpected bif magic")
	}
	count := binary.LittleEndian.Uint32(content[12:16])
	if count != 2 {
		t.Fatalf("unexpected image count: %d", count)
	}
	interval := binary.LittleEndian.Uint32(content[16:20])
	if interval != 10000 {
		t.Fatalf("unexpected interval: %d", interval)
	}
	firstTimestamp := binary.LittleEndian.Uint32(content[64:68])
	if firstTimestamp != 0 {
		t.Fatalf("unexpected first timestamp: %d", firstTimestamp)
	}
	secondTimestamp := binary.LittleEndian.Uint32(content[72:76])
	if secondTimestamp != 1 {
		t.Fatalf("unexpected second timestamp: %d", secondTimestamp)
	}
}

func TestBIFHWAccelAttempts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want string
	}{
		{mode: "cpu", want: "cpu"},
		{mode: "none", want: "cpu"},
		{mode: "nvidia", want: "cuda"},
		{mode: "cuda", want: "cuda"},
		{mode: "intel", want: "qsv"},
		{mode: "qsv", want: "qsv"},
		{mode: "d3d11va", want: "d3d11va"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.mode, func(t *testing.T) {
			t.Parallel()
			attempts := bifHWAccelAttempts(test.mode)
			if len(attempts) == 0 {
				t.Fatal("expected at least one attempt")
			}
			if attempts[0].name != test.want {
				t.Fatalf("unexpected first attempt: want %s got %s", test.want, attempts[0].name)
			}
		})
	}
}

func TestBIFFFmpegArgsIncludesHWAccelBeforeInput(t *testing.T) {
	t.Parallel()

	args := bifFFmpegArgs("input.mkv", "frame-%06d.jpg", 10, 320, hwBIFAttempt("d3d11va"))
	want := []string{"-y", "-hwaccel", "d3d11va", "-i", "input.mkv"}
	for idx, value := range want {
		if args[idx] != value {
			t.Fatalf("unexpected arg at %d: want %s got %s", idx, value, args[idx])
		}
	}
}
