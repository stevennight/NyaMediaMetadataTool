package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

func TestUploadProviderRejectsGenericCookieSecret(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()

	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:      "115 Archive",
		Type:      UploadProviderType115Cookie,
		Enabled:   true,
		UserAgent: "NyaMedia/Test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.HasCookie {
		t.Fatal("new provider should not report a Cookie")
	}
	if err := st.SetUploadProviderSecret(ctx, provider.ID, "cookie", "UID=secret"); !errors.Is(err, ErrUploadProviderCookieOnly) {
		t.Fatalf("generic Cookie write error=%v", err)
	}
	listed, err := st.ListUploadProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].HasCookie {
		t.Fatalf("unexpected listed providers: %#v", listed)
	}
	if listed[0].AuthDevice != "" {
		t.Fatalf("generic secret writes must not claim a known auth device: %#v", listed[0])
	}
	if listed[0].UserAgent != "NyaMedia/Test" {
		t.Fatalf("provider normalization failed: %#v", listed[0])
	}
	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		t.Fatalf("unexpected Cookie: %q", cookie)
	}
}

func TestUploadProviderUpdatePreservesConcurrentAuthDeviceAndRejectsTypeChange(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 Concurrent", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	// Model an edit form loaded before a QR authorization completes.
	staleEdit := provider
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=device", "android"); err != nil {
		t.Fatal(err)
	}
	staleEdit.Name = "115 Concurrent Updated"
	staleEdit.AuthDevice = "web"
	updated, err := st.UpdateUploadProvider(ctx, staleEdit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != staleEdit.Name || updated.AuthDevice != "android" {
		t.Fatalf("stale provider update overwrote authorization metadata: %#v", updated)
	}

	typeChange := updated
	typeChange.Name = "Must Not Persist"
	typeChange.Type = UploadProviderType115Open
	typeChange.Enabled = false
	if _, err := st.UpdateUploadProvider(ctx, typeChange); !errors.Is(err, ErrUploadProviderTypeImmutable) {
		t.Fatalf("provider type change error=%v", err)
	}
	persisted, err := st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != staleEdit.Name || persisted.Type != UploadProviderType115Cookie || persisted.AuthDevice != "android" || !persisted.HasCookie || cookie != "UID=device" {
		t.Fatalf("rejected type change altered provider credentials: provider=%#v cookie=%q", persisted, cookie)
	}
}

func TestUploadProviderCookiePersistsAndClearsAuthDevice(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 Device", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=device", "iOS"); err != nil {
		t.Fatal(err)
	}
	provider, err = st.GetUploadProvider(ctx, provider.ID)
	if err != nil || !provider.HasCookie || provider.AuthDevice != "ios" {
		t.Fatalf("unexpected provider after Cookie save: %#v err=%v", provider, err)
	}
	if err := st.DeleteUploadProviderSecret(ctx, provider.ID, "cookie"); err != nil {
		t.Fatal(err)
	}
	provider, err = st.GetUploadProvider(ctx, provider.ID)
	if err != nil || provider.HasCookie || provider.AuthDevice != "" {
		t.Fatalf("unexpected provider after Cookie delete: %#v err=%v", provider, err)
	}
}

func TestUploadProviderCookieRejectsUnsupportedAuthDevice(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 Invalid Device", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=device", "windows"); err == nil {
		t.Fatal("unsupported auth device should be rejected")
	}
	provider, err = st.GetUploadProvider(ctx, provider.ID)
	if err != nil || provider.HasCookie || provider.AuthDevice != "" {
		t.Fatalf("rejected auth device changed provider: %#v err=%v", provider, err)
	}
}

func TestCollectUploadBatchCoalescesFilesAndTargets(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	first, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 A", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 B", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{
		{ProviderID: first.ID, Enabled: true, RemoteRoot: "/A", CollisionPolicy: "fail", IncludeTypes: []string{"video", "nfo"}},
		{ProviderID: second.ID, Enabled: true, RemoteRoot: "/B", CollisionPolicy: "fail", IncludeTypes: []string{"video", "nfo"}},
	})
	seriesPath := filepath.Join(root, "Show")
	base := time.Now().UTC().Add(-time.Minute)
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
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
		WatchDirID:  &dir.ID,
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
	if detail.Batch.TransferCount != 4 || detail.Batch.CompletedTransfers != 0 || detail.Batch.FailedTransfers != 0 {
		t.Fatalf("unexpected initial transfer progress: %#v", detail.Batch)
	}
	if detail.Targets[0].ProviderID != first.ID {
		t.Fatalf("unexpected first target: %#v", detail.Targets[0])
	}
}

func TestUploadTargetCompletionCreatesOneEventPerDestination(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	providers := make([]UploadProvider, 0, 2)
	for _, name := range []string{"115 A", "115 B"} {
		provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: name, Type: UploadProviderType115Cookie, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		providers = append(providers, provider)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{
		{ProviderID: providers[0].ID, Enabled: true, RemoteRoot: "/A", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
		{ProviderID: providers[1].ID, Enabled: true, RemoteRoot: "/B", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
	})
	seriesPath := filepath.Join(root, "Show")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
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
	if detail.Batch.Status != UploadBatchCompleted || detail.Batch.CompletedTargets != 2 ||
		detail.Batch.TransferCount != 2 || detail.Batch.CompletedTransfers != 2 || detail.Batch.FailedTransfers != 0 {
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

func TestRetryUploadTargetResetsAutomaticRetryBudget(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115 Retry", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{
		ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Retry", CollisionPolicy: "fail", IncludeTypes: []string{"video"},
	}})
	seriesPath := filepath.Join(root, "Show")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
		SeriesKey:   "retry-show",
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
	target, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := st.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 1 {
		t.Fatalf("unexpected transfers: %#v", transfers)
	}
	if err := st.StartUploadTransfer(ctx, transfers[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.FailUploadTransfer(ctx, transfers[0].ID, "temporary"); err != nil {
		t.Fatal(err)
	}
	if err := st.RescheduleUploadTarget(ctx, target.ID, "temporary", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	rescheduled, err := st.GetUploadBatch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rescheduled.FailedTargets != 0 || rescheduled.FailedTransfers != 0 {
		t.Fatalf("automatic retry was counted as a terminal failure: %#v", rescheduled)
	}
	target, err = st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if target.Attempts != 2 {
		t.Fatalf("expected two automatic attempts before manual retry, got %#v", target)
	}
	if err := st.FailUploadTarget(ctx, target.ID, "automatic retry budget exhausted"); err != nil {
		t.Fatal(err)
	}
	failed, err := st.GetUploadBatch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.FailedTargets != 1 || failed.FailedTransfers != 1 {
		t.Fatalf("terminal upload failure was not reflected in progress: %#v", failed)
	}
	if err := st.RetryUploadTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.Status != UploadBatchPending || detail.Batch.FailedTargets != 0 || detail.Batch.FailedTransfers != 0 ||
		len(detail.Targets) != 1 || detail.Targets[0].Status != UploadTargetPending || detail.Targets[0].Attempts != 0 {
		t.Fatalf("manual retry did not reset target retry budget: %#v", detail)
	}
	reclaimed, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.ID != target.ID || reclaimed.Attempts != 1 {
		t.Fatalf("new automatic retry cycle did not start at attempt one: %#v", reclaimed)
	}
}

func TestCollectUploadBatchSeparatesWatchDirectoriesWithSameSeriesKey(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "Archive", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	firstRoot := filepath.Join(root, "First")
	secondRoot := filepath.Join(root, "Second")
	firstDir := createUploadTestWatchDir(t, st, ctx, firstRoot, []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/First", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}})
	secondDir := createUploadTestWatchDir(t, st, ctx, secondRoot, []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Second", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}})
	now := time.Now()
	collect := func(dir WatchDir, seriesPath, fileName string) UploadBatch {
		t.Helper()
		batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
			WatchDirID: &dir.ID, SeriesKey: "same-series-key", SeriesPath: seriesPath, QuietPeriod: time.Minute,
			Files: []UploadCandidate{{LocalPath: filepath.Join(seriesPath, fileName), RelativePath: filepath.ToSlash(filepath.Join(filepath.Base(dir.Path), fileName)), FileType: "video", Size: 100, ModifiedAt: now}},
		})
		if err != nil || !created {
			t.Fatalf("collect %s: batch=%#v created=%v err=%v", dir.Path, batch, created, err)
		}
		return batch
	}
	first := collect(firstDir, filepath.Join(firstRoot, "Show"), "S01E01.mkv")
	second := collect(secondDir, filepath.Join(secondRoot, "Show"), "S01E01.mkv")
	if first.ID == second.ID || first.Revision != 1 || second.Revision != 1 {
		t.Fatalf("same series keys crossed watch-directory boundaries: first=%#v second=%#v", first, second)
	}
	firstAgain, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID: &firstDir.ID, SeriesKey: "same-series-key", SeriesPath: filepath.Join(firstRoot, "Show"), QuietPeriod: time.Minute,
		Files: []UploadCandidate{{LocalPath: filepath.Join(firstRoot, "Show", "S01E02.mkv"), RelativePath: "First/S01E02.mkv", FileType: "video", Size: 100, ModifiedAt: now.Add(time.Second)}},
	})
	if err != nil || created || firstAgain.ID != first.ID || firstAgain.FileCount != 2 {
		t.Fatalf("same-directory batch did not coalesce: batch=%#v created=%v err=%v", firstAgain, created, err)
	}
	secondDetail, err := st.GetUploadBatchDetail(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondDetail.Batch.FileCount != 1 {
		t.Fatalf("second directory batch was mutated by first directory: %#v", secondDetail.Batch)
	}
}

func TestNewSeriesChangeStartsNextUploadBatchAfterSeal(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}})
	seriesPath := filepath.Join(root, "Show")
	input := UploadCollectionInput{
		WatchDirID:  &dir.ID,
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
	firstProvider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:    "First archive",
		Type:    UploadProviderType115Cookie,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondProvider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:    "Second archive",
		Type:    UploadProviderType115Cookie,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDir.UploadConfigs = []UploadProviderRoute{{ProviderID: firstProvider.ID, Enabled: true, RemoteRoot: "/First", CollisionPolicy: "fail", IncludeTypes: []string{"video", "nfo"}}}
	if firstDir, err = st.UpdateWatchDir(ctx, firstDir); err != nil {
		t.Fatal(err)
	}
	secondDir.UploadConfigs = []UploadProviderRoute{{ProviderID: secondProvider.ID, Enabled: true, RemoteRoot: "/Second", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}}
	if secondDir, err = st.UpdateWatchDir(ctx, secondDir); err != nil {
		t.Fatal(err)
	}

	seriesPath := filepath.Join(root, "First", "Show")
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &firstDir.ID,
		SeriesKey:   "watch-first:show",
		SeriesPath:  seriesPath,
		QuietPeriod: time.Minute,
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
		WatchDirID:  &secondDir.ID,
		SeriesKey:   "watch-second:show",
		SeriesPath:  filepath.Join(root, "Second", "Show"),
		QuietPeriod: time.Minute,
		Files:       []UploadCandidate{{LocalPath: filepath.Join(root, "Second", "Show", "S01E01.mkv"), RelativePath: "S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()}},
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

func TestUploadRoutesRequireExactWatchDirectory(t *testing.T) {
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
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "Archive", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	firstDir.UploadConfigs = []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/First", CollisionPolicy: "skip", IncludeTypes: []string{"video", "nfo"}}}
	if firstDir, err = st.UpdateWatchDir(ctx, firstDir); err != nil {
		t.Fatal(err)
	}

	firstBatch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &firstDir.ID,
		SeriesKey:   "first:show",
		SeriesPath:  filepath.Join(root, "First", "Show"),
		QuietPeriod: time.Minute,
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
		WatchDirID:  &secondDir.ID,
		SeriesKey:   "second:show",
		SeriesPath:  filepath.Join(root, "Second", "Show"),
		QuietPeriod: time.Minute,
		Files: []UploadCandidate{
			{LocalPath: filepath.Join(root, "Second", "Show", "episode.mkv"), RelativePath: "episode.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()},
			{LocalPath: filepath.Join(root, "Second", "Show", "episode.nfo"), RelativePath: "episode.nfo", FileType: "nfo", Size: 10, ModifiedAt: time.Now()},
		},
	})
	if err != nil || created || secondBatch.ID != 0 {
		t.Fatalf("directory without an exact upload configuration must not create a batch: batch=%#v created=%v err=%v", secondBatch, created, err)
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
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "First only", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	firstDir.UploadConfigs = []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/First", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}}
	if firstDir, err = st.UpdateWatchDir(ctx, firstDir); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteWatchDir(ctx, firstDir.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	configs, err := st.ListWatchDirUploadConfigs(ctx, firstDir.ID)
	if err != nil || len(configs) != 0 {
		t.Fatalf("watch-directory upload configurations should cascade on delete: %#v err=%v", configs, err)
	}
	batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &secondDir.ID,
		SeriesKey:   "second:show",
		SeriesPath:  filepath.Join(root, "Second", "Show"),
		QuietPeriod: time.Minute,
		Files: []UploadCandidate{{
			LocalPath: filepath.Join(root, "Second", "Show", "episode.mkv"), RelativePath: "episode.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now(),
		}},
	})
	if err != nil || created || batch.ID != 0 {
		t.Fatalf("deleted scoped route must not become global: batch=%#v created=%v err=%v", batch, created, err)
	}
}

func TestWatchDirUploadConfigNormalizesAndValidatesProfile(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	dir, err := st.CreateWatchDir(ctx, WatchDir{Path: filepath.Join(t.TempDir(), "Media"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{
		Name:    "Archive",
		Type:    UploadProviderType115Cookie,
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Archive"}}
	if _, err := st.UpdateWatchDir(ctx, dir); !errors.Is(err, ErrInvalidUploadConfig) {
		t.Fatalf("empty include types should be rejected, got %v", err)
	}
	dir.UploadConfigs = []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, IncludeTypes: []string{"all"}}}
	dir, err = st.UpdateWatchDir(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dir.UploadConfigs) != 1 || dir.UploadConfigs[0].RemoteRoot != "/" || dir.UploadConfigs[0].CollisionPolicy != "fail" || len(dir.UploadConfigs[0].IncludeTypes) != len(uploadFileTypes) {
		t.Fatalf("upload configuration was not normalized: %#v", dir.UploadConfigs)
	}
	dir.UploadConfigs = []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Invalid", IncludeTypes: []string{"not-a-file-type"}}}
	if _, err := st.UpdateWatchDir(ctx, dir); err == nil {
		t.Fatal("expected invalid route file type to be rejected")
	}
	persisted, err := st.GetWatchDir(ctx, dir.ID)
	if err != nil || len(persisted.UploadConfigs) != 1 || persisted.UploadConfigs[0].RemoteRoot != "/" {
		t.Fatalf("failed replacement should roll back atomically: %#v err=%v", persisted.UploadConfigs, err)
	}
}

func TestUploadEventLeaseLifecycleAndPayload(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Anime", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}})
	seriesPath := filepath.Join(root, "Show")
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
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
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "115", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}})
	seriesPath := filepath.Join(root, "Show")
	mediaPath := filepath.Join(seriesPath, "S01E01.mkv")
	firstModified := time.Now().Add(-time.Minute)
	batch, _, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID:  &dir.ID,
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
		WatchDirID:  &dir.ID,
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

func TestMigrateLegacyGlobalUploadRoutesToExplicitWatchDirConfigs(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, err = st.db.ExecContext(ctx, `
CREATE TABLE watch_dirs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  recursive INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  watch_enabled INTEGER NOT NULL DEFAULT 1,
  scan_on_start INTEGER NOT NULL DEFAULT 0,
  use_global_processing INTEGER NOT NULL DEFAULT 1,
  processing_config TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE upload_providers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  remote_root TEXT NOT NULL DEFAULT '/',
  user_agent TEXT NOT NULL DEFAULT '',
  collision_policy TEXT NOT NULL DEFAULT 'replace',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE upload_provider_routes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider_id INTEGER NOT NULL,
  watch_dir_id INTEGER,
  enabled INTEGER NOT NULL DEFAULT 1,
  remote_root TEXT NOT NULL DEFAULT '/',
  collision_policy TEXT NOT NULL DEFAULT 'fail',
  include_types TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(provider_id) REFERENCES upload_providers(id) ON DELETE CASCADE,
  FOREIGN KEY(watch_dir_id) REFERENCES watch_dirs(id) ON DELETE CASCADE
);
INSERT INTO watch_dirs (id, path) VALUES (1, 'D:/Media/A'), (2, 'D:/Media/B');
INSERT INTO upload_providers (id, name, type, remote_root, collision_policy)
VALUES (1, 'Legacy defaults', '115cookie', '/Legacy', 'skip'),
       (2, 'Legacy fallback', '115cookie', '/Unused', 'replace');
INSERT INTO upload_provider_routes (provider_id, watch_dir_id, enabled, remote_root, collision_policy, include_types)
VALUES (2, NULL, 1, '/All', 'fail', '["video"]'),
       (2, 1, 1, '/First', 'skip', '["video","nfo"]');
`)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &config.LegacyUploadConfig{
		Enabled:      false,
		Concurrency:  4,
		QuietPeriod:  45 * time.Second,
		MaxAttempts:  7,
		IncludeTypes: []string{"video", "nfo"},
	}
	if err := st.Migrate(ctx, legacy); err != nil {
		t.Fatal(err)
	}

	first, err := st.ListWatchDirUploadConfigs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.ListWatchDirUploadConfigs(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("legacy routes were not expanded for both directories: first=%#v second=%#v", first, second)
	}
	for _, route := range append(append([]UploadProviderRoute{}, first...), second...) {
		if route.Enabled {
			t.Fatalf("legacy upload.enabled=false must disable every migrated directory config: %#v", route)
		}
	}
	byProvider := func(items []UploadProviderRoute, providerID int64) UploadProviderRoute {
		for _, item := range items {
			if item.ProviderID == providerID {
				return item
			}
		}
		return UploadProviderRoute{}
	}
	if route := byProvider(first, 1); route.RemoteRoot != "/Legacy" || route.CollisionPolicy != "skip" || strings.Join(route.IncludeTypes, ",") != "video,nfo" {
		t.Fatalf("provider defaults were not materialized: %#v", route)
	}
	if route := byProvider(first, 2); route.RemoteRoot != "/First" || len(route.IncludeTypes) != 2 {
		t.Fatalf("scoped route should override the global route: %#v", route)
	}
	if route := byProvider(second, 2); route.RemoteRoot != "/All" || len(route.IncludeTypes) != 1 {
		t.Fatalf("global route was not materialized: %#v", route)
	}
	options, err := st.GetUploadRuntimeOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if options.Concurrency != 4 || options.QuietPeriod != 45*time.Second || options.MaxAttempts != 7 {
		t.Fatalf("legacy runtime settings were not persisted: %#v", options)
	}
	var globalCount int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upload_provider_routes WHERE watch_dir_id IS NULL`).Scan(&globalCount); err != nil || globalCount != 0 {
		t.Fatalf("global routes remain after migration: count=%d err=%v", globalCount, err)
	}
	if _, err := st.db.ExecContext(ctx, `INSERT INTO upload_provider_routes (provider_id, watch_dir_id) VALUES (1, NULL)`); err == nil {
		t.Fatal("database should reject new global upload routes")
	}
	newProvider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "New", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	persistedOptions, err := st.GetUploadRuntimeOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if persistedOptions != options {
		t.Fatalf("runtime settings changed after legacy config was removed: before=%#v after=%#v", options, persistedOptions)
	}
	for _, watchDirID := range []int64{1, 2} {
		configs, err := st.ListWatchDirUploadConfigs(ctx, watchDirID)
		if err != nil {
			t.Fatal(err)
		}
		if route := byProvider(configs, newProvider.ID); route.ID != 0 {
			t.Fatalf("new providers must not acquire implicit directory routes: %#v", route)
		}
	}
}

func TestMigrationCancelsLegacyIncompleteUploadWorkOnce(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "Archive", Type: UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := createUploadTestWatchDir(t, st, ctx, root, []UploadProviderRoute{{
		ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Archive", CollisionPolicy: "fail", IncludeTypes: []string{"video"},
	}})
	seriesPath := filepath.Join(root, "Show")
	collect := func(name string) UploadBatch {
		t.Helper()
		batch, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
			WatchDirID: &dir.ID, SeriesKey: "show", SeriesPath: seriesPath, QuietPeriod: time.Millisecond,
			Files: []UploadCandidate{{LocalPath: filepath.Join(seriesPath, name), RelativePath: filepath.ToSlash(filepath.Join("Show", name)), FileType: "video", Size: 100, ModifiedAt: time.Now()}},
		})
		if err != nil || !created {
			t.Fatalf("collect %s: batch=%#v created=%v err=%v", name, batch, created, err)
		}
		return batch
	}

	completed := collect("S01E01.mkv")
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
		if err := st.CompleteUploadTransfer(ctx, transfer.ID, "remote-complete"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CompleteUploadTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	incomplete := collect("S01E02.mkv")
	failedSeriesPath := filepath.Join(root, "Failed Show")
	failed, created, err := st.CollectUploadBatch(ctx, UploadCollectionInput{
		WatchDirID: &dir.ID, SeriesKey: "failed-show", SeriesPath: failedSeriesPath, QuietPeriod: time.Minute,
		Files: []UploadCandidate{{LocalPath: filepath.Join(failedSeriesPath, "S01E01.mkv"), RelativePath: "Failed Show/S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()}},
	})
	if err != nil || !created {
		t.Fatalf("collect failed batch: batch=%#v created=%v err=%v", failed, created, err)
	}
	failedBefore, err := st.GetUploadBatchDetail(ctx, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FailUploadTarget(ctx, failedBefore.Targets[0].ID, "legacy failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE name = 'cancel-legacy-upload-work-v1'`); err != nil {
		t.Fatal(err)
	}
	if err := st.cancelLegacyUploadWork(ctx); err != nil {
		t.Fatal(err)
	}

	completedDetail, err := st.GetUploadBatchDetail(ctx, completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completedDetail.Batch.Status != UploadBatchCompleted || completedDetail.Targets[0].Status != UploadTargetCompleted || completedDetail.Transfers[0].Status != UploadTransferCompleted || !completedDetail.Targets[0].Retryable {
		t.Fatalf("completed upload history was changed: %#v", completedDetail)
	}
	incompleteDetail, err := st.GetUploadBatchDetail(ctx, incomplete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if incompleteDetail.Batch.Status != UploadBatchCanceled || incompleteDetail.Targets[0].Status != UploadTargetCanceled || incompleteDetail.Transfers[0].Status != UploadTransferCanceled || incompleteDetail.Targets[0].Retryable {
		t.Fatalf("legacy incomplete upload work was not canceled: %#v", incompleteDetail)
	}
	failedDetail, err := st.GetUploadBatchDetail(ctx, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failedDetail.Batch.Status != UploadBatchFailed || failedDetail.Targets[0].Status != UploadTargetFailed || failedDetail.Targets[0].Retryable {
		t.Fatalf("legacy failed target was not made permanently non-retryable: %#v", failedDetail)
	}
	for _, detail := range []UploadBatchDetail{incompleteDetail, failedDetail} {
		targetID := detail.Targets[0].ID
		if err := st.RetryUploadTarget(ctx, targetID); !errors.Is(err, ErrUploadTargetNotRetryable) {
			t.Fatalf("legacy target %d retry error=%v", targetID, err)
		}
		after, err := st.GetUploadBatchDetail(ctx, detail.Batch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Batch.Status != detail.Batch.Status || after.Targets[0].Status != detail.Targets[0].Status || after.Transfers[0].Status != detail.Transfers[0].Status || after.Targets[0].Retryable {
			t.Fatalf("rejected retry changed legacy state: before=%#v after=%#v", detail, after)
		}
	}

	fresh := collect("S01E03.mkv")
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	freshBatch, err := st.GetUploadBatch(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if freshBatch.Status != UploadBatchCollecting {
		t.Fatalf("one-time migration canceled new upload work: %#v", freshBatch)
	}
	freshDetail, err := st.GetUploadBatchDetail(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshDetail.Targets) != 1 || !freshDetail.Targets[0].Retryable {
		t.Fatalf("new upload targets must remain retryable: %#v", freshDetail.Targets)
	}
}

func TestLegacyUploadMigrationDefaultsMissingBlockToDisabled(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "missing-upload-block.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
INSERT INTO watch_dirs (id, path) VALUES (1, 'D:/Media');
INSERT INTO upload_providers (id, name, type) VALUES (1, 'Legacy', '115cookie');
INSERT INTO upload_provider_routes (provider_id, watch_dir_id, enabled, remote_root, collision_policy, include_types)
VALUES (1, 1, 1, '/Archive', 'fail', '["video"]');
`); err != nil {
		t.Fatal(err)
	}
	if err := st.migrateLegacyUploadSettings(ctx, nil); err != nil {
		t.Fatal(err)
	}
	configs, err := st.ListWatchDirUploadConfigs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].Enabled {
		t.Fatalf("missing legacy upload block must remain disabled: %#v", configs)
	}
}

func TestMigrateAddsRetryableColumnToLegacyUploadTargets(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "legacy-targets.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, `
CREATE TABLE upload_batch_targets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  provider_id INTEGER NOT NULL,
  provider_name TEXT NOT NULL,
  provider_type TEXT NOT NULL,
  remote_root TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  collision_policy TEXT NOT NULL DEFAULT 'fail',
  include_types TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  available_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO upload_batch_targets (id, batch_id, provider_id, provider_name, provider_type, remote_root, status, available_at)
VALUES (1, 1, 1, 'Completed', '115cookie', '/', 'completed', CURRENT_TIMESTAMP),
       (2, 2, 2, 'Failed', '115cookie', '/', 'failed', CURRENT_TIMESTAMP);
`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	hasRetryable, err := st.hasColumn(ctx, "upload_batch_targets", "retryable")
	if err != nil || !hasRetryable {
		t.Fatalf("retryable column was not added: present=%v err=%v", hasRetryable, err)
	}
	rows, err := st.db.QueryContext(ctx, `SELECT id, retryable FROM upload_batch_targets ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64]int{}
	for rows.Next() {
		var id int64
		var retryable int
		if err := rows.Scan(&id, &retryable); err != nil {
			t.Fatal(err)
		}
		got[id] = retryable
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got[1] != 1 || got[2] != 0 {
		t.Fatalf("unexpected migrated retryable values: %#v", got)
	}
}

func createUploadTestWatchDir(t *testing.T, st *Store, ctx context.Context, path string, configs []UploadProviderRoute) WatchDir {
	t.Helper()
	dir, err := st.CreateWatchDir(ctx, WatchDir{
		Path:                path,
		Recursive:           true,
		WatchEnabled:        true,
		UseGlobalProcessing: true,
		UploadConfigs:       configs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
