package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func TestTaskSummaryIsIndependentOfTaskListFilters(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for index := 0; index < 4; index++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("episode-%d.mkv", index+1))
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mediaID, err := st.UpsertMediaFile(ctx, path, info)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.EnqueueMediaTaskWithOptions(ctx, mediaID, true, true); err != nil {
			t.Fatal(err)
		}
	}

	running, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FailTask(ctx, failed.ID, "test failure"); err != nil {
		t.Fatal(err)
	}
	completed, err := st.ClaimNextPendingTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteTask(ctx, completed.ID); err != nil {
		t.Fatal(err)
	}
	if running.Status != "running" {
		t.Fatalf("expected first task to be running, got %q", running.Status)
	}

	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/summary?status=completed&page=99", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", response.Code, response.Body.String())
	}
	var got store.TaskSummary
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != (store.TaskSummary{Total: 4, Active: 2, Failed: 1}) {
		t.Fatalf("unexpected task summary: %+v", got)
	}
}
