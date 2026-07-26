package upload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

type fakeProvider struct {
	mu         sync.Mutex
	uploads    []fakeUpload
	attempts   []fakeUpload
	fail       error
	failUpload func(string) error
	verifyFile func(string, int64) (RemoteFile, bool, error)
}

type fakeUpload struct {
	LocalPath  string
	RemotePath string
	Size       int64
}

type waitReportingProvider struct {
	fakeProvider
	reporter         func(string, time.Time)
	progressReporter func(int64)
	checkStarted     chan struct{}
	continueCheck    chan struct{}
	uploadStarted    chan struct{}
	continueUpload   chan struct{}
}

func (p *waitReportingProvider) setWaitReporter(reporter func(string, time.Time)) {
	p.reporter = reporter
}

func (p *waitReportingProvider) setProgressReporter(reporter func(int64)) {
	p.progressReporter = reporter
}

func (p *waitReportingProvider) Check(ctx context.Context) error {
	waitingUntil := time.Now().Add(time.Minute)
	if p.reporter != nil {
		p.reporter("115 已触发访问保护，正在等待冷却后重试", waitingUntil)
	}
	close(p.checkStarted)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.continueCheck:
		if p.reporter != nil {
			p.reporter("", time.Time{})
		}
		return nil
	}
}

func (p *waitReportingProvider) Upload(ctx context.Context, localPath string, remotePath string, size int64, collisionPolicy string) (RemoteFile, error) {
	if p.uploadStarted != nil {
		if p.progressReporter != nil {
			p.progressReporter(size / 2)
		}
		close(p.uploadStarted)
		select {
		case <-ctx.Done():
			return RemoteFile{}, ctx.Err()
		case <-p.continueUpload:
		}
	}
	return p.fakeProvider.Upload(ctx, localPath, remotePath, size, collisionPolicy)
}

func (p *fakeProvider) Check(context.Context) error { return nil }

func (p *fakeProvider) List(context.Context, string) ([]RemoteEntry, error) { return nil, nil }

func (p *fakeProvider) Upload(_ context.Context, localPath string, remotePath string, size int64, _ string) (RemoteFile, error) {
	p.mu.Lock()
	upload := fakeUpload{LocalPath: localPath, RemotePath: remotePath, Size: size}
	p.attempts = append(p.attempts, upload)
	fail := p.fail
	if p.failUpload != nil {
		fail = p.failUpload(remotePath)
	}
	if fail == nil {
		p.uploads = append(p.uploads, upload)
	}
	p.mu.Unlock()
	if fail != nil {
		return RemoteFile{}, fail
	}
	return RemoteFile{ID: "remote-" + filepath.Base(remotePath), Size: size}, nil
}

func (p *fakeProvider) Verify(_ context.Context, remotePath string, size int64) (RemoteFile, bool, error) {
	if p.verifyFile == nil {
		return RemoteFile{}, false, nil
	}
	return p.verifyFile(remotePath, size)
}

func TestSetTransferProgressAllowsMultipartRetryRollback(t *testing.T) {
	manager := NewWithFactory(Options{}, nil, slog.Default(), nil)
	const transferID int64 = 42
	manager.setTransferRuntime(transferID, "uploading", "正在上传到 115", time.Time{})

	manager.setTransferProgress(transferID, 16*1024*1024)
	manager.setTransferProgress(transferID, 0)

	state, ok := manager.TransferRuntimeStates()[transferID]
	if !ok || state.BytesTransferred != 0 {
		t.Fatalf("runtime state=%#v ok=%v, want retry rollback to zero", state, ok)
	}
}

func TestProcessTargetPublishesAndClearsRuntimeWaitState(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()

	root := t.TempDir()
	showPath := filepath.Join(root, "Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(showPath, "S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	providerRecord, err := st.CreateUploadProvider(ctx, store.UploadProvider{
		Name: "115 Wait", Type: store.UploadProviderType115Cookie, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{
		Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []store.UploadProviderRoute{{
		ProviderID: providerRecord.ID, Enabled: true, RemoteRoot: "/Archive", CollisionPolicy: "fail", IncludeTypes: []string{"video"},
	}}
	if _, err := st.UpdateWatchDir(ctx, dir); err != nil {
		t.Fatal(err)
	}

	provider := &waitReportingProvider{
		checkStarted:   make(chan struct{}),
		continueCheck:  make(chan struct{}),
		uploadStarted:  make(chan struct{}),
		continueUpload: make(chan struct{}),
	}
	manager := NewWithFactory(Options{QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(context.Context, store.UploadBatchTarget) (Provider, error) {
		return provider, nil
	})
	if err := manager.RecordMediaProcessed(ctx, store.Task{ID: 1}, store.MediaFile{
		Path: mediaPath, Size: info.Size(), ModifiedAt: info.ModTime(),
	}); err != nil {
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

	processDone := make(chan error, 1)
	go func() {
		processDone <- manager.processTarget(ctx, target)
	}()
	select {
	case <-provider.checkStarted:
	case <-time.After(time.Second):
		t.Fatal("provider check did not publish its wait state")
	}

	state, ok := manager.TransferRuntimeStates()[transfers[0].ID]
	if !ok || state.Phase != "waiting" ||
		state.StatusMessage != "115 已触发访问保护，正在等待冷却后重试" ||
		state.WaitingUntil == "" {
		t.Fatalf("unexpected transfer runtime state: %#v ok=%v", state, ok)
	}

	close(provider.continueCheck)
	select {
	case <-provider.uploadStarted:
	case <-time.After(time.Second):
		t.Fatal("provider upload did not publish progress")
	}
	state, ok = manager.TransferRuntimeStates()[transfers[0].ID]
	if !ok || state.Phase != "uploading" || state.BytesTransferred != info.Size()/2 || state.WaitingUntil != "" {
		t.Fatalf("unexpected transfer progress state: %#v ok=%v", state, ok)
	}
	close(provider.continueUpload)
	select {
	case err := <-processDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("target processing did not resume after the wait")
	}
	if _, ok := manager.TransferRuntimeStates()[transfers[0].ID]; ok {
		t.Fatal("completed transfer retained stale runtime wait state")
	}
}

func TestWorkerUploadsOneBatchToEveryEnabledProvider(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	root := t.TempDir()
	showPath := filepath.Join(root, "Example Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(showPath, "Example Show - S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 A", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 B", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []store.UploadProviderRoute{
		{ProviderID: first.ID, Enabled: true, RemoteRoot: "/A", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
		{ProviderID: second.ID, Enabled: true, RemoteRoot: "/B", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
	}
	if _, err := st.UpdateWatchDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	providers := map[int64]*fakeProvider{first.ID: {}, second.ID: {}}
	manager := NewWithFactory(Options{Concurrency: 1, QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(_ context.Context, target store.UploadBatchTarget) (Provider, error) {
		return providers[target.ProviderID], nil
	})
	info, err := os.Stat(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordMediaProcessed(ctx, store.Task{ID: 42}, store.MediaFile{ID: 1, Path: mediaPath, Size: info.Size(), ModifiedAt: info.ModTime()}); err != nil {
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
		if err := manager.processTarget(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	for providerID, fake := range providers {
		if len(fake.uploads) != 1 {
			t.Fatalf("provider %d uploaded %#v", providerID, fake.uploads)
		}
		wantRoot := "/A/Example Show/"
		if providerID == second.ID {
			wantRoot = "/B/Example Show/"
		}
		if got := fake.uploads[0].RemotePath; !strings.HasPrefix(got, wantRoot) {
			t.Fatalf("provider %d uploaded to %q, expected root %q", providerID, got, wantRoot)
		}
	}
	summary, err := st.GetUploadSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Completed != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestFailedProviderDoesNotPreventOtherTargetCompletion(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	root := t.TempDir()
	showPath := filepath.Join(root, "Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(showPath, "S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	okProvider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Good", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	badProvider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Bad", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []store.UploadProviderRoute{
		{ProviderID: okProvider.ID, Enabled: true, RemoteRoot: "/Good", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
		{ProviderID: badProvider.ID, Enabled: true, RemoteRoot: "/Bad", CollisionPolicy: "fail", IncludeTypes: []string{"video"}},
	}
	if _, err := st.UpdateWatchDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	providers := map[int64]*fakeProvider{okProvider.ID: {}, badProvider.ID: {fail: fmt.Errorf("network down")}}
	manager := NewWithFactory(Options{QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(_ context.Context, target store.UploadBatchTarget) (Provider, error) {
		return providers[target.ProviderID], nil
	})
	info, _ := os.Stat(mediaPath)
	if err := manager.RecordMediaProcessed(ctx, store.Task{ID: 7}, store.MediaFile{Path: mediaPath, Size: info.Size(), ModifiedAt: info.ModTime()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		manager.worker(workerCtx)
	}()
	deadline := time.Now().Add(3 * time.Second)
	var summary store.UploadSummary
	for time.Now().Before(deadline) {
		summary, err = st.GetUploadSummary(ctx)
		if err != nil {
			cancelWorker()
			<-workerDone
			t.Fatal(err)
		}
		if summary.Failed == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelWorker()
	<-workerDone
	if summary.Completed != 1 || summary.Failed != 1 || len(providers[okProvider.ID].uploads) != 1 {
		t.Fatalf("unexpected terminal states: summary=%#v good=%#v", summary, providers[okProvider.ID].uploads)
	}
}

func TestProcessTargetContinuesAfterOneTransferFails(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	root := t.TempDir()
	showPath := filepath.Join(root, "Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	providerRecord, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Archive", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []store.UploadProviderRoute{{
		ProviderID: providerRecord.ID, Enabled: true, RemoteRoot: "/Archive", CollisionPolicy: "fail", IncludeTypes: []string{"nfo"},
	}}
	if _, err := st.UpdateWatchDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	candidates := make([]store.UploadCandidate, 0, 3)
	for _, name := range []string{"first.nfo", "broken.json", "last.srt"} {
		localPath := filepath.Join(showPath, name)
		content := []byte("content-" + name)
		if err := os.WriteFile(localPath, content, 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(localPath)
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, store.UploadCandidate{
			LocalPath: localPath, RelativePath: filepath.ToSlash(filepath.Join("Show", name)), FileType: "nfo", Size: info.Size(), ModifiedAt: info.ModTime(),
		})
	}
	batch, created, err := st.CollectUploadBatch(ctx, store.UploadCollectionInput{
		WatchDirID: &dir.ID, SeriesKey: "show", SeriesPath: showPath, QuietPeriod: time.Millisecond, Files: candidates,
	})
	if err != nil || !created {
		t.Fatalf("collect batch: batch=%#v created=%v err=%v", batch, created, err)
	}
	if _, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	target, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{failUpload: func(remotePath string) error {
		if strings.HasSuffix(remotePath, "/broken.json") {
			return errors.New("temporary OSS failure")
		}
		return nil
	}}
	manager := NewWithFactory(Options{QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(context.Context, store.UploadBatchTarget) (Provider, error) {
		return fake, nil
	})
	err = manager.processTarget(ctx, target)
	if err == nil || !strings.Contains(err.Error(), "1 file(s) failed") {
		t.Fatalf("process target error=%v", err)
	}
	processErr := err
	if len(fake.attempts) != 3 || len(fake.uploads) != 2 {
		t.Fatalf("attempts=%#v successful=%#v", fake.attempts, fake.uploads)
	}
	transfers, err := st.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed, failed := 0, 0
	for _, transfer := range transfers {
		switch transfer.Status {
		case store.UploadTransferCompleted:
			completed++
		case store.UploadTransferFailed:
			failed++
		}
	}
	if completed != 2 || failed != 1 {
		t.Fatalf("completed=%d failed=%d transfers=%#v", completed, failed, transfers)
	}
	if err := st.RescheduleUploadTarget(ctx, target.ID, processErr.Error(), time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	fake.failUpload = nil
	retryTarget, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.processTarget(ctx, retryTarget); err != nil {
		t.Fatalf("retry target: %v", err)
	}
	if len(fake.attempts) != 4 || len(fake.uploads) != 3 {
		t.Fatalf("retry should upload only the failed transfer: attempts=%#v successful=%#v", fake.attempts, fake.uploads)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.Status != store.UploadBatchCompleted || detail.Targets[0].Status != store.UploadTargetCompleted {
		t.Fatalf("retry did not complete the batch: %#v", detail)
	}
}

func TestProcessTargetOnlyVerifiesUncertainCommitOnAutomaticRetry(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	root := t.TempDir()
	showPath := filepath.Join(root, "Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(showPath, "episode.mkv")
	if err := os.WriteFile(localPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(localPath)
	if err != nil {
		t.Fatal(err)
	}
	providerRecord, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Archive", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	dir.UploadConfigs = []store.UploadProviderRoute{{
		ProviderID: providerRecord.ID, Enabled: true, RemoteRoot: "/Archive", CollisionPolicy: "fail", IncludeTypes: []string{"video"},
	}}
	if _, err := st.UpdateWatchDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	batch, created, err := st.CollectUploadBatch(ctx, store.UploadCollectionInput{
		WatchDirID: &dir.ID, SeriesKey: "show", SeriesPath: showPath, QuietPeriod: time.Millisecond,
		Files: []store.UploadCandidate{{
			LocalPath: localPath, RelativePath: "Show/episode.mkv", FileType: "video", Size: info.Size(), ModifiedAt: info.ModTime(),
		}},
	})
	if err != nil || !created {
		t.Fatalf("collect batch: batch=%#v created=%v err=%v", batch, created, err)
	}
	if _, err := st.SealDueUploadBatches(ctx, time.Now(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	target, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{fail: &uncertain115CommitError{stage: "test PUT", err: errors.New("connection reset by peer")}}
	manager := NewWithFactory(Options{QuietPeriod: time.Millisecond, MaxAttempts: 3}, st, slog.Default(), func(context.Context, store.UploadBatchTarget) (Provider, error) {
		return fake, nil
	})
	firstErr := manager.processTarget(ctx, target)
	if firstErr == nil || !strings.Contains(firstErr.Error(), uncertain115CommitMarker) {
		t.Fatalf("first attempt error=%v", firstErr)
	}
	if err := st.RescheduleUploadTarget(ctx, target.ID, firstErr.Error(), time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}

	verifyCalls := 0
	fake.verifyFile = func(string, int64) (RemoteFile, bool, error) {
		verifyCalls++
		return RemoteFile{}, false, nil
	}
	retryTarget, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondErr := manager.processTarget(ctx, retryTarget)
	if secondErr == nil || !strings.Contains(secondErr.Error(), uncertain115CommitMarker) {
		t.Fatalf("verification-only retry error=%v", secondErr)
	}
	if len(fake.attempts) != 1 || verifyCalls != 1 {
		t.Fatalf("automatic retry replayed an uncertain upload: uploads=%d verifies=%d", len(fake.attempts), verifyCalls)
	}
	if err := st.RescheduleUploadTarget(ctx, retryTarget.ID, secondErr.Error(), time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	fake.verifyFile = func(_ string, size int64) (RemoteFile, bool, error) {
		verifyCalls++
		return RemoteFile{ID: "remote-episode", Size: size}, true, nil
	}
	finalTarget, err := st.ClaimNextUploadTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.processTarget(ctx, finalTarget); err != nil {
		t.Fatalf("remote verification should complete target: %v", err)
	}
	if len(fake.attempts) != 1 || verifyCalls != 2 {
		t.Fatalf("remote completion replayed upload: uploads=%d verifies=%d", len(fake.attempts), verifyCalls)
	}
	detail, err := st.GetUploadBatchDetail(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Batch.Status != store.UploadBatchCompleted || detail.Transfers[0].Status != store.UploadTransferCompleted {
		t.Fatalf("verified uncertain upload was not completed: %#v", detail)
	}
}

func TestRecordMediaProcessedRequiresExplicitWatchDirUploadConfig(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	root := t.TempDir()
	showPath := filepath.Join(root, "Show")
	if err := os.MkdirAll(showPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(showPath, "S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Unmapped", Type: store.UploadProviderType115Cookie, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	manager := NewWithFactory(Options{QuietPeriod: time.Millisecond}, st, slog.Default(), func(_ context.Context, target store.UploadBatchTarget) (Provider, error) {
		t.Fatalf("provider factory should not be used without a directory upload configuration: %#v", target)
		return nil, nil
	})
	info, err := os.Stat(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordMediaProcessed(ctx, store.Task{ID: 9}, store.MediaFile{Path: mediaPath, Size: info.Size(), ModifiedAt: info.ModTime()}); err != nil {
		t.Fatal(err)
	}
	result, err := st.ListUploadBatches(ctx, store.UploadBatchFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("provider without an explicit directory mapping created upload work: %#v", result)
	}
}

func TestCollectCandidatesSkipsSeriesArtifactsOutsideSeasonWatchRoot(t *testing.T) {
	showRoot := filepath.Join(t.TempDir(), "Show")
	watchRoot := filepath.Join(showRoot, "Season 01")
	if err := os.MkdirAll(watchRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(watchRoot, "S01E01.mkv")
	seasonNFO := filepath.Join(watchRoot, "season.nfo")
	showNFO := filepath.Join(showRoot, "tvshow.nfo")
	for path, content := range map[string]string{mediaPath: "video", seasonNFO: "season", showNFO: "show"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := collectCandidates(mediaPath, showRoot, watchRoot, []store.Artifact{
		{Path: seasonNFO, Type: "season-nfo"},
		{Path: showNFO, Type: "tvshow-nfo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		got[candidate.FileType] = candidate.RelativePath
	}
	if len(got) != 2 || got["video"] != "S01E01.mkv" || got["season-nfo"] != "season.nfo" {
		t.Fatalf("unexpected season-root candidates: %#v", candidates)
	}
	if _, ok := got["tvshow-nfo"]; ok {
		t.Fatalf("series artifact outside the watch root must be skipped: %#v", candidates)
	}
	if len(candidates) != 2 || candidates[0].RelativePath != "S01E01.mkv" || candidates[1].RelativePath != "season.nfo" {
		t.Fatalf("upload candidates are not in stable queue order: %#v", candidates)
	}
}

func openUploadTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "uploads.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		st.Close()
		t.Fatal(err)
	}
	return st
}
