package upload

import (
	"context"
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
	mu      sync.Mutex
	uploads []fakeUpload
	fail    error
}

type fakeUpload struct {
	LocalPath  string
	RemotePath string
	Size       int64
}

func (p *fakeProvider) Check(context.Context) error { return nil }

func (p *fakeProvider) List(context.Context, string) ([]RemoteEntry, error) { return nil, nil }

func (p *fakeProvider) Upload(_ context.Context, localPath string, remotePath string, size int64, _ string) (RemoteFile, error) {
	if p.fail != nil {
		return RemoteFile{}, p.fail
	}
	p.mu.Lock()
	p.uploads = append(p.uploads, fakeUpload{LocalPath: localPath, RemotePath: remotePath, Size: size})
	p.mu.Unlock()
	return RemoteFile{ID: "remote-" + filepath.Base(remotePath), Size: size}, nil
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
	if summary.Completed != 1 {
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
	for range 2 {
		target, err := st.ClaimNextUploadTarget(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = manager.processTarget(ctx, target)
		if err != nil {
			if err := st.FailUploadTarget(ctx, target.ID, err.Error()); err != nil {
				t.Fatal(err)
			}
		}
	}
	summary, err := st.GetUploadSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Completed != 0 || summary.Failed != 1 || len(providers[okProvider.ID].uploads) != 1 {
		t.Fatalf("unexpected terminal states: summary=%#v good=%#v", summary, providers[okProvider.ID].uploads)
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
