package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/store"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCookie115CheckUsesFileAPI(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != "webapi.115.com" || request.URL.Path != "/files" {
			t.Fatalf("unexpected check endpoint: %s", request.URL)
		}
		if request.URL.Query().Get("cid") != "0" || request.URL.Query().Get("limit") != "1" {
			t.Fatalf("unexpected check query: %s", request.URL.RawQuery)
		}
		for _, name := range []string{"UID", "CID", "SEID", "KID"} {
			if cookie, cookieErr := request.Cookie(name); cookieErr != nil || cookie.Value == "" {
				t.Fatalf("missing %s Cookie: cookie=%#v err=%v", name, cookie, cookieErr)
			}
		}
		return jsonHTTPResponse(request, http.StatusOK, `{"state":true,"cid":"0","count":0,"offset":0,"data":[]}`), nil
	}))

	if err := provider.Check(context.Background()); err != nil {
		t.Fatalf("operational file API check failed: %v", err)
	}
}

func TestCookie115CheckPreservesAuthenticationError(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(request, http.StatusOK, `{"state":false,"errno":99,"error":"not logged in"}`), nil
	}))

	err = provider.Check(context.Background())
	if err == nil {
		t.Fatal("authentication failure should fail the provider check")
	}
	if strings.Contains(strings.ToLower(err.Error()), "bad cookie") {
		t.Fatalf("provider check collapsed the real error into bad cookie: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not login") {
		t.Fatalf("provider check did not preserve the authentication error: %v", err)
	}
}

func TestCheck115FileAPIRejectsMalformedResponse(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(request, http.StatusOK, `not-json`), nil
	}))

	err = provider.check115FileAPI(context.Background())
	if err == nil {
		t.Fatal("malformed response should not pass the provider check")
	}
	if strings.Contains(strings.ToLower(err.Error()), "bad cookie") {
		t.Fatalf("malformed response was reported as bad cookie: %v", err)
	}
}

func TestCheck115FileAPIHonorsContext(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = provider.check115FileAPI(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("check error=%v, want context canceled", err)
	}
}

func TestCookie115CheckRetriesHTTPStatusErrors(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.requestInterval = func() time.Duration { return 0 }
	provider.uploadRetryDelay = func(int) time.Duration { return 0 }
	requests := 0
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if requests < 3 {
			return jsonHTTPResponse(request, http.StatusServiceUnavailable, ""), nil
		}
		return jsonHTTPResponse(request, http.StatusOK, `{"state":true,"cid":"0","count":0,"offset":0,"data":[]}`), nil
	}))

	if err := provider.Check(context.Background()); err != nil {
		t.Fatalf("provider check should retry HTTP 503: %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests=%d, want 3", requests)
	}
}

func TestCookie115ListPageReportsHTML405WithoutLeakingResponseBody(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.requestInterval = func() time.Duration { return 0 }
	provider.uploadRetryDelay = func(int) time.Duration { return 0 }
	provider.requestGuard.rateLimitDelayFunc = func(int) time.Duration { return 0 }
	requests := 0
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		response := jsonHTTPResponse(request, http.StatusMethodNotAllowed, `<!doctype html><title>405</title><p>blocked by WAF</p>`)
		response.Header.Set("Content-Type", "text/html; charset=utf-8")
		return response, nil
	}))

	_, err = provider.listPage(context.Background(), "3480384768003015802", "test path", 0)
	if err == nil {
		t.Fatal("HTML 405 should fail the directory request")
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "http 405") {
		t.Fatalf("directory error=%v, want structured HTTP status", err)
	}
	if strings.Contains(message, "<!doctype") || strings.Contains(message, "unexpected error") {
		t.Fatalf("directory error leaked the WAF page instead of reporting the status: %v", err)
	}
	if requests != max115ListRetries {
		t.Fatalf("requests=%d, want %d retries", requests, max115ListRetries)
	}
}

func TestCookie115RequestGuardRechecksCooldownWhileWaiting(t *testing.T) {
	guard := newCookie115RequestGuard()
	guard.lastRequest = time.Now()
	guard.rateLimitDelayFunc = func(int) time.Duration { return 80 * time.Millisecond }

	waitStarted := make(chan struct{}, 1)
	waitDone := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		waitDone <- guard.wait(context.Background(), 20*time.Millisecond, func(_ string, until time.Time) {
			if !until.IsZero() {
				select {
				case waitStarted <- struct{}{}:
				default:
				}
			}
		})
	}()

	select {
	case <-waitStarted:
	case <-time.After(time.Second):
		t.Fatal("request guard did not enter its initial interval wait")
	}
	guard.observeHTTPStatus(http.StatusMethodNotAllowed, 0)

	select {
	case err := <-waitDone:
		t.Fatalf("request guard ignored a cooldown observed while waiting: %v", err)
	case <-time.After(45 * time.Millisecond):
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request guard did not finish after the adaptive cooldown")
	}
	if elapsed := time.Since(startedAt); elapsed < 65*time.Millisecond {
		t.Fatalf("request guard waited only %s after an 80ms cooldown", elapsed)
	}
}

func TestRateLimit115CooldownAndRetryAfter(t *testing.T) {
	wantCooldowns := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}
	for index, want := range wantCooldowns {
		if got := rateLimit115Cooldown(index + 1); got != want {
			t.Fatalf("strike %d cooldown=%s, want %s", index+1, got, want)
		}
	}

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	if got := parse115RetryAfter("90", now); got != 90*time.Second {
		t.Fatalf("numeric Retry-After=%s", got)
	}
	if got := parse115RetryAfter(now.Add(2*time.Minute).Format(http.TimeFormat), now); got != 2*time.Minute {
		t.Fatalf("date Retry-After=%s", got)
	}
	if got := parse115RetryAfter("invalid", now); got != 0 {
		t.Fatalf("invalid Retry-After=%s", got)
	}
}

func TestRetryable115ErrorUsesStructuredHTTPStatus(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		if !isRetryable115Error(&http115StatusError{statusCode: status}) {
			t.Fatalf("HTTP %d should be retryable", status)
		}
	}
	if isRetryable115Error(errors.New("permanent error 50028")) {
		t.Fatal("business error codes must not be interpreted as HTTP status codes")
	}
	if !isRetryable115Error(oss.ServiceError{StatusCode: http.StatusServiceUnavailable}) {
		t.Fatal("OSS HTTP 503 should be retryable")
	}
}

func TestCookie115EnsureDirectoryCachesResolvedIDs(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}

	lookups := make([]string, 0, 2)
	provider.lookupChild = func(_ context.Context, parentID string, name string) (pan115.File, bool, error) {
		lookups = append(lookups, parentID+"/"+name)
		switch parentID + "/" + name {
		case "0/Anime":
			return pan115.File{FileID: "anime-id", Name: name, IsDirectory: true}, true, nil
		case "anime-id/Season 1":
			return pan115.File{FileID: "season-id", Name: name, IsDirectory: true}, true, nil
		default:
			return pan115.File{}, false, nil
		}
	}

	for call := 0; call < 2; call++ {
		id, ensureErr := provider.ensureDirectory(context.Background(), "/Anime/Season 1")
		if ensureErr != nil {
			t.Fatalf("ensure directory call %d: %v", call+1, ensureErr)
		}
		if id != "season-id" {
			t.Fatalf("ensure directory call %d returned id %q", call+1, id)
		}
	}

	if got := strings.Join(lookups, ","); got != "0/Anime,anime-id/Season 1" {
		t.Fatalf("directory lookups=%q; cached second resolution should not query 115", got)
	}
	if id, ok := provider.cachedDirectoryID("/Anime"); !ok || id != "anime-id" {
		t.Fatalf("cached /Anime=(%q, %v)", id, ok)
	}
	if id, ok := provider.cachedDirectoryID("/Anime/Season 1"); !ok || id != "season-id" {
		t.Fatalf("cached season=(%q, %v)", id, ok)
	}
}

func TestDecide115CollisionUsesContentSHA1(t *testing.T) {
	existing := pan115.File{FileID: "remote-id", Name: "episode.mkv", Size: 100, Sha1: "ABC123"}
	tests := []struct {
		name      string
		localSize int64
		localSHA1 string
		policy    string
		outcome   string
		replace   bool
		wantErr   bool
	}{
		{name: "identical content", localSize: 100, localSHA1: "abc123", policy: "replace", outcome: store.UploadOutcomeUnchanged},
		{name: "same size different content replace", localSize: 100, localSHA1: "DIFFERENT", policy: "replace", outcome: store.UploadOutcomeReplaced, replace: true},
		{name: "same size different content skip", localSize: 100, localSHA1: "DIFFERENT", policy: "skip", outcome: store.UploadOutcomeSkipped},
		{name: "different size replace", localSize: 200, localSHA1: "", policy: "replace", outcome: store.UploadOutcomeReplaced, replace: true},
		{name: "missing remote hash cannot prove equality", localSize: 100, localSHA1: "ABC123", policy: "fail", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := existing
			if test.name == "missing remote hash cannot prove equality" {
				candidate.Sha1 = ""
			}
			decision, err := decide115Collision(candidate, test.localSize, test.localSHA1, test.policy)
			if (err != nil) != test.wantErr {
				t.Fatalf("decision=%#v err=%v wantErr=%v", decision, err, test.wantErr)
			}
			if decision.outcome != test.outcome || decision.replace != test.replace {
				t.Fatalf("decision=%#v, want outcome=%q replace=%v", decision, test.outcome, test.replace)
			}
		})
	}
}

func TestCookie115UploadMarksIdenticalRemoteAsUnchanged(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	file := writeCookie115TestFile(t, "same payload")
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := provider.client.GetDigestResult(strings.NewReader("same payload"))
	if err != nil {
		t.Fatal(err)
	}
	provider.lookupChild = func(_ context.Context, parentID string, name string) (pan115.File, bool, error) {
		if parentID != "0" || name != "episode.mkv" {
			t.Fatalf("unexpected lookup %s/%s", parentID, name)
		}
		return pan115.File{FileID: "remote-id", ParentID: parentID, Name: name, Size: info.Size(), Sha1: digest.QuickID}, true, nil
	}
	provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
		t.Fatal("identical remote content must not be uploaded")
		return nil
	}

	remote, err := provider.Upload(context.Background(), file.Name(), "/episode.mkv", info.Size(), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.Outcome != store.UploadOutcomeUnchanged || remote.LocalSHA1 != digest.QuickID || remote.SHA1 != digest.QuickID {
		t.Fatalf("remote result=%#v", remote)
	}
}

func TestCookie115UploadUsesCachedSHA1WithoutReadingFile(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.lookupChild = func(_ context.Context, _ string, name string) (pan115.File, bool, error) {
		return pan115.File{FileID: "remote-id", Name: name, Size: 100, Sha1: "CACHED-SHA1"}, true, nil
	}

	remote, err := provider.Upload(context.Background(), filepath.Join(t.TempDir(), "missing.mkv"), "/episode.mkv", 100, "CACHED-SHA1", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.Outcome != store.UploadOutcomeUnchanged {
		t.Fatalf("remote result=%#v", remote)
	}
}

func TestCookie115UploadAndVerifyRetriesTransientErrors(t *testing.T) {
	t.Run("stops after maximum attempts", func(t *testing.T) {
		provider := &cookie115Provider{}
		uploadAttempts := 0
		remoteLookups := 0
		provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
			uploadAttempts++
			return errors.New("connection reset by peer")
		}
		provider.lookupChild = func(context.Context, string, string) (pan115.File, bool, error) {
			remoteLookups++
			return pan115.File{}, false, nil
		}
		provider.uploadRetryDelay = func(int) time.Duration { return 0 }

		file := writeCookie115TestFile(t, "retry payload")
		_, uploadErr := provider.uploadAndVerify(context.Background(), "parent", "episode.mkv", 13, file)
		if uploadErr == nil || !strings.Contains(uploadErr.Error(), "failed after 3 attempts") {
			t.Fatalf("upload error=%v, want exhausted retry error", uploadErr)
		}
		if uploadAttempts != max115UploadAttempts {
			t.Fatalf("upload attempts=%d, want %d", uploadAttempts, max115UploadAttempts)
		}
		if remoteLookups < max115UploadAttempts-1 {
			t.Fatalf("remote lookups=%d, want a check before each retry", remoteLookups)
		}
	})

	t.Run("finds completed remote file before retry", func(t *testing.T) {
		provider := &cookie115Provider{}
		uploadAttempts := 0
		remoteLookups := 0
		provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
			uploadAttempts++
			return errors.New("connection reset by peer")
		}
		provider.lookupChild = func(_ context.Context, parentID string, name string) (pan115.File, bool, error) {
			remoteLookups++
			return pan115.File{FileID: "remote-id", ParentID: parentID, Name: name, Size: 13}, true, nil
		}
		provider.uploadRetryDelay = func(int) time.Duration {
			t.Fatal("successful remote verification must not wait for a retry")
			return 0
		}

		file := writeCookie115TestFile(t, "retry payload")
		remote, uploadErr := provider.uploadAndVerify(context.Background(), "parent", "episode.mkv", 13, file)
		if uploadErr != nil {
			t.Fatalf("upload should accept the remotely completed file: %v", uploadErr)
		}
		if remote.ID != "remote-id" || remote.Size != 13 {
			t.Fatalf("remote file=%#v", remote)
		}
		if uploadAttempts != 1 || remoteLookups != 1 {
			t.Fatalf("upload attempts=%d lookups=%d, want one of each", uploadAttempts, remoteLookups)
		}
	})
}

func TestCookie115UploadAndVerifyDoesNotRetryCancellation(t *testing.T) {
	provider := &cookie115Provider{}
	uploadAttempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
		uploadAttempts++
		cancel()
		return errors.New("temporary timeout")
	}
	provider.lookupChild = func(context.Context, string, string) (pan115.File, bool, error) {
		t.Fatal("canceled upload must not perform retry verification")
		return pan115.File{}, false, nil
	}
	provider.uploadRetryDelay = func(int) time.Duration {
		t.Fatal("canceled upload must not wait for a retry")
		return 0
	}

	file := writeCookie115TestFile(t, "cancel payload")
	_, uploadErr := provider.uploadAndVerify(ctx, "parent", "episode.mkv", 14, file)
	if !errors.Is(uploadErr, context.Canceled) {
		t.Fatalf("upload error=%v, want context canceled", uploadErr)
	}
	if uploadAttempts != 1 {
		t.Fatalf("upload attempts=%d, want 1", uploadAttempts)
	}
}

func TestCookie115UploadAndVerifyDoesNotReplayUncertainCommit(t *testing.T) {
	provider := &cookie115Provider{}
	uploadAttempts := 0
	remoteLookups := 0
	provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
		uploadAttempts++
		return &uncertain115CommitError{stage: "test commit", err: errors.New("connection reset by peer")}
	}
	provider.lookupChild = func(context.Context, string, string) (pan115.File, bool, error) {
		remoteLookups++
		return pan115.File{}, false, errors.New("not logged in")
	}
	provider.uploadRetryDelay = func(int) time.Duration {
		t.Fatal("uncertain commit must not be replayed")
		return 0
	}

	file := writeCookie115TestFile(t, "uncertain payload")
	_, uploadErr := provider.uploadAndVerify(context.Background(), "parent", "episode.mkv", 17, file)
	if uploadErr == nil || !strings.Contains(uploadErr.Error(), uncertain115CommitMarker) {
		t.Fatalf("upload error=%v, want uncertain result", uploadErr)
	}
	if uploadAttempts != 1 || remoteLookups != 1 {
		t.Fatalf("upload attempts=%d lookups=%d, want no replay", uploadAttempts, remoteLookups)
	}
}

func TestCookie115UploadAndVerifyRetriesPreCommitDialFailure(t *testing.T) {
	provider := &cookie115Provider{}
	uploadAttempts := 0
	remoteLookups := 0
	provider.uploadContent = func(context.Context, string, string, int64, *os.File) error {
		uploadAttempts++
		return fmt.Errorf("connect to OSS before upload: %w", &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: os.ErrDeadlineExceeded,
		})
	}
	provider.lookupChild = func(context.Context, string, string) (pan115.File, bool, error) {
		remoteLookups++
		return pan115.File{}, false, nil
	}
	provider.uploadRetryDelay = func(int) time.Duration { return 0 }

	file := writeCookie115TestFile(t, "retry dial payload")
	_, uploadErr := provider.uploadAndVerify(context.Background(), "parent", "episode.mkv", 18, file)
	if uploadErr == nil || !strings.Contains(uploadErr.Error(), "failed after 3 attempts") {
		t.Fatalf("upload error=%v, want exhausted retry error", uploadErr)
	}
	if uploadAttempts != max115UploadAttempts {
		t.Fatalf("upload attempts=%d, want %d", uploadAttempts, max115UploadAttempts)
	}
	if remoteLookups != max115UploadAttempts {
		t.Fatalf("remote lookups=%d, want %d", remoteLookups, max115UploadAttempts)
	}
}

func TestFormat115PreCommitConnectionErrorExplainsProxyFakeIP(t *testing.T) {
	err := format115PreCommitConnectionError(&net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(198, 18, 2, 162), Port: 80},
		Err:  os.ErrDeadlineExceeded,
	})
	if !strings.Contains(err.Error(), "proxy fake-IP 198.18.2.162") {
		t.Fatalf("error=%q, want proxy fake-IP hint", err)
	}
}

func TestCookie115ListPageRejectsMismatchedDirectoryID(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.requestInterval = func() time.Duration { return 0 }
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(request, http.StatusOK, `{"state":true,"cid":"wrong-directory","count":0,"data":[]}`), nil
	}))

	_, err = provider.listPage(context.Background(), "expected-directory", "test path", 0)
	if err == nil || !strings.Contains(err.Error(), "does not match requested CID") {
		t.Fatalf("list page error=%v, want CID mismatch", err)
	}
}

func writeCookie115TestFile(t *testing.T, contents string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "cookie115-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file
}

func jsonHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
