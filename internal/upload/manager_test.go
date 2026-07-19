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

	"NyaMediaMetadataTool/internal/config"
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
	if _, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true}); err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 A", Type: store.UploadProviderType115Cookie, Enabled: true, RemoteRoot: "/A"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 B", Type: store.UploadProviderType115Cookie, Enabled: true, RemoteRoot: "/B"})
	if err != nil {
		t.Fatal(err)
	}
	providers := map[int64]*fakeProvider{first.ID: {}, second.ID: {}}
	manager := NewWithFactory(config.UploadConfig{Enabled: true, Concurrency: 1, QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(_ context.Context, target store.UploadBatchTarget) (Provider, error) {
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
		wantRoot := "/A/"
		if providerID == second.ID {
			wantRoot = "/B/"
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
	if _, err := st.CreateWatchDir(ctx, store.WatchDir{Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true}); err != nil {
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
	providers := map[int64]*fakeProvider{okProvider.ID: {}, badProvider.ID: {fail: fmt.Errorf("network down")}}
	manager := NewWithFactory(config.UploadConfig{Enabled: true, QuietPeriod: time.Millisecond, MaxAttempts: 1}, st, slog.Default(), func(_ context.Context, target store.UploadBatchTarget) (Provider, error) {
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
