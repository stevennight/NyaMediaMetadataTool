package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

type fakeOpen115AuthRemote struct {
	startedClientID   string
	startedChallenge  string
	exchangedUID      string
	exchangedVerifier string
}

func (f *fakeOpen115AuthRemote) Start(_ context.Context, clientID, challenge string) (open115DeviceCode, error) {
	f.startedClientID = clientID
	f.startedChallenge = challenge
	return open115DeviceCode{UID: "uid-1", Time: 1234, QRCode: "https://115.com/scan/test", Sign: "sign-1"}, nil
}

func (f *fakeOpen115AuthRemote) Poll(context.Context, string, string, string) (open115QRStatus, error) {
	return open115QRStatus{State: json.RawMessage("true"), Status: 2, Msg: "confirmed"}, nil
}

func (f *fakeOpen115AuthRemote) Exchange(_ context.Context, uid, verifier string) (open115Tokens, error) {
	f.exchangedUID = uid
	f.exchangedVerifier = verifier
	return open115Tokens{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 3600}, nil
}

func TestOpen115AuthorizationPersistsCredentialsAndQRCode(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Open 115", Type: store.UploadProviderType115Open, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetUploadProviderCache(ctx, provider.ID, "node:/Stale", "stale"); err != nil {
		t.Fatal(err)
	}

	remote := &fakeOpen115AuthRemote{}
	originalFactory := newOpen115AuthRemote
	newOpen115AuthRemote = func(string) open115AuthRemote { return remote }
	t.Cleanup(func() { newOpen115AuthRemote = originalFactory })

	manager := New(st, slog.Default())
	started, err := manager.StartOpen115Auth(ctx, provider.ID, " client-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if started.State != "pending" || started.ClientID != "client-1" || remote.startedClientID != "client-1" || remote.startedChallenge == "" {
		t.Fatalf("unexpected start state: status=%#v remote=%#v", started, remote)
	}
	image, err := manager.Open115AuthQRCode(ctx, provider.ID, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(image, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("QR response is not PNG: %x", image[:min(8, len(image))])
	}

	completed, err := manager.PollOpen115Auth(ctx, provider.ID, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != "authorized" || remote.exchangedUID != "uid-1" || remote.exchangedVerifier == "" {
		t.Fatalf("unexpected completed state: status=%#v remote=%#v", completed, remote)
	}
	if _, ok, err := st.GetUploadProviderCache(ctx, provider.ID, "node:/Stale"); err != nil || ok {
		t.Fatalf("authorization did not clear provider cache: ok=%v err=%v", ok, err)
	}
	for key, want := range map[string]string{"client_id": "client-1", "access_token": "access-1", "refresh_token": "refresh-1"} {
		got, err := st.GetUploadProviderSecret(ctx, provider.ID, key)
		if err != nil || got != want {
			t.Fatalf("secret %s = %q, err=%v, want %q", key, got, err, want)
		}
	}
	expiresAt, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token_expires_at")
	if err != nil {
		t.Fatal(err)
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || expiry.Before(time.Now().Add(50*time.Minute)) {
		t.Fatalf("unexpected access token expiry: value=%q err=%v", expiresAt, err)
	}

	listed, err := st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !listed.HasCredentials {
		t.Fatalf("provider did not report saved credentials: %#v", listed)
	}

	if err := st.SetUploadProviderCache(ctx, provider.ID, "node:/Imported", "stale"); err != nil {
		t.Fatal(err)
	}
	imported, err := manager.ImportOpen115Tokens(ctx, provider.ID, "", "", "refresh-2")
	if err != nil {
		t.Fatal(err)
	}
	if !imported.HasCredentials {
		t.Fatalf("imported provider did not report credentials: %#v", imported)
	}
	if _, ok, err := st.GetUploadProviderCache(ctx, provider.ID, "node:/Imported"); err != nil || ok {
		t.Fatalf("token import did not clear provider cache: ok=%v err=%v", ok, err)
	}
	accessToken, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token")
	if err != nil || accessToken != "access-1" {
		t.Fatalf("partial import changed access token: value=%q err=%v", accessToken, err)
	}
	refreshToken, err := st.GetUploadProviderSecret(ctx, provider.ID, "refresh_token")
	if err != nil || refreshToken != "refresh-2" {
		t.Fatalf("partial import did not update refresh token: value=%q err=%v", refreshToken, err)
	}
	preservedExpiry, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token_expires_at")
	if err != nil || preservedExpiry != expiresAt {
		t.Fatalf("refresh-only import changed access token expiry: value=%q err=%v", preservedExpiry, err)
	}
	if _, err := manager.ImportOpen115Tokens(ctx, provider.ID, "", "access-2", ""); err != nil {
		t.Fatal(err)
	}
	clearedExpiry, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token_expires_at")
	if err != nil || clearedExpiry != "" {
		t.Fatalf("access token import did not clear stale expiry: value=%q err=%v", clearedExpiry, err)
	}
}
