package upload

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

func TestBaiduPCSUploadUsesRapidUploadAfterPrecreate(t *testing.T) {
	content := bytes.Repeat([]byte("rapid-upload-content"), (baiduPCSDataContentSize/len("rapid-upload-content"))+1)
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digestFile, err := os.Open(localPath)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := calculateBaiduPCSDigest(context.Background(), digestFile)
	if closeErr := digestFile.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	listCalls := 0
	rapidCalls := 0
	precreateCalls := 0
	var rapidValidationErr error
	var uploadedParts []string
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			if req.URL.Host != "pan.baidu.com" || req.URL.Query().Get("bdstoken") != "token" {
				return nil, fmt.Errorf("web list request host=%q bdstoken=%q", req.URL.Host, req.URL.Query().Get("bdstoken"))
			}
			listCalls++
			if listCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":101,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
		case "/rest/2.0/pcs/file":
			if req.URL.Host != "pcs.baidu.com" || req.URL.Query().Get("method") != "locateupload" {
				return nil, fmt.Errorf("unexpected PCS locateupload request host=%q method=%q", req.URL.Host, req.URL.Query().Get("method"))
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/api/gettemplatevariable":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"result":{"uk":12345}}`), nil
		case "/api/precreate":
			precreateCalls++
			formBytes, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(formBytes))
			var blocks []string
			if err := json.Unmarshal([]byte(form.Get("block_list")), &blocks); err != nil {
				return nil, fmt.Errorf("precreate block list: %w", err)
			}
			if form.Get("path") != "/episode.mkv" || form.Get("target_path") != "/" || form.Get("ondup") != "overwrite" || len(blocks) != 1 || blocks[0] != digest.ChunkMD5s[0] {
				return nil, fmt.Errorf("precreate form = %s", formBytes)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":1,"uploadid":"upload-rapid","block_list":[0]}`), nil
		case "/rest/2.0/pcs/superfile2":
			uploadedParts = append(uploadedParts, req.URL.Query().Get("partseq"))
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"ignored"}`), nil
		case "/api/rapidupload":
			rapidCalls++
			formBytes, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(formBytes))
			if form.Get("uploadid") != "upload-rapid" || form.Get("content-md5") != encodeBaiduOpenMD5(digest.MD5) || form.Get("slice-md5") != encodeBaiduOpenMD5(digest.SliceMD5) || form.Get("data_content") == "" || form.Get("local_mtime") == "" || form.Get("local_ctime") != "" {
				rapidValidationErr = fmt.Errorf("rapid form = %s", formBytes)
				return nil, rapidValidationErr
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"info":{"fs_id":101,"path":"/episode.mkv","size":%d}}`, len(content))), nil
		case "/api/create":
			if rapidValidationErr != nil {
				return nil, rapidValidationErr
			}
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "101" || remote.Size != int64(len(content)) || remote.Outcome != "created" {
		t.Fatalf("remote = %#v", remote)
	}
	if precreateCalls != 1 || len(uploadedParts) != 0 {
		t.Fatalf("precreate calls = %d, uploaded parts = %#v", precreateCalls, uploadedParts)
	}
	if rapidCalls != 1 {
		t.Fatalf("rapid calls = %d, want 1", rapidCalls)
	}
}

func TestBaiduPCSUnexpectedRapidPathIsRenamedWhenTargetRemainsAbsent(t *testing.T) {
	content := bytes.Repeat([]byte("rapid-rename-content"), (baiduPCSDataContentSize/len("rapid-rename-content"))+1)
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	listCalls := 0
	renameCalls := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			listCalls++
			if listCalls < 3 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":707,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
		case "/api/gettemplatevariable":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"result":{"uk":12345}}`), nil
		case "/api/precreate":
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			if form.Get("path") != "/episode.mkv" || form.Get("target_path") != "/" || form.Get("ondup") != "overwrite" {
				return nil, fmt.Errorf("precreate form = %s", formBytes)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":1,"uploadid":"upload-rename","block_list":[0]}`), nil
		case "/api/rapidupload":
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"info":{"fs_id":707,"path":"/episode_20260816_200159.mkv","size":%d}}`, len(content))), nil
		case "/api/filemanager":
			renameCalls++
			if req.URL.Query().Get("bdstoken") != "token" || req.URL.Query().Get("async") != "2" || req.URL.Query().Get("onnest") != "fail" || req.URL.Query().Get("opera") != "rename" {
				return nil, fmt.Errorf("rename query = %s", req.URL.RawQuery)
			}
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			var fileList []struct {
				ID      json.Number `json:"id"`
				Path    string      `json:"path"`
				NewName string      `json:"newname"`
			}
			if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
				return nil, fmt.Errorf("rename filelist: %w", err)
			}
			if len(fileList) != 1 || string(fileList[0].ID) != "707" || fileList[0].Path != "/episode_20260816_200159.mkv" || fileList[0].NewName != "episode.mkv" {
				return nil, fmt.Errorf("rename filelist = %#v", fileList)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"info":[],"taskid":123}`), nil
		case "/share/taskquery":
			if req.URL.Query().Get("taskid") != "123" {
				return nil, fmt.Errorf("rename task query = %s", req.URL.RawQuery)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"task_errno":0,"status":"success"}`), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "707" || remote.Size != int64(len(content)) || remote.Outcome != store.UploadOutcomeCreated {
		t.Fatalf("remote = %#v", remote)
	}
	if listCalls != 3 || renameCalls != 1 {
		t.Fatalf("list calls = %d, rename calls = %d, want list=3 rename=1", listCalls, renameCalls)
	}
}

func TestBaiduPCSUnexpectedRapidPathDeletesExistingTargetBeforeRename(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	listCalls := 0
	operations := make([]string, 0, 2)
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			listCalls++
			if listCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":909,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":10}]}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":707,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":10}]}`), nil
		case "/api/filemanager":
			if req.URL.Query().Get("bdstoken") != "token" || req.URL.Query().Get("async") != "2" || req.URL.Query().Get("onnest") != "fail" {
				return nil, fmt.Errorf("file manager query = %s", req.URL.RawQuery)
			}
			operation := req.URL.Query().Get("opera")
			operations = append(operations, operation)
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			switch operation {
			case "delete":
				if req.URL.Query().Get("newVerify") != "1" {
					return nil, fmt.Errorf("delete query = %s", req.URL.RawQuery)
				}
				var fileList []string
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, fmt.Errorf("delete filelist: %w", err)
				}
				if len(fileList) != 1 || fileList[0] != "/episode.mkv" {
					return nil, fmt.Errorf("delete filelist = %#v", fileList)
				}
			case "rename":
				var fileList []baiduPCSFileManagerItem
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, fmt.Errorf("rename filelist: %w", err)
				}
				if len(fileList) != 1 {
					return nil, fmt.Errorf("rename filelist = %#v", fileList)
				}
				if string(fileList[0].ID) != "707" || fileList[0].Path != "/episode_20260816_200159.mkv" || fileList[0].NewName != "episode.mkv" {
					return nil, fmt.Errorf("rename filelist = %#v", fileList)
				}
			default:
				return nil, fmt.Errorf("unexpected file manager operation %q", operation)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0}`), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.repairUnexpectedRapidUploadPath(context.Background(), &baiduPCSRemotePathError{
		Expected: "/episode.mkv",
		Actual:   "/episode_20260816_200159.mkv",
		FSID:     "707",
	}, "/episode.mkv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "707" || remote.Size != 10 {
		t.Fatalf("remote = %#v", remote)
	}
	if listCalls != 2 || strings.Join(operations, ",") != "delete,rename" {
		t.Fatalf("list calls = %d, operations = %#v, want list=2 operations=[delete rename]", listCalls, operations)
	}
}

func TestBaiduPCSUnexpectedRapidPathCleansUpWhenTargetDeleteFails(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	operations := make([]string, 0, 2)
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":909,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":10}]}`), nil
		case "/api/filemanager":
			operation := req.URL.Query().Get("opera")
			operations = append(operations, operation)
			if operation != "delete" {
				return nil, fmt.Errorf("unexpected operation after target delete failure: %s", operation)
			}
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			if len(operations) == 1 {
				if req.URL.Query().Get("newVerify") != "1" {
					return nil, fmt.Errorf("target delete query = %s", req.URL.RawQuery)
				}
				var fileList []string
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, err
				}
				if len(fileList) != 1 || fileList[0] != "/episode.mkv" {
					return nil, fmt.Errorf("target delete filelist = %#v", fileList)
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":12345,"errmsg":"delete failed"}`), nil
			}
			if req.URL.Query().Get("newVerify") != "1" {
				return nil, fmt.Errorf("rapid cleanup query = %s", req.URL.RawQuery)
			}
			var fileList []string
			if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
				return nil, err
			}
			if len(fileList) != 1 || fileList[0] != "/episode_20260816_200159.mkv" {
				return nil, fmt.Errorf("cleanup filelist = %#v", fileList)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0}`), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	_, err = provider.repairUnexpectedRapidUploadPath(context.Background(), &baiduPCSRemotePathError{
		Expected: "/episode.mkv",
		Actual:   "/episode_20260816_200159.mkv",
		FSID:     "707",
	}, "/episode.mkv", 10)
	if err == nil || !strings.Contains(err.Error(), "delete existing BaiduPCS target") {
		t.Fatalf("error = %v, want target delete failure", err)
	}
	if strings.Join(operations, ",") != "delete,delete" {
		t.Fatalf("operations = %#v, want [delete delete]", operations)
	}
}

func TestBaiduPCSUnexpectedRapidPathCleansUpWhenRenameFails(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	operations := make([]string, 0, 3)
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[{"fs_id":909,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":10}]}`), nil
		case "/api/filemanager":
			operation := req.URL.Query().Get("opera")
			operations = append(operations, operation)
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			switch len(operations) {
			case 1:
				if operation != "delete" || req.URL.Query().Get("newVerify") != "1" {
					return nil, fmt.Errorf("target delete query = %s", req.URL.RawQuery)
				}
				var fileList []string
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, err
				}
				if len(fileList) != 1 || fileList[0] != "/episode.mkv" {
					return nil, fmt.Errorf("target delete filelist = %#v", fileList)
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0}`), nil
			case 2:
				var fileList []baiduPCSFileManagerItem
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, err
				}
				if len(fileList) != 1 {
					return nil, fmt.Errorf("rename filelist = %#v", fileList)
				}
				if operation != "rename" || string(fileList[0].ID) != "707" {
					return nil, fmt.Errorf("rename = %#v", fileList)
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":456,"errmsg":"rename failed"}`), nil
			case 3:
				if operation != "delete" || req.URL.Query().Get("newVerify") != "1" {
					return nil, fmt.Errorf("rapid cleanup query = %s", req.URL.RawQuery)
				}
				var fileList []string
				if err := json.Unmarshal([]byte(form.Get("filelist")), &fileList); err != nil {
					return nil, err
				}
				if len(fileList) != 1 || fileList[0] != "/episode_20260816_200159.mkv" {
					return nil, fmt.Errorf("rapid cleanup filelist = %#v", fileList)
				}
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0}`), nil
			default:
				return nil, fmt.Errorf("unexpected operation %d", len(operations))
			}
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	_, err = provider.repairUnexpectedRapidUploadPath(context.Background(), &baiduPCSRemotePathError{
		Expected: "/episode.mkv",
		Actual:   "/episode_20260816_200159.mkv",
		FSID:     "707",
	}, "/episode.mkv", 10)
	if err == nil || !strings.Contains(err.Error(), "rename rapid upload result") {
		t.Fatalf("error = %v, want rename failure", err)
	}
	if strings.Join(operations, ",") != "delete,rename,delete" {
		t.Fatalf("operations = %#v, want [delete rename delete]", operations)
	}
}

func TestBaiduPCSExistingTargetUsesRapidUploadWhenReplaced(t *testing.T) {
	content := make([]byte, baiduPCSMinBlockSize+10)
	for index := range content {
		content[index] = byte(index % 251)
	}
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	listCalls := 0
	rapidCalls := 0
	createCalls := 0
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			listCalls++
			if listCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":99,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":100,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
		case "/api/gettemplatevariable":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"result":{"uk":12345}}`), nil
		case "/api/precreate":
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			if form.Get("path") != "/episode.mkv" || form.Get("ondup") != "overwrite" {
				return nil, fmt.Errorf("precreate form = %s", formBytes)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":1,"uploadid":"upload-replace","block_list":[0,1]}`), nil
		case "/api/rapidupload":
			rapidCalls++
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"info":{"fs_id":100,"path":"/episode.mkv","size":%d}}`, len(content))), nil
		case "/api/create":
			createCalls++
			return nil, fmt.Errorf("create must not be called after rapid upload")
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "100" || remote.Size != int64(len(content)) || remote.Outcome != store.UploadOutcomeReplaced {
		t.Fatalf("remote = %#v", remote)
	}
	if rapidCalls != 1 || createCalls != 0 {
		t.Fatalf("rapid calls = %d, create calls = %d, want rapid=1 create=0", rapidCalls, createCalls)
	}
}

func TestBaiduPCSInitialDigestOnlyReadsPrecreateBlocks(t *testing.T) {
	content := make([]byte, 2*baiduPCSMinBlockSize+123)
	for index := range content {
		content[index] = byte(index % 251)
	}
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(localPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	digest, err := calculateBaiduPCSInitialDigest(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if digest.Size != int64(len(content)) || digest.BlockSize != baiduPCSMinBlockSize || len(digest.ChunkMD5s) != 2 {
		t.Fatalf("initial digest metadata = %#v", digest)
	}
	wantFirst := md5.Sum(content[:baiduPCSMinBlockSize])
	wantSecond := md5.Sum(content[baiduPCSMinBlockSize : 2*baiduPCSMinBlockSize])
	wantSlice := md5.Sum(content[:baiduPCSSliceSize])
	wantBlocks := []string{hex.EncodeToString(wantFirst[:]), hex.EncodeToString(wantSecond[:])}
	if len(digest.ChunkMD5s) != len(wantBlocks) || digest.ChunkMD5s[0] != wantBlocks[0] || digest.ChunkMD5s[1] != wantBlocks[1] || digest.SliceMD5 != hex.EncodeToString(wantSlice[:]) {
		t.Fatalf("initial digest hashes = %#v, want blocks=%v slice=%s", digest, wantBlocks, hex.EncodeToString(wantSlice[:]))
	}
	if digest.MD5 != "" || digest.SHA1 != "" {
		t.Fatalf("initial digest should not contain complete hashes: %#v", digest)
	}
}

func TestBaiduPCSPreuploadRunsChunksBeforeRapidUpload(t *testing.T) {
	content := make([]byte, 12*1024*1024+123)
	for index := range content {
		content[index] = byte(index % 251)
	}
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := newBaiduPCSProviderWithOptions("BDUSS=test", "token", "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	var partsMu sync.Mutex
	uploadedParts := make([]string, 0)
	listCalls := 0
	rapidCalls := 0
	precreateCalls := 0
	createCalls := 0
	firstBlock := md5.Sum(content[:baiduPCSMinBlockSize])
	secondBlock := md5.Sum(content[baiduPCSMinBlockSize : 2*baiduPCSMinBlockSize])
	wantBlocks := []string{hex.EncodeToString(firstBlock[:]), hex.EncodeToString(secondBlock[:])}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			listCalls++
			if listCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":303,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
		case "/api/gettemplatevariable":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"result":{"uk":12345}}`), nil
		case "/rest/2.0/pcs/file":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/api/precreate":
			precreateCalls++
			formBytes, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(formBytes))
			var blocks []string
			if err := json.Unmarshal([]byte(form.Get("block_list")), &blocks); err != nil {
				return nil, err
			}
			if len(blocks) != len(wantBlocks) || blocks[0] != wantBlocks[0] || blocks[1] != wantBlocks[1] {
				return nil, fmt.Errorf("precreate block list = %#v, want %#v", blocks, wantBlocks)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":1,"uploadid":"upload-preupload","block_list":[0,1]}`), nil
		case "/rest/2.0/pcs/superfile2":
			partsMu.Lock()
			uploadedParts = append(uploadedParts, req.URL.Query().Get("partseq"))
			partsMu.Unlock()
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"md5":"ignored"}`), nil
		case "/api/rapidupload":
			rapidCalls++
			deadline := time.Now().Add(2 * time.Second)
			for {
				partsMu.Lock()
				hasUploadedPart := len(uploadedParts) > 0
				partsMu.Unlock()
				if hasUploadedPart || time.Now().After(deadline) {
					if !hasUploadedPart {
						return nil, errors.New("rapidupload started before any preupload part")
					}
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"info":{"fs_id":303,"path":"/episode.mkv","size":%d}}`, len(content))), nil
		case "/api/create":
			createCalls++
			return nil, fmt.Errorf("unexpected create request")
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	partsMu.Lock()
	preuploadedCount := len(uploadedParts)
	partsMu.Unlock()
	if remote.ID != "303" || remote.Size != int64(len(content)) || preuploadedCount == 0 {
		t.Fatalf("remote=%#v preuploaded_count=%d", remote, preuploadedCount)
	}
	if precreateCalls != 1 || rapidCalls != 1 || createCalls != 0 {
		t.Fatalf("calls precreate=%d rapid=%d create=%d", precreateCalls, rapidCalls, createCalls)
	}
}

func TestBaiduWebMD5EncodingMatchesCapturedValues(t *testing.T) {
	tests := map[string]string{
		"75d430ecaf7e2af89083bdcf83cb3310": "ae5d6f9ffs7ffd0382e8767719287020",
		"4e6ff05e39d763d062288e3c2798de36": "38f426b7cnc43db126bb9b51eb8343d3",
		"aeec9c4e038d046789e2dfaeab8b1031": "02ae41002n4751a1aaa8555600491241",
	}
	for plain, encoded := range tests {
		if got := encodeBaiduOpenMD5(plain); got != encoded {
			t.Fatalf("encodeBaiduOpenMD5(%q) = %q, want %q", plain, got, encoded)
		}
		if got := decodeBaiduOpenMD5(encoded); got != plain {
			t.Fatalf("decodeBaiduOpenMD5(%q) = %q, want %q", encoded, got, plain)
		}
	}
}

func TestBaiduPCSDataOffsetMatchesCapturedValue(t *testing.T) {
	got := baiduPCSDataOffset("38f426b7cnc43db126bb9b51eb8343d3", 416237033, 1786779706, 741385398)
	if got != 426185739 {
		t.Fatalf("baiduPCSDataOffset() = %d, want 426185739", got)
	}
}

func TestBaiduPCSDataOffsetUsesZeroForSmallFiles(t *testing.T) {
	for _, size := range []int64{0, 1, baiduPCSDataContentSize} {
		if got := baiduPCSDataOffset("38f426b7cnc43db126bb9b51eb8343d3", 416237033, 1786779706, size); got != 0 {
			t.Fatalf("baiduPCSDataOffset(size=%d) = %d, want 0", size, got)
		}
	}
}

func TestBaiduPCSMaxConcurrentFilesMapsSVIP(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/loginStatus" || req.URL.Query().Get("version") == "" {
			return nil, fmt.Errorf("unexpected login status request: %s", req.URL.String())
		}
		return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"login_info":{"vip_identity":21,"vip_level":9}}`), nil
	})
	limit, err := provider.MaxConcurrentFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if limit != 3 {
		t.Fatalf("max concurrent files = %d, want 3", limit)
	}
	if second, err := provider.MaxConcurrentFiles(context.Background()); err != nil || second != 3 {
		t.Fatalf("cached max concurrent files = %d err=%v, want 3", second, err)
	}
}

func TestBaiduPCSLogErrorRedactsCredentials(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	message := provider.logError(fmt.Errorf(`Post "https://pan.baidu.com/api/list?bdstoken=token" with Cookie BDUSS=test: connection failed`))
	if strings.Contains(message, "token") || strings.Contains(message, "BDUSS=test") {
		t.Fatalf("redacted message leaked credentials: %s", message)
	}
	if !strings.Contains(message, "<redacted>") {
		t.Fatalf("redacted message = %q, want redaction marker", message)
	}
}

func TestBaiduPCSRequestAcceptsPlainJSONWithGzipHeader(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Encoding": []string{"gzip"}},
			Body:       io.NopCloser(strings.NewReader(`{"errno":0,"list":[]}`)),
			Request:    req,
		}, nil
	})

	var response baiduPCSListResponse
	if err := provider.doJSONRequest(context.Background(), http.MethodGet, baiduPCSAPIBaseURL+"/rest/2.0/pcs/file", url.Values{"method": []string{"list"}}, nil, "", 0, false, &response); err != nil {
		t.Fatal(err)
	}
	if response.Errno != 0 || response.List == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestBaiduPCSCreateDirectoryUsesWebAPI(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/create" || req.URL.Host != "pan.baidu.com" {
			return nil, fmt.Errorf("unexpected directory create request: %s://%s%s", req.URL.Scheme, req.URL.Host, req.URL.Path)
		}
		if req.URL.Query().Get("bdstoken") != "token" || req.URL.Query().Get("rtype") != "1" {
			return nil, fmt.Errorf("directory create query = %s", req.URL.RawQuery)
		}
		formBytes, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return nil, readErr
		}
		form, parseErr := url.ParseQuery(string(formBytes))
		if parseErr != nil {
			return nil, parseErr
		}
		if form.Get("path") != "/Video/NEW" || form.Get("isdir") != "1" || form.Get("ondup") != "fail" {
			return nil, fmt.Errorf("directory create form = %s", formBytes)
		}
		return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"path":"/Video/NEW","isdir":1}`), nil
	})

	if err := provider.createDirectory(context.Background(), "/Video/NEW"); err != nil {
		t.Fatal(err)
	}
}

func TestBaiduPCSEnsureDirectorySerializesConcurrentCreation(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var stateMu sync.Mutex
	created := make(map[string]bool)
	createCalls := make(map[string]int)
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			directory := req.URL.Query().Get("dir")
			stateMu.Lock()
			defer stateMu.Unlock()
			entries := make([]string, 0, 1)
			for _, child := range []string{"/Video", "/Video/NEW", "/Video/NEW/Season 1"} {
				if !created[child] {
					continue
				}
				parent := "/"
				name := child[1:]
				if index := strings.LastIndex(name, "/"); index >= 0 {
					parent = "/" + name[:index]
					name = name[index+1:]
				}
				if parent == directory {
					entries = append(entries, fmt.Sprintf(`{"fs_id":%d,"path":%q,"server_filename":%q,"isdir":1}`, len(entries)+1, child, name))
				}
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[%s]}`, strings.Join(entries, ","))), nil
		case "/api/create":
			formBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				return nil, readErr
			}
			form, parseErr := url.ParseQuery(string(formBytes))
			if parseErr != nil {
				return nil, parseErr
			}
			directory := form.Get("path")
			stateMu.Lock()
			createCalls[directory]++
			created[directory] = true
			stateMu.Unlock()
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"path":%q,"isdir":1}`, directory)), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	const workers = 8
	start := make(chan struct{})
	errorsCh := make(chan error, workers)
	var workersGroup sync.WaitGroup
	workersGroup.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer workersGroup.Done()
			<-start
			errorsCh <- provider.ensureDirectory(context.Background(), "/Video/NEW/Season 1")
		}()
	}
	close(start)
	workersGroup.Wait()
	close(errorsCh)
	for ensureErr := range errorsCh {
		if ensureErr != nil {
			t.Fatal(ensureErr)
		}
	}

	stateMu.Lock()
	defer stateMu.Unlock()
	if len(createCalls) != 3 {
		t.Fatalf("created directories = %#v, want exactly the three path components", createCalls)
	}
	for _, directory := range []string{"/Video", "/Video/NEW", "/Video/NEW/Season 1"} {
		if createCalls[directory] != 1 {
			t.Fatalf("create calls for %s = %d, want 1", directory, createCalls[directory])
		}
	}
}

func TestBaiduPCSCreateDirectoryRejectsRenamedPath(t *testing.T) {
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/create" {
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
		return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"info":{"fs_id":42,"path":"/Video/NEW/Season 1_20260815_183626","isdir":1}}`), nil
	})

	err = provider.createDirectory(context.Background(), "/Video/NEW/Season 1")
	var pathErr *baiduPCSDirectoryPathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error = %v, want renamed-directory path error", err)
	}
	if pathErr.Expected != "/Video/NEW/Season 1" || pathErr.Actual != "/Video/NEW/Season 1_20260815_183626" || pathErr.FSID != "42" {
		t.Fatalf("path error = %#v", pathErr)
	}
}

func TestBaiduPCSPrecreateRequiresUploadID(t *testing.T) {
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, []byte("missing-fsid"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			if req.URL.Host != "pan.baidu.com" {
				return nil, fmt.Errorf("unexpected web list host %q", req.URL.Host)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
		case "/api/precreate":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":2}`), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	_, err = provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len("missing-fsid")), "", "replace")
	if err == nil || !strings.Contains(err.Error(), "no uploadid") {
		t.Fatalf("error = %v, want missing uploadid failure", err)
	}
}

func TestBaiduPCSUploadFallsBackToAllBlocksAndCreate(t *testing.T) {
	content := make([]byte, baiduPCSMinBlockSize+10)
	for index := range content {
		content[index] = byte(index % 251)
	}
	localPath := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var logBuffer bytes.Buffer
	provider, err := newBaiduPCSProvider("BDUSS=test", "token", "", 0, slog.New(slog.NewTextHandler(&logBuffer, nil)))
	if err != nil {
		t.Fatal(err)
	}
	firstBlock := md5.Sum(content[:baiduPCSMinBlockSize])
	secondBlock := md5.Sum(content[baiduPCSMinBlockSize:])
	wantBlocks := []string{hex.EncodeToString(firstBlock[:]), hex.EncodeToString(secondBlock[:])}
	listCalls := 0
	rapidCalls := 0
	var uploadedParts []string
	var uploadedPartsMu sync.Mutex
	provider.httpClient.Transport = baiduOpenRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/list":
			if req.URL.Host != "pan.baidu.com" {
				return nil, fmt.Errorf("unexpected web list host %q", req.URL.Host)
			}
			listCalls++
			if listCalls == 1 {
				return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"list":[]}`), nil
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"list":[{"fs_id":202,"path":"/episode.mkv","server_filename":"episode.mkv","isdir":0,"size":%d}]}`, len(content))), nil
		case "/api/gettemplatevariable":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"result":{"uk":12345}}`), nil
		case "/rest/2.0/pcs/file":
			if req.URL.Host != "pcs.baidu.com" || req.URL.Query().Get("method") != "locateupload" {
				return nil, fmt.Errorf("unexpected PCS locateupload request host=%q method=%q", req.URL.Host, req.URL.Query().Get("method"))
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"servers":[{"server":"https://upload.example"}]}`), nil
		case "/api/user/getinfo":
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"records":[{"uk":12345}]}`), nil
		case "/api/rapidupload":
			rapidCalls++
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":31023,"errmsg":"not found"}`), nil
		case "/api/precreate":
			formBytes, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(formBytes))
			var blocks []string
			if err := json.Unmarshal([]byte(form.Get("block_list")), &blocks); err != nil {
				return nil, fmt.Errorf("precreate block list: %w", err)
			}
			if len(blocks) != 2 || blocks[0] != wantBlocks[0] || blocks[1] != wantBlocks[1] {
				return nil, fmt.Errorf("precreate block list = %#v, want %#v", blocks, wantBlocks)
			}
			return baiduOpenJSONResponse(http.StatusOK, `{"errno":0,"return_type":1,"uploadid":"upload-1","block_list":[1]}`), nil
		case "/rest/2.0/pcs/superfile2":
			uploadedPartsMu.Lock()
			uploadedParts = append(uploadedParts, req.URL.Query().Get("partseq"))
			uploadedPartsMu.Unlock()
			return baiduOpenJSONResponse(http.StatusOK, `{"md5":"ignored"}`), nil
		case "/api/create":
			formBytes, _ := io.ReadAll(req.Body)
			form, _ := url.ParseQuery(string(formBytes))
			var blocks []string
			if err := json.Unmarshal([]byte(form.Get("block_list")), &blocks); err != nil {
				return nil, fmt.Errorf("create block list: %w", err)
			}
			if form.Get("uploadid") != "upload-1" || form.Get("rtype") != "3" || len(blocks) != 2 || blocks[0] != wantBlocks[0] || blocks[1] != wantBlocks[1] {
				return nil, fmt.Errorf("create form = %s", formBytes)
			}
			return baiduOpenJSONResponse(http.StatusOK, fmt.Sprintf(`{"errno":0,"fs_id":202,"path":"/episode.mkv","size":%d}`, len(content))), nil
		default:
			return nil, fmt.Errorf("unexpected BaiduPCS request %s", req.URL.Path)
		}
	})

	remote, err := provider.Upload(context.Background(), localPath, "/episode.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if remote.ID != "202" || remote.Size != int64(len(content)) || remote.Outcome != "created" {
		t.Fatalf("remote = %#v", remote)
	}
	if rapidCalls != 1 {
		t.Fatalf("rapid calls = %d, want upload-session attempt", rapidCalls)
	}
	uploadedPartsMu.Lock()
	sort.Strings(uploadedParts)
	uploadedPartsMu.Unlock()
	if len(uploadedParts) != 2 || uploadedParts[0] != "0" || uploadedParts[1] != "1" {
		t.Fatalf("uploaded parts = %#v, want [0 1]", uploadedParts)
	}
	logs := logBuffer.String()
	for _, want := range []string{
		"operation=precreate",
		"operation=locateupload",
		"operation=superfile2",
		"operation=rapidupload",
		"code=31023",
		"operation=create",
		"baidu pcs rapid upload missed; falling back to chunk upload",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("diagnostic logs do not contain %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "BDUSS=test") || strings.Contains(logs, "bdstoken=token") {
		t.Fatalf("diagnostic logs leaked credentials:\n%s", logs)
	}
}
