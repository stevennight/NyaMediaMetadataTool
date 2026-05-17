package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"NyaMediaMetadataTool/internal/config"
)

func TestSearchTVRetriesUnexpectedEOFResponse(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`{"results":`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":1,"name":"Sword Art Online","original_name":"ソードアート・オンライン","first_air_date":"2012-07-08","overview":""}]}`))
	}))
	defer server.Close()

	client, err := NewClient(config.ScrapingConfig{EnableTMDB: true, TMDBAPIKey: "test", TMDBBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	results, err := client.SearchTV(context.Background(), "ソードアート・オンライン")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != 1 {
		t.Fatalf("unexpected results: %#v", results)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestImageDownloadURLDoesNotChangeNFOImageURL(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.ScrapingConfig{
		EnableTMDB:        true,
		TMDBAPIKey:        "test",
		TMDBImageBaseURL:  "https://tmdb-image-cache.example/cache",
		TMDBBaseURL:       "https://api.example/tmdb",
		FallbackLanguages: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := client.ImageURL("/poster.jpg"); !strings.HasPrefix(got, officialTMDBImageURL) {
		t.Fatalf("expected official image URL for NFO, got %q", got)
	}
	if got := client.DownloadImageURL("/poster.jpg"); got != "https://tmdb-image-cache.example/cache/t/p/original/poster.jpg" {
		t.Fatalf("unexpected download URL: %q", got)
	}
}

func TestTMDBBaseURLPrefixAppendsAPIVersion(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.ScrapingConfig{EnableTMDB: true, TMDBAPIKey: "test", TMDBBaseURL: "https://api.example/tmdb"})
	if err != nil {
		t.Fatal(err)
	}
	if client.baseURL != "https://api.example/tmdb/3" {
		t.Fatalf("unexpected base URL: %q", client.baseURL)
	}
}

func TestSearchTVRetriesServerError(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":2,"name":"MAO"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(config.ScrapingConfig{EnableTMDB: true, TMDBAPIKey: "test", TMDBBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	results, err := client.SearchTV(context.Background(), "MAO")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != 2 {
		t.Fatalf("unexpected results: %#v", results)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}
