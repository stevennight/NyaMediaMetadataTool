package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestTaskRunsEndpointReturnsBatchSummary(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginScanRun(context.Background(), "scan-api", "manual", "media"); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishScanRun(context.Background(), "scan-api", ""); err != nil {
		t.Fatal(err)
	}

	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/runs?limit=10", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("runs status=%d body=%s", response.Code, response.Body.String())
	}
	var runs []store.ScanRunSummary
	if err := json.NewDecoder(response.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "scan-api" || runs[0].Status != store.ScanRunStatusEmpty {
		t.Fatalf("unexpected task runs: %+v", runs)
	}
}

func TestTasksEndpointFiltersByScanRun(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	for index, scanRunID := range []string{"batch-a", "batch-b"} {
		if err := st.BeginScanRun(ctx, scanRunID, "manual", fmt.Sprintf("media-%d", index)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), fmt.Sprintf("episode-%d.mkv", index))
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
		if err := st.EnqueueMediaTaskWithScanRun(ctx, mediaID, true, true, scanRunID); err != nil {
			t.Fatal(err)
		}
	}

	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())
	request := httptest.NewRequest(http.MethodGet, "/api/tasks?scanRunId=batch-b&page=1&pageSize=20", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("tasks cache-control=%q, want no-store", got)
	}
	var got store.TaskListResult
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ScanRunID != "batch-b" {
		t.Fatalf("unexpected filtered tasks: %+v", got)
	}
}

func TestTaskEventsEndpointNotifiesAfterTaskChange(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default()))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/tasks/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("events status=%d", response.StatusCode)
	}

	reader := bufio.NewReader(response.Body)
	for index := 0; index < 2; index++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.BeginScanRun(context.Background(), "events-test", "manual", "media"); err != nil {
		t.Fatal(err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "event: tasks-changed" {
			return
		}
	}
}

func TestRescanAllDirectoriesUsesOneScanRun(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Processing.StableDelay = 0
	for index := 1; index <= 2; index++ {
		dirPath := t.TempDir()
		mediaPath := filepath.Join(dirPath, fmt.Sprintf("Series - S01E%02d.mkv", index))
		if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateWatchDir(context.Background(), store.WatchDir{
			Path:                dirPath,
			Recursive:           true,
			WatchEnabled:        true,
			UseGlobalProcessing: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	server := NewServerWithContext(context.Background(), cfg, filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/rescan", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("rescan status=%d body=%s", response.Code, response.Body.String())
	}
	var queued struct {
		ScanRunID string `json:"scanRunId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&queued); err != nil {
		t.Fatal(err)
	}
	if queued.ScanRunID == "" {
		t.Fatal("rescan response omitted scanRunId")
	}
	if err := server.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	tasks, err := st.ListTasks(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ScanRunID != queued.ScanRunID || tasks[1].ScanRunID != queued.ScanRunID {
		t.Fatalf("expected two tasks in run %q, got %+v", queued.ScanRunID, tasks)
	}
	runs, err := st.ListScanRunSummaries(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != queued.ScanRunID || runs[0].ScanFinishedAt == "" || runs[0].Total != 2 {
		t.Fatalf("unexpected scan run summary: %+v", runs)
	}
}
