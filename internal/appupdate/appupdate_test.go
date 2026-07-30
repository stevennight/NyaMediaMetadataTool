package appupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckClassifiesNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/stevennight/NyaMediaMetadataTool/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v1.2.0","name":"Release 1.2.0","body":"Fixes","published_at":"2026-07-30T00:00:00Z","assets":[]}`)
	}))
	defer server.Close()

	client := Client{APIBaseURL: server.URL, HTTPClient: server.Client()}
	result, err := client.Check(context.Background(), "1.1.9", runtime.GOOS == "windows")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if result.Status != "unsupported" || result.Reason != ReasonUnsupported {
			t.Fatalf("Check() = %+v", result)
		}
		return
	}
	if result.Status != "available" || result.Version != "1.2.0" || result.ReleaseNotes != "Fixes" {
		t.Fatalf("Check() = %+v", result)
	}
}

func TestCheckRejectsDevelopmentVersionWithoutNetwork(t *testing.T) {
	client := Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("development version unexpectedly used the network")
		return nil, nil
	})}}
	result, err := client.Check(context.Background(), "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "unsupported" || result.Reason != ReasonDevelopmentBuild {
		t.Fatalf("Check() = %+v", result)
	}
}

func TestDownloadVerifiesInstallerChecksum(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("automatic installer downloads are Windows-only")
	}
	installer := []byte("signed installer placeholder")
	checksum := fmt.Sprintf("%x", sha256.Sum256(installer))
	assetName := "NyaMediaMetadataTool-1.2.0-windows-amd64-installer.exe"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/stevennight/NyaMediaMetadataTool/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.0","assets":[{"name":%q,"browser_download_url":%q,"size":%d},{"name":"SHA256SUMS","browser_download_url":%q,"size":%d}]}`,
				assetName, "http://"+r.Host+"/installer", len(installer), "http://"+r.Host+"/checksums", len(checksum)+2+len(assetName)+1)
		case "/installer":
			w.Write(installer)
		case "/checksums":
			fmt.Fprintf(w, "%s  %s\n", checksum, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{APIBaseURL: server.URL, HTTPClient: server.Client()}
	target, err := client.Download(context.Background(), "1.1.0", "1.2.0", t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(installer) || filepath.Base(target) != assetName {
		t.Fatalf("unexpected installer %q at %q", data, target)
	}
}

func TestChecksumForFileRequiresExactSafeName(t *testing.T) {
	hash := strings.Repeat("a", 64)
	checksums := hash + "  expected.exe\n" + hash + "  ../expected.exe\n"
	if actual, ok := checksumForFile(checksums, "expected.exe"); !ok || actual != hash {
		t.Fatalf("checksumForFile() = %q, %v", actual, ok)
	}
	if _, ok := checksumForFile(checksums, "other.exe"); ok {
		t.Fatal("checksumForFile matched another file")
	}
}

func TestSelectAssetsRejectsUnsafeInstallerName(t *testing.T) {
	assets := []releaseAsset{
		{
			Name: "NyaMediaMetadataTool-1.2.0/subdir-windows-amd64-installer.exe",
			Size: 42,
		},
		{Name: "SHA256SUMS", Size: 42},
	}
	if _, _, err := selectAssets(assets, "amd64"); err == nil {
		t.Fatal("selectAssets accepted an installer outside the download directory")
	}
}

func TestVersionComparison(t *testing.T) {
	current, _ := parseVersion("1.9.9")
	latest, _ := parseVersion("2.0.0")
	if !latest.greaterThan(current) || current.greaterThan(latest) {
		t.Fatal("semantic version comparison is incorrect")
	}
	for _, invalid := range []string{"v1.0.0", "1.0", "1.0.0-beta", "01.0.0"} {
		if _, err := parseVersion(invalid); err == nil {
			t.Fatalf("parseVersion(%q) unexpectedly succeeded", invalid)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
