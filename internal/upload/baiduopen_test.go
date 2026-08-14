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

	"NyaMediaMetadataTool/internal/store"
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

func baiduOpenTestFileMetasResponse(req *http.Request, fsID, path string, size int64, md5Text string) (*http.Response, error) {
	if req.URL.Query().Get("method") != "filemetas" {
		return nil, fmt.Errorf("file metadata method = %q", req.URL.Query().Get("method"))
	}
	if req.URL.Query().Get("fsids") != "["+fsID+"]" {
		return nil, fmt.Errorf("file metadata fsids = %q", req.URL.Query().Get("fsids"))
	}
	return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"info":[{"fs_id":%s,"path":%q,"server_filename":%q,"isdir":0,"size":%d,"md5":%q}]}`, fsID, path, filepath.Base(path), size, md5Text)), nil
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
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
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
				if values.Get("rtype") != "3" || values.Get("ondup") != "" {
					return nil, fmt.Errorf("create collision fields = rtype=%q ondup=%q", values.Get("rtype"), values.Get("ondup"))
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/Anime/episode.mkv"}`), nil
			case "precreate":
				form, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(form))
				var blockList []string
				if err := json.Unmarshal([]byte(values.Get("block_list")), &blockList); err != nil || len(blockList) != 1 || blockList[0] != md5Text {
					return nil, fmt.Errorf("precreate block_list = %q", values.Get("block_list"))
				}
				if values.Get("rtype") != "3" || values.Get("ondup") != "" {
					return nil, fmt.Errorf("precreate collision fields = rtype=%q ondup=%q", values.Get("rtype"), values.Get("ondup"))
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[0]}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		}
		if req.URL.Path == "/rest/2.0/xpan/multimedia" {
			return baiduOpenTestFileMetasResponse(req, "20", "/Anime/episode.mkv", int64(len(content)), md5Text)
		}
		if req.URL.Path == "/rest/2.0/pcs/file" {
			if req.URL.Query().Get("method") != "locateupload" || req.URL.Query().Get("appid") != "250528" || req.URL.Query().Get("uploadid") != "upload-1" {
				return nil, fmt.Errorf("locate upload query = %s", req.URL.RawQuery)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
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
	if len(requests) != 8 {
		t.Fatalf("requests = %d (%v)", len(requests), requests)
	}
}

func TestBaiduOpenUploadUsesOnlyReturnedBlockList(t *testing.T) {
	content := make([]byte, baiduOpenChunkSize+1)
	for index := range content {
		content[index] = byte(index % 251)
	}
	localPath := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	firstPartMD5 := md5.Sum(content[:baiduOpenChunkSize])
	firstPartMD5Text := hex.EncodeToString(firstPartMD5[:])
	partMD5 := md5.Sum(content[baiduOpenChunkSize:])
	partMD5Text := hex.EncodeToString(partMD5[:])
	provider, err := newBaiduOpenProvider("client", "secret", "access", "refresh", "2099-01-01T00:00:00Z", "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var uploadedParts []string
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":20,"path":"/large.bin","server_filename":"large.bin","isdir":0,"size":`+strconv.Itoa(len(content))+`}]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":7,"block_list":[1]}`), nil
			case "create":
				form, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(form))
				var blockList []string
				if err := json.Unmarshal([]byte(values.Get("block_list")), &blockList); err != nil || len(blockList) != 2 || blockList[0] != firstPartMD5Text || blockList[1] != partMD5Text {
					return nil, fmt.Errorf("create form = %s", form)
				}
				if values.Get("rtype") != "3" {
					return nil, fmt.Errorf("create rtype = %q", values.Get("rtype"))
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/large.bin"}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			return baiduOpenTestFileMetasResponse(req, "20", "/large.bin", int64(len(content)), "")
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			uploadedParts = append(uploadedParts, req.URL.Query().Get("partseq"))
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+partMD5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/large.bin", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "20" || len(uploadedParts) != 1 || uploadedParts[0] != "1" {
		t.Fatalf("remote=%#v uploadedParts=%v, want only block 1", remote, uploadedParts)
	}
}

func TestBaiduOpenUploadEmptyReturnedBlockListMeansBlockZero(t *testing.T) {
	content := []byte("empty-block-list")
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
	var uploadedPart string
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":20,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":`+strconv.Itoa(len(content))+`}]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[]}`), nil
			case "create":
				form, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(form))
				if values.Get("rtype") != "3" {
					return nil, fmt.Errorf("create rtype = %q", values.Get("rtype"))
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/episode.mkv"}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			return baiduOpenTestFileMetasResponse(req, "20", "/episode.mkv", int64(len(content)), md5Text)
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			uploadedPart = req.URL.Query().Get("partseq")
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	if _, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace"); err != nil {
		t.Fatal(err)
	}
	if uploadedPart != "0" {
		t.Fatalf("uploaded part = %q, want 0", uploadedPart)
	}
}

func TestBaiduOpenCollisionPolicyDoesNotRenameOnBaidu(t *testing.T) {
	content := []byte("collision-policy")
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fullMD5 := md5.Sum(content)
	md5Text := hex.EncodeToString(fullMD5[:])
	for _, policy := range []string{"skip", "fail"} {
		t.Run(policy, func(t *testing.T) {
			provider, err := newBaiduOpenProvider("client", "secret", "access", "refresh", "2099-01-01T00:00:00Z", "", 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			var precreateRType, createRType string
			listCalls := 0
			provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/rest/2.0/pcs/file":
					return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
				case "/rest/2.0/pcs/superfile2":
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
				case "/rest/2.0/xpan/file":
					switch req.URL.Query().Get("method") {
					case "list":
						listCalls++
						if listCalls == 1 {
							return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
						}
						return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":20,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":`+strconv.Itoa(len(content))+`}]}`), nil
					case "precreate":
						form, _ := io.ReadAll(req.Body)
						values, _ := url.ParseQuery(string(form))
						precreateRType = values.Get("rtype")
						return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[0]}`), nil
					case "create":
						form, _ := io.ReadAll(req.Body)
						values, _ := url.ParseQuery(string(form))
						createRType = values.Get("rtype")
						return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/episode.mkv"}`), nil
					default:
						return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
					}
				case "/rest/2.0/xpan/multimedia":
					return baiduOpenTestFileMetasResponse(req, "20", "/episode.mkv", int64(len(content)), "")
				default:
					return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
				}
			})

			_, err = provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", policy)
			if err != nil {
				t.Fatal(err)
			}
			if precreateRType != "0" || createRType != "0" {
				t.Fatalf("policy=%s precreate rtype=%q create rtype=%q, want 0/0", policy, precreateRType, createRType)
			}
		})
	}
}

func TestBaiduOpenUploadIgnoresPrecreateReturnType(t *testing.T) {
	content := []byte("invalid-precreate")
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
	listCalls := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				listCalls++
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":99,"block_list":[0]}`), nil
			case "create":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/episode.mkv"}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			return baiduOpenTestFileMetasResponse(req, "20", "/episode.mkv", int64(len(content)), md5Text)
		case "/rest/2.0/pcs/file":
			if req.URL.Query().Get("method") != "locateupload" {
				return nil, fmt.Errorf("unexpected locate method %q", req.URL.Query().Get("method"))
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil || remote.ID != "20" || listCalls != 1 {
		t.Fatalf("remote=%#v error=%v listCalls=%d, want successful upload despite return_type", remote, err, listCalls)
	}
}

func TestBaiduOpenUploadRejectsCreateWithoutFSID(t *testing.T) {
	content := []byte("missing-create-fsid")
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
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/rest/2.0/pcs/file" {
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		}
		if req.URL.Path == "/rest/2.0/pcs/superfile2" {
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		}
		if req.URL.Path != "/rest/2.0/xpan/file" {
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
		switch req.URL.Query().Get("method") {
		case "list":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
		case "create":
			form, _ := io.ReadAll(req.Body)
			values, _ := url.ParseQuery(string(form))
			if values.Get("isdir") == "1" {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":10,"path":"/Anime"}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"path":"/Anime/episode.mkv"}`), nil
		case "precreate":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[0]}`), nil
		default:
			return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
		}
	})

	_, err = provider.Upload(context.Background(), localPath, "/Anime/episode.mkv", int64(len(content)), "", "replace")
	if err == nil || !strings.Contains(err.Error(), "create returned no fs_id") {
		t.Fatalf("error = %v, want missing fs_id failure", err)
	}
}

func TestBaiduOpenUploadWaitsForFileMetas(t *testing.T) {
	content := []byte("delayed-file-metadata")
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
	provider.verifyRetryDelay = func(int) time.Duration { return 0 }
	fileMetaCalls := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[0]}`), nil
			case "create":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/episode.mkv"}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			fileMetaCalls++
			if fileMetaCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"info":[]}`), nil
			}
			return baiduOpenTestFileMetasResponse(req, "20", "/episode.mkv", int64(len(content)), md5Text)
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "20" || fileMetaCalls != 2 {
		t.Fatalf("remote=%#v fileMetaCalls=%d, want fs_id 20 after two metadata calls", remote, fileMetaCalls)
	}
}

func TestBaiduOpenUploadRejectsFileMetasPathMismatch(t *testing.T) {
	content := []byte("metadata-path-mismatch")
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
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":1,"block_list":[0]}`), nil
			case "create":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			return baiduOpenTestFileMetasResponse(req, "20", "/episode (1).mkv", int64(len(content)), md5Text)
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	_, err = provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err == nil || !strings.Contains(err.Error(), "metadata path") {
		t.Fatalf("error = %v, want metadata path mismatch", err)
	}
}

func TestBaiduOpenRapidUploadVerifiesRemoteFile(t *testing.T) {
	content := []byte("rapid-upload")
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
	listCalls := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/rest/2.0/xpan/file":
			switch req.URL.Query().Get("method") {
			case "list":
				listCalls++
				if listCalls == 1 {
					return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":20,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":`+strconv.Itoa(len(content))+`,"md5":"`+md5Text+`"}]}`), nil
			case "precreate":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"uploadid":"upload-1","return_type":2,"block_list":[]}`), nil
			case "create":
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"fs_id":20,"path":"/episode.mkv"}`), nil
			default:
				return nil, fmt.Errorf("unexpected xpan method %q", req.URL.Query().Get("method"))
			}
		case "/rest/2.0/xpan/multimedia":
			return baiduOpenTestFileMetasResponse(req, "20", "/episode.mkv", int64(len(content)), md5Text)
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"error_code":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/rest/2.0/pcs/superfile2":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"`+md5Text+`"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "20" || remote.Size != int64(len(content)) || remote.Outcome != store.UploadOutcomeCreated {
		t.Fatalf("remote = %#v", remote)
	}
	if listCalls != 1 {
		t.Fatalf("list calls = %d, want only initial collision check", listCalls)
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

func TestBaiduOpenWaitForFileMetasHonorsCancellation(t *testing.T) {
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
	_, err = provider.waitForRemoteFileByID(ctx, "20", "/missing.mkv", 1, "missing")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
