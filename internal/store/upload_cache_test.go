package store

import (
	"context"
	"testing"
	"time"
)

func TestUploadProviderCacheIsScopedAndExpires(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	defer st.Close()
	first, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "Cache A", Type: UploadProviderType115Open, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUploadProvider(ctx, UploadProvider{Name: "Cache B", Type: UploadProviderType115Open, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCache(ctx, first.ID, "node:/Anime", "first"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCache(ctx, second.ID, "node:/Anime", "second"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := st.GetUploadProviderCache(ctx, first.ID, "node:/Anime"); err != nil || !ok || got != "first" {
		t.Fatalf("first cache=%q ok=%v err=%v", got, ok, err)
	}
	if err := st.SetUploadProviderCacheWithTTL(ctx, first.ID, "children:/Anime", "expired", time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if got, ok, err := st.GetUploadProviderCache(ctx, first.ID, "children:/Anime"); err != nil || ok || got != "" {
		t.Fatalf("expired cache=%q ok=%v err=%v", got, ok, err)
	}
	if err := st.DeleteUploadProviderCache(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.GetUploadProviderCache(ctx, first.ID, "node:/Anime"); err != nil || ok {
		t.Fatalf("deleted cache ok=%v err=%v", ok, err)
	}
	if got, ok, err := st.GetUploadProviderCache(ctx, second.ID, "node:/Anime"); err != nil || !ok || got != "second" {
		t.Fatalf("second cache=%q ok=%v err=%v", got, ok, err)
	}
}
