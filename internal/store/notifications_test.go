package store

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadCompletionNotificationUsesRemoteSeriesDirectoryAndRouteVariables(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()

	template, err := st.CreateUploadNotificationTemplate(ctx, UploadNotificationTemplate{
		Name: "Media library refresh",
		URL:  "https://example.test/notify",
		HeadersTemplate: `{
			"X-Webhook-Token": "{{webhook_token}}",
			"X-Notification-Mode": "fixed"
		}`,
		PayloadTemplate: `{
			"event": "change",
			"source_path": "{{path}}",
			"is_dir": true,
			"provider_id": "{{provider_id}}",
			"library_id": "{{library_id}}"
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:    "115 Notification",
		Type:    UploadProviderType115Cookie,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{
		ProviderID:             provider.ID,
		Enabled:                true,
		RemoteRoot:             "/影视/番剧",
		CollisionPolicy:        "fail",
		IncludeTypes:           []string{"video"},
		NotificationTemplateID: &template.ID,
		NotificationVariables: map[string]string{
			"provider_id":   "provider-a",
			"library_id":    "library-b",
			"webhook_token": "secret-token",
		},
	}})
	seriesPath := filepath.Join(root, "示例番剧")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "Season 01", "S01E01.mkv"),
			RelativePath: "示例番剧/Season 01/S01E01.mkv",
			FileType:     "video",
			Size:         100,
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
	if target.NotificationTemplateID == nil || *target.NotificationTemplateID != template.ID ||
		target.NotificationVariables["provider_id"] != "provider-a" {
		t.Fatalf("notification configuration was not snapshotted: %#v", target)
	}
	transfers, err := st.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil || len(transfers) != 1 {
		t.Fatalf("transfers=%#v err=%v", transfers, err)
	}
	if err := st.StartUploadTransfer(ctx, transfers[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUploadTransferWithResult(ctx, transfers[0].ID, UploadTransferCompletion{
		RemoteID: "remote-file",
		Outcome:  UploadOutcomeCreated,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteUploadTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	if batch.ID <= 0 {
		t.Fatal("batch was not created")
	}
	notification, err := st.ClaimNextUploadNotification(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["source_path"] != "/影视/番剧/示例番剧" || payload["provider_id"] != "provider-a" ||
		payload["library_id"] != "library-b" || payload["is_dir"] != true {
		t.Fatalf("unexpected notification payload: %#v", payload)
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(notification.Headers), &headers); err != nil {
		t.Fatal(err)
	}
	if headers["X-Webhook-Token"] != "secret-token" || headers["X-Notification-Mode"] != "fixed" {
		t.Fatalf("unexpected notification headers: %#v", headers)
	}
	if err := st.CompleteUploadNotification(ctx, notification.ID, http.StatusNoContent); err != nil {
		t.Fatal(err)
	}
	records, err := st.ListUploadNotificationRecords(ctx, UploadNotificationRecordFilters{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if records.Total != 1 || len(records.Items) != 1 || records.Items[0].Status != UploadNotificationDelivered || records.Items[0].BatchID != batch.ID || records.Items[0].ProviderName != "115 Notification" {
		t.Fatalf("unexpected notification records: %#v", records)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.NotificationCount != 1 || detail.Batch.DeliveredNotifications != 1 || detail.Batch.PendingNotifications != 0 || detail.Batch.FailedNotifications != 0 || len(detail.Notifications) != 1 {
		t.Fatalf("unexpected notification summary: batch=%#v notifications=%#v", detail.Batch, detail.Notifications)
	}
}

func TestUploadNotificationTemplateRejectsMissingRouteVariableAndDeleteWhileInUse(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	template, err := st.CreateUploadNotificationTemplate(ctx, UploadNotificationTemplate{
		Name:            "Needs provider",
		URL:             "https://example.test/notify",
		HeadersTemplate: `{"X-Webhook-Token":"{{webhook_token}}"}`,
		PayloadTemplate: `{"source_path":"{{path}}","provider_id":"{{provider_id}}"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 Variables", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateWatchDir(ctx, WatchDir{
		Path:         t.TempDir(),
		WatchEnabled: true,
		UploadConfigs: []UploadProviderRoute{{
			ProviderID:             provider.ID,
			Enabled:                false,
			RemoteRoot:             "/",
			CollisionPolicy:        "fail",
			IncludeTypes:           []string{"video"},
			NotificationTemplateID: &template.ID,
		}},
	})
	if err == nil {
		t.Fatal("missing provider_id variable was accepted")
	}
	dir, err := st.CreateWatchDir(ctx, WatchDir{
		Path:         t.TempDir(),
		WatchEnabled: true,
		UploadConfigs: []UploadProviderRoute{{
			ProviderID:             provider.ID,
			Enabled:                false,
			RemoteRoot:             "/",
			CollisionPolicy:        "fail",
			IncludeTypes:           []string{"video"},
			NotificationTemplateID: &template.ID,
			NotificationVariables:  map[string]string{"provider_id": "configured", "webhook_token": "secret"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.UploadConfigs) != 1 || dir.UploadConfigs[0].NotificationVariables["provider_id"] != "configured" {
		t.Fatalf("notification route did not round trip: %#v", dir.UploadConfigs)
	}
	if err := st.DeleteUploadNotificationTemplate(ctx, template.ID); err != ErrUploadNotificationTemplateInUse {
		t.Fatalf("delete in-use template error=%v", err)
	}
}

func TestUploadNotificationTemplateRejectsInvalidHeaders(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	for _, headers := range []string{
		`{"X-Webhook-Token":123}`,
		`{"Invalid Header":"token"}`,
		"{\"X-Webhook-Token\":\"line1\\nline2\"}",
		`null`,
	} {
		_, err := st.CreateUploadNotificationTemplate(ctx, UploadNotificationTemplate{
			Name:            "Invalid headers",
			URL:             "https://example.test/notify",
			HeadersTemplate: headers,
			PayloadTemplate: `{}`,
		})
		if err == nil {
			t.Fatalf("invalid headers were accepted: %s", headers)
		}
	}
}

func TestUploadNotificationTemplateUpdateAddsVariablesToExistingRoutes(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	template, err := st.CreateUploadNotificationTemplate(ctx, UploadNotificationTemplate{
		Name:            "Existing route",
		URL:             "https://example.test/notify",
		HeadersTemplate: `{}`,
		PayloadTemplate: `{"source_path":"{{path}}"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:    "115 Existing Route",
		Type:    UploadProviderType115Cookie,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, WatchDir{
		Path:         t.TempDir(),
		WatchEnabled: true,
		UploadConfigs: []UploadProviderRoute{{
			ProviderID:             provider.ID,
			Enabled:                false,
			RemoteRoot:             "/",
			CollisionPolicy:        "fail",
			IncludeTypes:           []string{"video"},
			NotificationTemplateID: &template.ID,
			NotificationVariables:  map[string]string{"existing": "keep-me"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	template.HeadersTemplate = `{"X-Webhook-Token":"{{webhook_token}}"}`
	template.PayloadTemplate = `{"source_path":"{{path}}","provider_id":"{{provider_id}}"}`
	if _, err := st.UpdateUploadNotificationTemplate(ctx, template); err != nil {
		t.Fatal(err)
	}
	persisted, err := st.GetWatchDir(ctx, dir.ID)
	if err != nil {
		t.Fatal(err)
	}
	variables := persisted.UploadConfigs[0].NotificationVariables
	if variables["webhook_token"] != "" || variables["provider_id"] != "" || variables["existing"] != "keep-me" {
		t.Fatalf("template variables were not synchronized: %#v", variables)
	}
}

func TestMigrateAddsUploadNotificationHeaderColumns(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "legacy-notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, `
CREATE TABLE upload_notification_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  payload_template TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE upload_notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_target_id INTEGER NOT NULL UNIQUE,
  template_id INTEGER NOT NULL,
  template_name TEXT NOT NULL,
  url TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  response_status INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  delivered_at TEXT,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for table, column := range map[string]string{
		"upload_notification_templates": "headers_template",
		"upload_notifications":          "headers",
	} {
		exists, err := st.hasColumn(ctx, table, column)
		if err != nil || !exists {
			t.Fatalf("%s.%s exists=%v err=%v", table, column, exists, err)
		}
	}
}
