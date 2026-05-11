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
}
