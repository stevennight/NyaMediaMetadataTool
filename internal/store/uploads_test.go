package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadProviderStoresCookieSeparately(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()

	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "115 Archive",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/Anime",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.HasCookie {
		t.Fatal("new provider should not report a Cookie")
	}
	if err := st.SetUploadProviderSecret(ctx, provider.ID, "cookie", "UID=secret"); err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListUploadProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].HasCookie {
		t.Fatalf("unexpected listed providers: %#v", listed)
	}
	if listed[0].RemoteRoot != "/Anime" || listed[0].CollisionPolicy != "fail" {
		t.Fatalf("provider normalization failed: %#v", listed[0])
	}
	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "UID=secret" {
		t.Fatalf("unexpected Cookie: %q", cookie)
	}
}

func TestCollectUploadBatchCoalescesFilesAndTargets(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	first, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 A", Type: UploadProviderType115Cookie, Enabled: true, RemoteRoot: "/A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 B", Type: UploadProviderType115Cookie, Enabled: true, RemoteRoot: "/B"}); err != nil {
		t.Fatal(err)
	}

	seriesPath := filepath.Join(t.TempDir(), "Show")
	base := time.Now().UTC().Add(-time.Minute)
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "watch-1:" + seriesPath,
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "S01E01.mkv"),
			RelativePath: "S01E01.mkv",
			FileType:     "video",
			Size:         100,
			ModifiedAt:   base,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || batch.ID == 0 || batch.TargetCount != 2 {
		t.Fatalf("unexpected initial batch: %#v created=%v", batch, created)
	}

	updated, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "watch-1:" + seriesPath,
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "S01E01.nfo"),
			RelativePath: "S01E01.nfo",
			FileType:     "nfo",
			Size:         10,
			ModifiedAt:   base,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || updated.ID != batch.ID || updated.FileCount != 2 || updated.TargetCount != 2 {
		t.Fatalf("batch should coalesce: %#v created=%v", updated, created)
	}

	sealed, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if sealed != 1 {
		t.Fatalf("expected one sealed batch, got %d", sealed)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.Status != UploadBatchPending || len(detail.Files) != 2 || len(detail.Targets) != 2 || len(detail.Transfers) != 4 {
		t.Fatalf("unexpected sealed detail: %#v files=%d targets=%d transfers=%d", detail.Batch, len(detail.Files), len(detail.Targets), len(detail.Transfers))
	}
	if detail.Targets[0].ProviderID != first.ID {
		t.Fatalf("unexpected first target: %#v", detail.Targets[0])
	}
}

func TestUploadTargetCompletionCreatesOneEventPerDestination(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	for _, name := range []string{"115 A", "115 B"} {
		if _, err := st.CreateUploadProvider(ctx, UploadProvider{Name: name, Type: UploadProviderType115Cookie, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	seriesPath := filepath.Join(t.TempDir(), "Show")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "episode.mkv"),
			RelativePath: "episode.mkv",
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
	for range 2 {
		target, err := st.ClaimNextUploadTarget(ctx)
		if err != nil {
			t.Fatal(err)
		}
		transfers, err := st.ListUploadTransfersByTarget(ctx, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, transfer := range transfers {
			if err := st.StartUploadTransfer(ctx, transfer.ID); err != nil {
				t.Fatal(err)
			}
			if err := st.CompleteUploadTransfer(ctx, transfer.ID, "remote-"+transfer.RelativePath); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.CompleteUploadTarget(ctx, target.ID); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.Status != UploadBatchCompleted || detail.Batch.CompletedTargets != 2 {
		t.Fatalf("batch did not complete: %#v", detail.Batch)
	}
	events, err := st.ListUploadEvents(ctx, "pending", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected one event per destination, got %#v", events)
	}
}

func TestNewSeriesChangeStartsNextUploadBatchAfterSeal(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(t.TempDir(), "Show")
	input := UploadCollectionInput{
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath:    filepath.Join(seriesPath, "S01E01.mkv"),
			RelativePath: "S01E01.mkv",
			FileType:     "video",
			Size:         100,
			ModifiedAt:   time.Now().Add(-time.Minute),
		}},
	}
	first, created, err := st.CollectUploadBatch(ctx, input)
	if err != nil || !created {
		t.Fatalf("first batch err=%v created=%v", err, created)
	}
	if _, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	input.Files[0] = UploadCandidate{
		LocalPath:    filepath.Join(seriesPath, "S01E02.mkv"),
		RelativePath: "S01E02.mkv",
		FileType:     "video",
		Size:         200,
		ModifiedAt:   time.Now().Add(-time.Minute),
	}
	second, created, err := st.CollectUploadBatch(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !created || second.ID == first.ID || second.Revision != 2 {
		t.Fatalf("expected new revision after seal: first=%#v second=%#v created=%v", first, second, created)
	}
	firstDetail, err := st.GetUploadBatchDetail(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstDetail.Files) != 1 || firstDetail.Files[0].RelativePath != "S01E01.mkv" {
		t.Fatalf("sealed batch was mutated: %#v", firstDetail.Files)
	}
}

func TestUploadRoutesLimitTargetsAndOverrideContentProfile(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	root := t.TempDir()
	firstDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "First"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	secondDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "Second"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	firstID := firstDir.ID
	secondID := secondDir.ID
	firstProvider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "First archive",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/unused",
		Routes: []UploadProviderRoute{{
			WatchDirID:      &firstID,
			Enabled:         true,
			RemoteRoot:      "/First",
			CollisionPolicy: "fail",
			IncludeTypes:    []string{"video", "nfo"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondProvider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "Second archive",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/unused",
		Routes: []UploadProviderRoute{{
			WatchDirID:      &secondID,
			Enabled:         true,
			RemoteRoot:      "/Second",
			CollisionPolicy: "fail",
			IncludeTypes:    []string{"video"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	seriesPath := filepath.Join(root, "First", "Show")
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:          &firstID,
		SeriesKey:           "watch-first:show",
		SeriesPath:          seriesPath,
		QuietPeriod:         time.Minute,
		DefaultIncludeTypes: []string{"video"},
		Files: []UploadCandidate{
			{LocalPath: filepath.Join(seriesPath, "S01E01.mkv"), RelativePath: "S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()},
			{LocalPath: filepath.Join(seriesPath, "S01E01.nfo"), RelativePath: "S01E01.nfo", FileType: "nfo", Size: 10, ModifiedAt: time.Now()},
			{LocalPath: filepath.Join(seriesPath, "S01E01.bif"), RelativePath: "S01E01.bif", FileType: "bif", Size: 10, ModifiedAt: time.Now()},
		},
	})
	if err != nil || !created {
		t.Fatalf("collect batch err=%v created=%v", err, created)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Targets) != 1 || detail.Targets[0].ProviderID != firstProvider.ID || detail.Targets[0].RemoteRoot != "/First" {
		t.Fatalf("unexpected target snapshot: %#v", detail.Targets)
	}
	if len(detail.Files) != 2 || len(detail.Transfers) != 2 {
		t.Fatalf("expected only video and nfo transfers, files=%#v transfers=%#v", detail.Files, detail.Transfers)
	}
	for _, transfer := range detail.Transfers {
		if transfer.BatchTargetID != detail.Targets[0].ID || transfer.RemotePath[:len("/First/")] != "/First/" {
			t.Fatalf("unexpected transfer: %#v", transfer)
		}
	}

	secondBatch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:          &secondID,
		SeriesKey:           "watch-second:show",
		SeriesPath:          filepath.Join(root, "Second", "Show"),
		QuietPeriod:         time.Minute,
		DefaultIncludeTypes: []string{"video"},
		Files:               []UploadCandidate{{LocalPath: filepath.Join(root, "Second", "Show", "S01E01.mkv"), RelativePath: "S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()}},
	})
	if err != nil || !created {
		t.Fatalf("collect second batch err=%v created=%v", err, created)
	}
	secondDetail, err := st.GetUploadBatchDetail(ctx, secondBatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondDetail.Targets) != 1 || secondDetail.Targets[0].ProviderID != secondProvider.ID || secondDetail.Targets[0].RemoteRoot != "/Second" {
		t.Fatalf("unexpected second target snapshot: %#v", secondDetail.Targets)
	}
}

func TestUploadRoutesUseScopedOverrideBeforeGlobalFallback(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	root := t.TempDir()
	firstDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "First"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	secondDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "Second"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	firstID := firstDir.ID
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "Archive",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/Legacy",
		Routes: []UploadProviderRoute{
			{Enabled: true, RemoteRoot: "/All", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
			{WatchDirID: &firstID, Enabled: true, RemoteRoot: "/First", CollisionPolicy: "skip", IncludeTypes: []string{"video", "nfo"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	firstBatch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:          &firstDir.ID,
		SeriesKey:           "first:show",
		SeriesPath:          filepath.Join(root, "First", "Show"),
		QuietPeriod:         time.Minute,
		DefaultIncludeTypes: []string{"video"},
		Files: []UploadCandidate{
			{LocalPath: filepath.Join(root, "First", "Show", "episode.mkv"), RelativePath: "episode.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()},
			{LocalPath: filepath.Join(root, "First", "Show", "episode.nfo"), RelativePath: "episode.nfo", FileType: "nfo", Size: 10, ModifiedAt: time.Now()},
		},
	})
	if err != nil || !created {
		t.Fatalf("collect first batch err=%v created=%v", err, created)
	}
	firstDetail, err := st.GetUploadBatchDetail(ctx, firstBatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstDetail.Targets) != 1 || firstDetail.Targets[0].RemoteRoot != "/First" || firstDetail.Targets[0].CollisionPolicy != "skip" || len(firstDetail.Transfers) != 2 {
		t.Fatalf("scoped override did not win: targets=%#v transfers=%#v", firstDetail.Targets, firstDetail.Transfers)
	}

	secondBatch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:          &secondDir.ID,
		SeriesKey:           "second:show",
		SeriesPath:          filepath.Join(root, "Second", "Show"),
		QuietPeriod:         time.Minute,
		DefaultIncludeTypes: []string{"video"},
		Files: []UploadCandidate{
			{LocalPath: filepath.Join(root, "Second", "Show", "episode.mkv"), RelativePath: "episode.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()},
			{LocalPath: filepath.Join(root, "Second", "Show", "episode.nfo"), RelativePath: "episode.nfo", FileType: "nfo", Size: 10, ModifiedAt: time.Now()},
		},
	})
	if err != nil || !created {
		t.Fatalf("collect second batch err=%v created=%v", err, created)
	}
	secondDetail, err := st.GetUploadBatchDetail(ctx, secondBatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondDetail.Targets) != 1 || secondDetail.Targets[0].RemoteRoot != "/All" || secondDetail.Targets[0].CollisionPolicy != "fail" || len(secondDetail.Transfers) != 1 {
		t.Fatalf("global fallback was not applied: targets=%#v transfers=%#v", secondDetail.Targets, secondDetail.Transfers)
	}
}

func TestScopedUploadRouteStaysRestrictedAfterWatchDirectoryDeleted(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	root := t.TempDir()
	firstDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "First"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	secondDir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(root, "Second"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	firstID := firstDir.ID
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "First only",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/First",
		Routes: []UploadProviderRoute{{
			WatchDirID:      &firstID,
			Enabled:         true,
			RemoteRoot:      "/First",
			CollisionPolicy: "fail",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteWatchDir(ctx, firstDir.ID); err != nil {
		t.Fatal(err)
	}
	provider, err = st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Routes) != 1 || provider.Routes[0].WatchDirID != nil || provider.Routes[0].Enabled {
		t.Fatalf("expected a disabled global fallback after watch-dir deletion, got %#v", provider.Routes)
	}
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:          &secondDir.ID,
		SeriesKey:           "second:show",
		SeriesPath:          filepath.Join(root, "Second", "Show"),
		QuietPeriod:         time.Minute,
		DefaultIncludeTypes: []string{"video"},
		Files: []UploadCandidate{{
			LocalPath: filepath.Join(root, "Second", "Show", "episode.mkv"), RelativePath: "episode.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now(),
		}},
	})
	if err != nil || created || batch.ID != 0 {
		t.Fatalf("deleted scoped route must not become global: batch=%#v created=%v err=%v", batch, created, err)
	}
}

func TestUploadRouteInheritsDestinationDefaultsAndValidatesFileTypes(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	dir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(t.TempDir(), "Media"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	dirID := dir.ID
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:            "Inherited route",
		Type:            UploadProviderType115Cookie,
		Enabled:         true,
		RemoteRoot:      "/Archive",
		CollisionPolicy: "skip",
		Routes: []UploadProviderRoute{{
			WatchDirID:   &dirID,
			Enabled:      true,
			IncludeTypes: []string{"all"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var scoped *UploadProviderRoute
	for index := range provider.Routes {
		if provider.Routes[index].WatchDirID != nil {
			scoped = &provider.Routes[index]
			break
		}
	}
	if scoped == nil || scoped.RemoteRoot != "/Archive" || scoped.CollisionPolicy != "skip" || len(scoped.IncludeTypes) != len(uploadFileTypes) {
		t.Fatalf("route did not inherit defaults or expand all: %#v", provider.Routes)
	}
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:       "Invalid profile",
		Type:       UploadProviderType115Cookie,
		Enabled:    true,
		RemoteRoot: "/Invalid",
		Routes:     []UploadProviderRoute{{WatchDirID: &dirID, Enabled: true, IncludeTypes: []string{"not-a-file-type"}}},
	}); err == nil {
		t.Fatal("expected invalid route file type to be rejected")
	}
}

func TestUploadEventLeaseLifecycleAndPayload(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true, RemoteRoot: "/Anime"}); err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(t.TempDir(), "Show")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Millisecond,
		Files: []UploadCandidate{{
			LocalPath: filepath.Join(seriesPath, "S01E01.mkv"), RelativePath: "S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now().Add(-time.Minute),
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
	if err != nil {
		t.Fatal(err)
	}
	for _, transfer := range transfers {
		if err := st.StartUploadTransfer(ctx, transfer.ID); err != nil {
			t.Fatal(err)
		}
		if err := st.CompleteUploadTransfer(ctx, transfer.ID, "remote-file"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CompleteUploadTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimUploadEvents(ctx, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].Status != UploadEventProcessing || claimed[0].LeaseID == "" {
		t.Fatalf("unexpected claimed events: %#v", claimed)
	}
	for _, required := range []string{"eventKey", "providerType", "seriesKey", "revision", "files"} {
		if !strings.Contains(claimed[0].Payload, required) {
			t.Fatalf("event payload lacks %q: %s", required, claimed[0].Payload)
		}
	}
	if err := st.FailUploadEvent(ctx, claimed[0].ID, claimed[0].LeaseID, "temporary", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	claimed, err = st.ClaimUploadEvents(ctx, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 2 {
		t.Fatalf("expected a reclaimed event, got %#v", claimed)
	}
	if err := st.AckUploadEvent(ctx, claimed[0].ID, claimed[0].LeaseID); err != nil {
		t.Fatal(err)
	}
	delivered, err := st.ListUploadEvents(ctx, UploadEventDelivered, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0].BatchTargetID != target.ID || delivered[0].Attempts != 2 {
		t.Fatalf("unexpected delivered events: %#v", delivered)
	}
	if detail, err := st.GetUploadBatchDetail(ctx, batch.ID); err != nil || detail.Batch.Status != UploadBatchCompleted {
		t.Fatalf("batch completion failed: detail=%#v err=%v", detail, err)
	}
}

func TestCollectingBatchRefreshesChangedTransferSnapshot(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	if _, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(t.TempDir(), "Show")
	mediaPath := filepath.Join(seriesPath, "S01E01.mkv")
	firstModified := time.Now().Add(-time.Minute)
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Minute,
		Files: []UploadCandidate{{
			LocalPath: mediaPath, RelativePath: "S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: firstModified,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		SeriesKey:   "show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Minute,
		Files: []UploadCandidate{{
			LocalPath: mediaPath, RelativePath: "S01E01.mkv", FileType: "video", Size: 125, ModifiedAt: firstModified.Add(time.Second),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Transfers) != 1 || detail.Transfers[0].BytesTotal != 125 || detail.Transfers[0].Status != UploadTransferPending {
		t.Fatalf("transfer snapshot was not refreshed: %#v", detail.Transfers)
	}
}
