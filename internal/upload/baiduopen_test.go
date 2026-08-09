package upload

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type baiduOpenRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn baiduOpenRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func baiduOpenJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d test", status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestBaiduOpenUploadCreatesDirectoryUploadsAndVerifies(t *testing.T) {
	content := []byte("baidu-open-upload")
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fullMD5 := md5.Sum(content)
	md5Text := hex.EncodeToString(fullMD5[:])
	provider, err := newBaiduOpenProvider("client", "secret", "access", "refresh", "2099-01-01T00:00:00Z", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []string
	animeListCount := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, req.URL.Path+"?"+req.URL.RawQuery)
		mu.Unlock()
		if req.URL.Path == "/rest/2.0/xpan/file" {
			switch req.URL.Query().Get("method") {
			case "list":
				switch req.URL.Query().Get("dir") {
				case "/":
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
				case "/Anime":
					animeListCount++
					if animeListCount == 1 {
						return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
					}
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":20,"path":"/Anime/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":`+strconv.Itoa(len(content))+`,"md5":"`+md5Text+`"}]}`), nil
				default:
					return nil, fmt.Errorf("unexpected list dir %q", req.URL.Query().Get("dir"))
				}
			case "create":
				form, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(form))
				if values.Get("isdir") == "1" {
					if values.Get("path") != "/Anime" {
						return nil, fmt.Errorf("mkdir path = %q", values.Get("path"))
					}
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":10,"path":"/Anime"}`), nil
				}
				if values.Get("uploadid") != "upload-1" || values.Get("path") != "/Anime/episode.mkv" {
					return nil, fmt.Errorf("create form = %s", form)
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/Anime/episode.mkv"}`), nil
			case "precreate":
				form, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(form))
				var blockList []string
				if err := json.Unmarshal([]byte(values.Get("block_list")), &blockList); err != nil || len(blockList) != 1 || blockList[0] != md5Text {
					return nil, fmt.Errorf("precreate block_list = %q", values.Get("block_list"))
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":2,"block_list":[0]}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		}
		if req.URL.Path == "/rest/2.0/pcs/superfile2" {
			if req.URL.Query().Get("method") != "upload" || req.URL.Query().Get("partseq") != "0" || req.URL.Query().Get("uploadid") != "upload-1" {
				return nil, fmt.Errorf("upload query = %s", req.URL.RawQuery)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		}
		return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
	})

	remote, err := provider.Upload(context.Background(), localPath, "/Anime/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "20" || remote.Size != int64(len(content)) || remote.Outcome != "created" || remote.LocalSHA1 == "" {
		t.Fatalf("remote = %#v", remote)
	}
	if len(requests) != 7 {
		t.Fatalf("requests = %d (%v)", len(requests), requests)
	}
}

func TestBaiduOpenIdenticalFileDoesNotUploadAgain(t *testing.T) {
	content := []byte("already-on-baidu")
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fullMD5 := md5.Sum(content)
	provider, err := newBaiduOpenProvider("client", "secret", "access", "refresh", "2099-01-01T00:00:00Z", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var requestCount int
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if req.URL.Query().Get("method") != "list" || req.URL.Query().Get("dir") != "/" {
			return nil, fmt.Errorf("unexpected request %s", req.URL.String())
		}
		return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":9,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d,"md5":"%s"}]}`, len(content), hex.EncodeToString(fullMD5[:]))), nil
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "9" || remote.Outcome != "unchanged" || requestCount != 1 {
		t.Fatalf("remote = %#v, requests = %d", remote, requestCount)
	}
}

func TestBaiduOpenRefreshesExpiredTokenOnce(t *testing.T) {
	provider, err := newBaiduOpenProvider("client", "secret", "old-access", "refresh", "2000-01-01T00:00:00Z", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var tokenRequests int
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/oauth/2.0/token":
			tokenRequests++
			return baiduOpenJSONResponse(http.StatusOK, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":2592000}`), nil
		case "/rest/2.0/xpan/file":
			if req.URL.Query().Get("access_token") != "new-access" {
				return nil, fmt.Errorf("access_token = %q", req.URL.Query().Get("access_token"))
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
		default:
			return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
		}
	})

	if err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d", tokenRequests)
	}
}

func TestBaiduOpenWaitForRemoteFileHonorsCancellation(t *testing.T) {
	provider, err := newBaiduOpenProvider("client", "secret", "access", "refresh", "2099-01-01T00:00:00Z", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider.verifyRetryDelay = func(int) time.Duration { return time.Hour }
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.waitForRemoteFile(ctx, "/", "missing.mkv", 1, "missing")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
