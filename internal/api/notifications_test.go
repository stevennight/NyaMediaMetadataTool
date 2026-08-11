package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

func TestUploadNotificationTemplateCRUDAPI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())

	create := httptest.NewRequest(http.MethodPost, "/api/upload/notification-templates", bytes.NewBufferString(
		`{"name":"Refresh","url":"https://example.test/notify","headersTemplate":"{\"X-Webhook-Token\":\"{{webhook_token}}\"}","payloadTemplate":"{\"source_path\":\"{{path}}\"}"}`,
	))
	create.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created store.UploadNotificationTemplate
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.Name != "Refresh" || created.HeadersTemplate != "{\n  \"X-Webhook-Token\": \"{{webhook_token}}\"\n}" {
		t.Fatalf("unexpected created template: %#v", created)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/upload/notification-templates", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed []store.UploadNotificationTemplate
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected template list: %#v", listed)
	}

	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, httptest.NewRequest(http.MethodDelete, "/api/upload/notification-templates/"+jsonNumber(created.ID), nil))
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestUploadNotificationRecordsAPIIsSafeAndQueryable(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	template, err := st.CreateUploadNotificationTemplate(ctx, store.UploadNotificationTemplate{
		Name:            "Safe record",
		URL:             "https://example.test/notify",
		HeadersTemplate: `{"X-Webhook-Token":"{{webhook_token}}"}`,
		PayloadTemplate: `{"source_path":"{{path}}"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Record Provider", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	watchDir, err := st.CreateWatchDir(ctx, store.WatchDir{
		Path:         root,
		WatchEnabled: true,
		UploadConfigs: []store.UploadProviderRoute{{
			ProviderID:             provider.ID,
			Enabled:                true,
			RemoteRoot:             "/Video",
			CollisionPolicy:        "fail",
			IncludeTypes:           []string{"video"},
			NotificationTemplateID: &template.ID,
			NotificationVariables:  map[string]string{"webhook_token": "private-token"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(root, "Record Show")
	batch, _, err := st.CollectUploadBatch(ctx, store.UploadCollectionInput{
		WatchDirID:  &watchDir.ID,
		SeriesKey:   "record-show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []store.UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "episode.mkv"),
			RelativePath: "Record Show/episode.mkv",
			FileType:     "video",
			Size:         10,
			ModifiedAt:   time.Now().Add(-time.Minute),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	target, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := st.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil || len(transfers) != 1 {
		t.Fatalf("transfers=%#v err=%v", transfers, err)
	}
	if err := st.StartUploadTransfer(ctx, transfers[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUploadTransferWithResult(ctx, transfers[0].ID, store.UploadTransferCompletion{Outcome: store.UploadOutcomeCreated}); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUploadTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	notification, err := st.ClaimNextUploadNotification(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUploadNotification(ctx, notification.ID, http.StatusNoContent); err != nil {
		t.Fatal(err)
	}

	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/upload/notifications?status=delivered&path=Record+Show", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("private-token")) {
		t.Fatal("notification credentials leaked through records API")
	}
	var listed store.UploadNotificationRecordListResult
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].BatchID != batch.ID || listed.Items[0].Status != store.UploadNotificationDelivered {
		t.Fatalf("unexpected notification records: %#v", listed)
	}
}
