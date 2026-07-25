package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type passthrough115UploadCipher struct{}

func (passthrough115UploadCipher) EncodeToken(int64) (string, error) {
	return "test-k-ec", nil
}

func (passthrough115UploadCipher) Encrypt(plainText []byte) ([]byte, error) {
	return append([]byte(nil), plainText...), nil
}

func (passthrough115UploadCipher) Decrypt(cipherText []byte) ([]byte, error) {
	return append([]byte(nil), cipherText...), nil
}

func TestCookie115DynamicVersionDrivesUserAgentFormAndToken(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.UserID = 123456
	provider.client.Userkey = "upload-user-key"
	provider.newUploadCipher = func() (upload115Cipher, error) { return passthrough115UploadCipher{}, nil }
	provider.nowMilli = func() int64 { return 1721867000123 }

	versionRequests := 0
	initRequests := 0
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "appversion.115.com":
			versionRequests++
			return jsonHTTPResponse(request, http.StatusOK, `{"state":true,"data":{"win":{"version_code":"36.1.2.3"}}}`), nil
		case "uplb.115.com":
			initRequests++
			form := read115UploadForm(t, request)
			if got := request.Header.Get("User-Agent"); got != "Mozilla/5.0 115Browser/36.1.2.3" {
				t.Fatalf("upload User-Agent=%q", got)
			}
			if got := form.Get("appversion"); got != "36.1.2.3" {
				t.Fatalf("appversion=%q", got)
			}
			if got := form.Get("token"); got != "bae4007bbaddbd7f7bd60cf511114e19" {
				t.Fatalf("upload token=%q", got)
			}
			if got := form.Get("filename"); got != "episode.mkv" {
				t.Fatalf("filename=%q", got)
			}
			if got := form.Get("target"); got != "U_1_42" {
				t.Fatalf("target=%q", got)
			}
			if _, exists := form["topupload"]; exists {
				t.Fatalf("legacy topupload was sent: %v", form["topupload"])
			}
			if got := request.URL.Query().Get("k_ec"); got != "test-k-ec" {
				t.Fatalf("k_ec=%q", got)
			}
			return jsonHTTPResponse(request, http.StatusOK, `{"status":2,"statuscode":0,"pickcode":"pick-code"}`), nil
		default:
			t.Fatalf("unexpected 115 endpoint: %s", request.URL)
			return nil, nil
		}
	}))

	version, err := provider.resolve115AppVersion(context.Background())
	if err != nil {
		t.Fatalf("resolve app version: %v", err)
	}
	if version != "36.1.2.3" {
		t.Fatalf("version=%q", version)
	}
	result, err := provider.rapidUpload(context.Background(), 689000000, "episode.mkv", "42", "PREID", "ABCDEF", version, strings.NewReader("content"))
	if err != nil {
		t.Fatalf("rapid upload init: %v", err)
	}
	if result.SHA1 != "ABCDEF" || result.PickCode != "pick-code" {
		t.Fatalf("unexpected init result: %#v", result)
	}
	if versionRequests != 1 || initRequests != 1 {
		t.Fatalf("requests: version=%d init=%d", versionRequests, initRequests)
	}

	secondVersion, secondErr := provider.resolve115AppVersion(context.Background())
	if secondErr != nil || secondVersion != version || versionRequests != 1 {
		t.Fatalf("cached version=(%q, %v), requests=%d", secondVersion, secondErr, versionRequests)
	}
}

func TestCookie115AppVersionFallsBackAndKeepsExplicitUserAgent(t *testing.T) {
	t.Run("endpoint failure uses safe fallback", func(t *testing.T) {
		provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
		if err != nil {
			t.Fatal(err)
		}
		requests := 0
		provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			return jsonHTTPResponse(request, http.StatusServiceUnavailable, ""), nil
		}))

		version, resolveErr := provider.resolve115AppVersion(context.Background())
		if resolveErr == nil {
			t.Fatal("version endpoint failure should be reported")
		}
		if version != fallback115AppVersion {
			t.Fatalf("fallback version=%q", version)
		}
		if got := provider.client.Client.Header.Get("User-Agent"); got != default115UserAgent {
			t.Fatalf("fallback User-Agent=%q", got)
		}
		if requests != 1 {
			t.Fatalf("version requests=%d", requests)
		}
	})

	t.Run("explicit user agent remains an override", func(t *testing.T) {
		provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "NyaMedia/Test")
		if err != nil {
			t.Fatal(err)
		}
		provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return jsonHTTPResponse(request, http.StatusOK, `{"data":{"win":{"version_code":"36.1.2.3"}}}`), nil
		}))
		if _, err := provider.resolve115AppVersion(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := provider.client.Client.Header.Get("User-Agent"); got != "NyaMedia/Test" {
			t.Fatalf("explicit User-Agent=%q", got)
		}
	})
}

func TestGenerate115UploadTokenUsesSelectedAppVersion(t *testing.T) {
	got := generate115UploadToken(123456, "ABCDEF", "1721867000123", "689000000", "sign-key", "SIGNVALUE", "35.6.0.3")
	if got != "aa3a11b0f275600ea6e30232985a6200" {
		t.Fatalf("token=%q", got)
	}
}

func TestCookie115UploadInitCompletesSignChallenge(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.UserID = 123456
	provider.client.Userkey = "upload-user-key"
	provider.newUploadCipher = func() (upload115Cipher, error) { return passthrough115UploadCipher{}, nil }
	provider.nowMilli = func() int64 { return 1721867000123 }

	forms := make([]url.Values, 0, 2)
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		forms = append(forms, read115UploadForm(t, request))
		if len(forms) == 1 {
			return jsonHTTPResponse(request, http.StatusOK, `{"status":7,"statuscode":0,"sign_key":"range-key","sign_check":"1-3"}`), nil
		}
		return jsonHTTPResponse(request, http.StatusOK, `{"status":2,"statuscode":0,"pickcode":"matched"}`), nil
	}))

	result, err := provider.rapidUpload(context.Background(), 6, "small.bin", "7", "PREID", "FILEID", fallback115AppVersion, strings.NewReader("abcdef"))
	if err != nil {
		t.Fatalf("rapid upload sign challenge: %v", err)
	}
	if result.PickCode != "matched" || len(forms) != 2 {
		t.Fatalf("result=%#v forms=%d", result, len(forms))
	}
	if forms[0].Get("sign_key") != "" || forms[0].Get("sign_val") != "" {
		t.Fatalf("initial request unexpectedly included a range signature: %v", forms[0])
	}
	if got := forms[1].Get("sign_key"); got != "range-key" {
		t.Fatalf("sign_key=%q", got)
	}
	if got := forms[1].Get("sign_val"); got != "924F61661A3472DA74307A35F2C8D22E07E84A4D" {
		t.Fatalf("sign_val=%q", got)
	}
	wantToken := generate115UploadToken(123456, "FILEID", "1721867000123", "6", "range-key", "924F61661A3472DA74307A35F2C8D22E07E84A4D", fallback115AppVersion)
	if got := forms[1].Get("token"); got != wantToken {
		t.Fatalf("challenge token=%q, want %q", got, wantToken)
	}
}

func TestCookie115UploadInitHonorsContextCancellation(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.UserID = 123456
	provider.client.Userkey = "upload-user-key"
	provider.newUploadCipher = func() (upload115Cipher, error) { return passthrough115UploadCipher{}, nil }
	provider.nowMilli = func() int64 { return 1721867000123 }

	requestStarted := make(chan struct{})
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-requestStarted
		cancel()
	}()

	_, err = provider.rapidUpload(ctx, 6, "small.bin", "7", "PREID", "FILEID", fallback115AppVersion, strings.NewReader("abcdef"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rapid upload error=%v, want context canceled", err)
	}
}

func read115UploadForm(t *testing.T, request *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read upload init body: %v", err)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse upload init body: %v", err)
	}
	return form
}
