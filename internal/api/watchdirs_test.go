package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

type failingWatchDirReloader struct{}

func (failingWatchDirReloader) ReloadWatchDirs(context.Context) error {
	return errors.New("reload failed")
}

func TestCreateWatchDirReturnsConflictForExistingPath(t *testing.T) {
	handler, st := newWatchDirTestHandler(t, nil)
	path := filepath.Join(t.TempDir(), "media")
	if _, err := st.CreateWatchDir(context.Background(), store.WatchDir{Path: path, Recursive: true, WatchEnabled: true}); err != nil {
		t.Fatal(err)
	}

	response := postWatchDir(t, handler, path)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateWatchDirSucceedsWhenWatcherReloadFails(t *testing.T) {
	handler, st := newWatchDirTestHandler(t, failingWatchDirReloader{})
	path := filepath.Join(t.TempDir(), "media")

	response := postWatchDir(t, handler, path)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	dirs, err := st.ListWatchDirs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0].Path != path {
		t.Fatalf("persisted watch directories = %#v", dirs)
	}
}

func newWatchDirTestHandler(t *testing.T, reloader WatchDirReloader) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, reloader, slog.Default()), st
}

func postWatchDir(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"path":` + strconv.Quote(path) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
