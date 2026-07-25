package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"
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

func TestCookie115SmallFileUsesSingleOSSPut(t *testing.T) {
	if singleRequest115UploadLimit != 16*1024*1024 {
		t.Fatalf("single-request threshold=%d, want 16 MiB", singleRequest115UploadLimit)
	}

	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.UserID = 123456
	provider.client.Userkey = "upload-user-key"
	provider.client.UploadMetaInfo = &pan115.UploadMetaInfo{SizeLimit: singleRequest115UploadLimit * 2, UploadAllowed: true}
	provider.appVersion = fallback115AppVersion
	provider.appVersionResolved = true
	provider.newUploadCipher = func() (upload115Cipher, error) { return passthrough115UploadCipher{}, nil }
	provider.nowMilli = func() int64 { return 1721867000123 }
	provider.ossToken = &pan115.UploadOSSTokenResp{
		AccessKeyID:     "test-access-key",
		AccessKeySecret: "test-secret",
		SecurityToken:   "test-security-token",
		StatusCode:      "200",
	}
	provider.ossTokenExpiresAt = time.Now().Add(time.Hour)

	initRequests := 0
	provider.client.Client.SetTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		initRequests++
		if request.URL.Host != "uplb.115.com" {
			t.Fatalf("unexpected 115 request: %s", request.URL)
		}
		return jsonHTTPResponse(request, http.StatusOK, `{"status":1,"statuscode":0,"bucket":"test-bucket","object":"subtitle.srt","callback":{"callback":"test-callback","callback_var":"test-vars"}}`), nil
	}))

	ossRequests := 0
	provider.ossHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		ossRequests++
		if request.Method != http.MethodPut {
			t.Fatalf("OSS method=%s, want PUT", request.Method)
		}
		if request.URL.Query().Has("uploads") || request.URL.Query().Has("uploadId") {
			t.Fatalf("small upload unexpectedly used multipart: %s", request.URL)
		}
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatalf("read OSS body: %v", readErr)
		}
		if got := string(body); got != "subtitle payload" {
			t.Fatalf("OSS body=%q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})}

	file := writeCookie115TestFile(t, "subtitle payload")
	if err := provider.rapidUploadOrByMultipart(context.Background(), "42", "subtitle.srt", 16, file); err != nil {
		t.Fatalf("small upload: %v", err)
	}
	if initRequests != 1 || ossRequests != 1 {
		t.Fatalf("requests: init=%d OSS=%d, want one of each", initRequests, ossRequests)
	}
}

func TestMultipart115PartSizeForTypicalVideo(t *testing.T) {
	fileSize := int64(689 * 1024 * 1024)
	partSize := multipart115PartSizeFor(fileSize)
	if partSize != 16*1024*1024 {
		t.Fatalf("part size=%d, want 16 MiB", partSize)
	}
	partCount := (fileSize + partSize - 1) / partSize
	if partCount != 44 {
		t.Fatalf("689 MiB part count=%d, want 44", partCount)
	}

	// The adaptive branch must keep even very large files under OSS's
	// 10,000-part protocol limit without allocating or reading such a file.
	largeFileSize := int64(max115MultipartParts+1) * int64(multipart115PartSize)
	largePartSize := multipart115PartSizeFor(largeFileSize)
	largePartCount := (largeFileSize + largePartSize - 1) / largePartSize
	if largePartCount > max115MultipartParts {
		t.Fatalf("large file part count=%d exceeds %d", largePartCount, max115MultipartParts)
	}
}

func TestCookie115MultipartUploadUsesOneSessionAndSequentialParts(t *testing.T) {
	provider, err := newCookie115Provider("UID=user;CID=cid;SEID=seid;KID=kid", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.ossToken = &pan115.UploadOSSTokenResp{
		AccessKeyID:     "test-access-key",
		AccessKeySecret: "test-secret",
		SecurityToken:   "test-security-token",
		StatusCode:      "200",
	}
	provider.ossTokenExpiresAt = time.Now().Add(time.Hour)

	params := &pan115.UploadOSSParams{Bucket: "test-bucket", Object: "episode.mkv", SHA1: "TEST-SHA1"}
	params.Callback.Callback = "test-callback"
	params.Callback.CallbackVar = "test-callback-vars"

	fileSize := int64(multipart115PartSize) + 3
	file, err := os.CreateTemp(t.TempDir(), "cookie115-multipart-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := file.Truncate(fileSize); err != nil {
		t.Fatal(err)
	}

	initRequests := 0
	completeRequests := 0
	partNumbers := make([]string, 0, 2)
	events := make([]string, 0, 4)
	provider.ossHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		switch {
		case request.Method == http.MethodPost && query.Has("uploads"):
			initRequests++
			events = append(events, "init")
			if !query.Has("sequential") || !query.Has("x-oss-enable-sha1") {
				t.Fatalf("multipart init lacks sequential/SHA1 options: %s", request.URL.RawQuery)
			}
			return cookie115UploadHTTPResponse(request, "application/xml", `<InitiateMultipartUploadResult><Bucket>test-bucket</Bucket><Key>episode.mkv</Key><UploadId>upload-id</UploadId></InitiateMultipartUploadResult>`), nil

		case request.Method == http.MethodPut && query.Get("uploadId") == "upload-id":
			partNumber := query.Get("partNumber")
			partNumbers = append(partNumbers, partNumber)
			events = append(events, "part-"+partNumber)
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read OSS part %s: %v", partNumber, readErr)
			}
			var wantSize int64
			switch partNumber {
			case "1":
				wantSize = int64(multipart115PartSize)
			case "2":
				wantSize = 3
			default:
				t.Fatalf("unexpected part number %q", partNumber)
			}
			if int64(len(body)) != wantSize || request.ContentLength != wantSize {
				t.Fatalf("part %s sizes: body=%d content-length=%d, want %d", partNumber, len(body), request.ContentLength, wantSize)
			}
			if request.Header.Get("Content-MD5") == "" {
				t.Fatalf("part %s did not enable Content-MD5", partNumber)
			}
			if request.Header.Get("x-oss-callback") != "" || request.Header.Get("x-oss-callback-var") != "" {
				t.Fatalf("part %s unexpectedly carried completion callback headers", partNumber)
			}
			response := cookie115UploadHTTPResponse(request, "application/xml", "")
			response.Header.Set("ETag", `"etag-`+partNumber+`"`)
			return response, nil

		case request.Method == http.MethodPost && query.Get("uploadId") == "upload-id":
			completeRequests++
			events = append(events, "complete")
			if request.Header.Get("x-oss-callback") == "" || request.Header.Get("x-oss-callback-var") == "" {
				t.Fatal("multipart completion did not carry the 115 callback headers")
			}
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read multipart completion: %v", readErr)
			}
			completionXML := string(body)
			part1 := strings.Index(completionXML, "<PartNumber>1</PartNumber>")
			part2 := strings.Index(completionXML, "<PartNumber>2</PartNumber>")
			if part1 < 0 || part2 <= part1 {
				t.Fatalf("completion parts are missing or out of order: %s", completionXML)
			}
			return cookie115UploadHTTPResponse(request, "application/json", `{"state":true,"data":{"file_name":"episode.mkv"}}`), nil

		default:
			t.Fatalf("unexpected OSS request: %s %s", request.Method, request.URL)
			return nil, nil
		}
	})}

	if err := provider.uploadByOSSMultipart(context.Background(), params, fileSize, file); err != nil {
		t.Fatalf("multipart upload: %v", err)
	}
	if initRequests != 1 {
		t.Fatalf("multipart init requests=%d, want 1", initRequests)
	}
	if got := strings.Join(partNumbers, ","); got != "1,2" {
		t.Fatalf("part order=%q, want 1,2", got)
	}
	if completeRequests != 1 {
		t.Fatalf("multipart complete requests=%d, want 1", completeRequests)
	}
	if got := strings.Join(events, ","); got != "init,part-1,part-2,complete" {
		t.Fatalf("multipart request order=%q", got)
	}
}

func cookie115UploadHTTPResponse(request *http.Request, contentType string, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
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
