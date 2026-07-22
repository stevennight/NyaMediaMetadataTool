package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
