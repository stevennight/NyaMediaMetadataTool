package store

import (
	"context"
	"encoding/json"
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
			"provider_id": "provider-a",
			"library_id":  "library-b",
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
}

func TestUploadNotificationTemplateRejectsMissingRouteVariableAndDeleteWhileInUse(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	template, err := st.CreateUploadNotificationTemplate(ctx, UploadNotificationTemplate{
		Name:            "Needs provider",
		URL:             "https://example.test/notify",
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
			NotificationVariables:  map[string]string{"provider_id": "configured"},
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
