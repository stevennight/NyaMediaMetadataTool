package upload

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"

	"NyaMediaMetadataTool/internal/store"
)

type blockingCookie115AuthClient struct {
	mu            sync.Mutex
	statusEntered chan struct{}
	releaseStatus chan struct{}
	startCalls    int
	statusCalls   int
	loginCalls    int
	loginApp      pan115.LoginApp
}

func (c *blockingCookie115AuthClient) QRCodeStart() (*pan115.QRCodeSession, error) {
	c.mu.Lock()
	c.startCalls++
	c.mu.Unlock()
	return &pan115.QRCodeSession{UID: "qr-user", Sign: "sign", Time: 1, QrcodeContent: "qr"}, nil
}

func (c *blockingCookie115AuthClient) QRCodeStatus(*pan115.QRCodeSession) (*pan115.QRCodeStatus, error) {
	c.mu.Lock()
	c.statusCalls++
	call := c.statusCalls
	c.mu.Unlock()
	c.statusEntered <- struct{}{}
	<-c.releaseStatus
	if call > 1 {
		return nil, errors.New("late QR status failure")
	}
	return &pan115.QRCodeStatus{Status: 2}, nil
}

func (c *blockingCookie115AuthClient) QRCodeLoginWithApp(_ *pan115.QRCodeSession, app pan115.LoginApp) (*pan115.Credential, error) {
	c.mu.Lock()
	c.loginCalls++
	c.loginApp = app
	c.mu.Unlock()
	return &pan115.Credential{UID: "user", CID: "cid", SEID: "seid", KID: "kid"}, nil
}

func (c *blockingCookie115AuthClient) calls() (start int, status int, login int, app pan115.LoginApp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startCalls, c.statusCalls, c.loginCalls, c.loginApp
}

func TestPollCookie115AuthClaimsSessionDuringCredentialExchange(t *testing.T) {
	ctx := context.Background()
	st := openUploadTestStore(t)
	defer st.Close()
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 QR", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	client := &blockingCookie115AuthClient{
		statusEntered: make(chan struct{}, 2),
		releaseStatus: make(chan struct{}),
	}
	originalFactory := newCookie115AuthClient
	newCookie115AuthClient = func() cookie115AuthClient { return client }
	t.Cleanup(func() { newCookie115AuthClient = originalFactory })

	manager := New(st, slog.Default())
	started, err := manager.StartCookie115Auth(ctx, provider.ID, "ios")
	if err != nil {
		t.Fatal(err)
	}
	type pollResult struct {
		status Cookie115AuthStatus
		err    error
	}
	firstResult := make(chan pollResult, 1)
	go func() {
		status, err := manager.PollCookie115Auth(ctx, provider.ID, started.SessionID)
		firstResult <- pollResult{status: status, err: err}
	}()

	select {
	case <-client.statusEntered:
	case <-time.After(time.Second):
		t.Fatal("first Poll did not enter QRCodeStatus")
	}

	secondResult := make(chan pollResult, 1)
	go func() {
		status, err := manager.PollCookie115Auth(ctx, provider.ID, started.SessionID)
		secondResult <- pollResult{status: status, err: err}
	}()
	var overlapping pollResult
	select {
	case overlapping = <-secondResult:
	case <-time.After(time.Second):
		close(client.releaseStatus)
		t.Fatal("overlapping Poll waited for or repeated the remote exchange")
	}
	if overlapping.err != nil || overlapping.status.State != "pending" {
		t.Fatalf("overlapping Poll returned status=%#v err=%v", overlapping.status, overlapping.err)
	}
	if _, statusCalls, loginCalls, _ := client.calls(); statusCalls != 1 || loginCalls != 0 {
		t.Fatalf("overlapping Poll reached remote auth: statusCalls=%d loginCalls=%d", statusCalls, loginCalls)
	}

	close(client.releaseStatus)
	var authorized pollResult
	select {
	case authorized = <-firstResult:
	case <-time.After(2 * time.Second):
		t.Fatal("claimed Poll did not complete")
	}
	if authorized.err != nil || authorized.status.State != "authorized" {
		t.Fatalf("claimed Poll returned status=%#v err=%v", authorized.status, authorized.err)
	}
	if startCalls, statusCalls, loginCalls, app := client.calls(); startCalls != 1 || statusCalls != 1 || loginCalls != 1 || app != pan115.LoginAppIOS {
		t.Fatalf("unexpected remote auth calls: start=%d status=%d login=%d app=%q", startCalls, statusCalls, loginCalls, app)
	}

	terminal, err := manager.PollCookie115Auth(ctx, provider.ID, started.SessionID)
	if err != nil || terminal.State != "authorized" {
		t.Fatalf("terminal Poll returned status=%#v err=%v", terminal, err)
	}
	if _, statusCalls, loginCalls, _ := client.calls(); statusCalls != 1 || loginCalls != 1 {
		t.Fatalf("terminal Poll repeated remote auth: statusCalls=%d loginCalls=%d", statusCalls, loginCalls)
	}
	persisted, err := st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.HasCookie || persisted.AuthDevice != "ios" || cookie != "UID=user;CID=cid;SEID=seid;KID=kid" {
		t.Fatalf("authorization was not persisted once with its device: provider=%#v cookie=%q", persisted, cookie)
	}
}
