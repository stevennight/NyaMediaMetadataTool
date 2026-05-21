package fileaudit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareDetectsMissingExtraAndSizeMismatch(t *testing.T) {
	t.Parallel()

	localRoot := t.TempDir()
	remoteRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "Season 01", "Show S01E01.mkv"), "local")
	writeFile(t, filepath.Join(localRoot, "Season 01", "Show S01E02.mkv"), "same")
	writeFile(t, filepath.Join(remoteRoot, "Season 01", "Show S01E01.mkv"), "remote larger")
	writeFile(t, filepath.Join(remoteRoot, "Season 01", "Show S01E03.mkv"), "extra")

	report, err := Compare(context.Background(), Options{LocalRoot: localRoot, RemoteRoot: remoteRoot, RemoteFS: LocalFS{}, CompareSize: true})
	if err != nil {
		t.Fatal(err)
	}
	types := issueTypes(report.Issues)
	for _, typ := range []string{"missing_remote", "extra_remote", "size_mismatch"} {
		if !types[typ] {
			t.Fatalf("missing issue type %q in %#v", typ, report.Issues)
		}
	}
}

func TestCompareAllowsVideoToSTRMProxy(t *testing.T) {
	t.Parallel()

	localRoot := t.TempDir()
	remoteRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "Season 01", "Show S01E01.mkv"), "video bytes")
	writeFile(t, filepath.Join(remoteRoot, "Season 01", "Show S01E01.strm"), "https://example.invalid/video.mkv")

	report, err := Compare(context.Background(), Options{LocalRoot: localRoot, RemoteRoot: remoteRoot, RemoteFS: LocalFS{}, AllowSTRMProxy: true, CompareSize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("expected no issues, got %#v", report.Issues)
	}
}

func TestCompareCollapsesRemoteFilesUnderIgnoredLocalDirectory(t *testing.T) {
	t.Parallel()

	localRoot := t.TempDir()
	remoteRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "Season 99", ".ignore"), "")
	writeFile(t, filepath.Join(localRoot, "Season 99", "Local Only.mkv"), "ignored")
	writeFile(t, filepath.Join(remoteRoot, "Season 99", "Remote A.mkv"), "remote")
	writeFile(t, filepath.Join(remoteRoot, "Season 99", "Nested", "Remote B.nfo"), "remote")

	report, err := Compare(context.Background(), Options{LocalRoot: localRoot, RemoteRoot: remoteRoot, RemoteFS: LocalFS{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("expected one collapsed issue, got %#v", report.Issues)
	}
	if report.Issues[0].Type != "extra_remote_dir" || report.Issues[0].Path != "Season 99" {
		t.Fatalf("unexpected issue: %#v", report.Issues[0])
	}
}

func TestCompareMD5DetectsMismatch(t *testing.T) {
	t.Parallel()

	localRoot := t.TempDir()
	remoteRoot := t.TempDir()
	writeFile(t, filepath.Join(localRoot, "file.nfo"), "left")
	writeFile(t, filepath.Join(remoteRoot, "file.nfo"), "right")

	report, err := Compare(context.Background(), Options{LocalRoot: localRoot, RemoteRoot: remoteRoot, RemoteFS: LocalFS{}, CompareMD5: true})
	if err != nil {
		t.Fatal(err)
	}
	if !issueTypes(report.Issues)["md5_mismatch"] {
		t.Fatalf("expected md5 mismatch, got %#v", report.Issues)
	}
}

func writeFile(t *testing.T, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func issueTypes(issues []Issue) map[string]bool {
	result := map[string]bool{}
	for _, issue := range issues {
		result[issue.Type] = true
	}
	return result
}

var _ FileSystem = LocalFS{}
