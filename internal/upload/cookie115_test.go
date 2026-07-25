package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

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

	err = check115FileAPI(context.Background(), provider.client)
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

	err = check115FileAPI(ctx, provider.client)
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

func TestRetryable115ErrorUsesStructuredHTTPStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
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
