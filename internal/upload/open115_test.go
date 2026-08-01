package upload

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk115 "github.com/xhofe/115-sdk-go"

	"NyaMediaMetadataTool/internal/store"
)

type fakeOpen115API struct {
	files       map[string][]sdk115.GetFilesResp_File
	uploadInits []*sdk115.UploadInitReq
	mkdirs      []string
	deletes     []*sdk115.DelFileReq
}

func newFakeOpen115API() *fakeOpen115API {
	return &fakeOpen115API{files: map[string][]sdk115.GetFilesResp_File{"0": {}}}
}

func (f *fakeOpen115API) UserInfo(context.Context) (*sdk115.UserInfoResp, error) {
	return &sdk115.UserInfoResp{}, nil
}

func (f *fakeOpen115API) GetFiles(_ context.Context, request *sdk115.GetFilesReq) (*sdk115.GetFilesResp, error) {
	items := append([]sdk115.GetFilesResp_File{}, f.files[request.CID]...)
	response := &sdk115.GetFilesResp{Count: int64(len(items)), Offset: request.Offset}
	start := request.Offset
	if start > int64(len(items)) {
		start = int64(len(items))
	}
	end := start + request.Limit
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	response.Data = items[start:end]
	return response, nil
}

func (f *fakeOpen115API) GetInfoByPath(context.Context, string) (*open115PathInfo, error) {
	return nil, &sdk115.Error{Code: open115PathNotFoundCode, Message: "not found"}
}

func (f *fakeOpen115API) Mkdir(_ context.Context, parentID, name string) (*sdk115.MkdirResp, error) {
	id := "dir-" + name
	f.mkdirs = append(f.mkdirs, parentID+"/"+name)
	f.files[parentID] = append(f.files[parentID], sdk115.GetFilesResp_File{Fid: id, Pid: parentID, Fn: name, Fc: "0"})
	f.files[id] = nil
	return &sdk115.MkdirResp{FileID: id, FileName: name}, nil
}

func (f *fakeOpen115API) DelFile(_ context.Context, request *sdk115.DelFileReq) ([]string, error) {
	f.deletes = append(f.deletes, request)
	items := f.files[request.ParentID]
	for index, item := range items {
		if item.Fid == request.FileIDs {
			f.files[request.ParentID] = append(items[:index], items[index+1:]...)
			break
		}
	}
	return []string{request.FileIDs}, nil
}

func (f *fakeOpen115API) UploadInit(_ context.Context, request *sdk115.UploadInitReq) (*sdk115.UploadInitResp, error) {
	copyOfRequest := *request
	f.uploadInits = append(f.uploadInits, &copyOfRequest)
	f.files[request.Target] = append(f.files[request.Target], sdk115.GetFilesResp_File{
		Fid:  "uploaded-file",
		Pid:  request.Target,
		Fn:   request.FileName,
		Fc:   "1",
		FS:   request.FileSize,
		Sha1: request.FileID,
	})
	return &sdk115.UploadInitResp{Status: 2, FileID: "uploaded-file"}, nil
}

func (f *fakeOpen115API) UploadGetToken(context.Context) (*sdk115.UploadGetTokenResp, error) {
	return &sdk115.UploadGetTokenResp{}, nil
}

func newFakeOpen115Provider(client open115API) *open115Provider {
	return &open115Provider{
		client:               client,
		requestGuard:         newCookie115RequestGuard(),
		requestInterval:      0,
		directoryIDs:         map[string]string{"/": "0"},
		pathInfoRetryDelay:   0,
		uploadRetryDelay:     func(int) time.Duration { return 0 },
		visibilityRetryDelay: func(int) time.Duration { return 0 },
	}
}

func TestCalculateOpen115Digest(t *testing.T) {
	content := strings.Repeat("nya-media-", 20000)
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest, err := calculateOpen115Digest(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	full := sha1.Sum([]byte(content))
	pre := sha1.Sum([]byte(content)[:128*1024])
	if digest.Size != int64(len(content)) || digest.SHA1 != strings.ToUpper(hex.EncodeToString(full[:])) || digest.PreID != strings.ToUpper(hex.EncodeToString(pre[:])) {
		t.Fatalf("unexpected digest: %#v", digest)
	}
}

func TestOpen115ProviderRapidUploadCreatesDirectoryAndVerifies(t *testing.T) {
	client := newFakeOpen115API()
	provider := newFakeOpen115Provider(client)
	localPath := filepath.Join(t.TempDir(), "movie.mkv")
	content := []byte("open-115-rapid-upload")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	remote, err := provider.Upload(context.Background(), localPath, "/Anime/movie.mkv", int64(len(content)), "", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.mkdirs) != 1 || client.mkdirs[0] != "0/Anime" {
		t.Fatalf("mkdir calls = %#v", client.mkdirs)
	}
	if len(client.uploadInits) != 1 || client.uploadInits[0].Target != "dir-Anime" || client.uploadInits[0].PreID == "" {
		t.Fatalf("upload init calls = %#v", client.uploadInits)
	}
	if remote.ID != "uploaded-file" || remote.Outcome != store.UploadOutcomeCreated || !strings.EqualFold(remote.SHA1, client.uploadInits[0].FileID) {
		t.Fatalf("remote = %#v", remote)
	}
}

func TestOpen115ProviderDoesNotUploadIdenticalFile(t *testing.T) {
	content := []byte("already-present")
	sha := sha1.Sum(content)
	shaText := strings.ToUpper(hex.EncodeToString(sha[:]))
	client := newFakeOpen115API()
	client.files["0"] = []sdk115.GetFilesResp_File{{Fid: "existing", Fn: "movie.mkv", Fc: "1", FS: int64(len(content)), Sha1: shaText}}
	provider := newFakeOpen115Provider(client)
	localPath := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	remote, err := provider.Upload(context.Background(), localPath, "/movie.mkv", int64(len(content)), shaText, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.uploadInits) != 0 || remote.ID != "existing" || remote.Outcome != store.UploadOutcomeUnchanged {
		t.Fatalf("remote=%#v upload calls=%d", remote, len(client.uploadInits))
	}
}

func TestOpen115SessionRefreshesExpiredTokenOnce(t *testing.T) {
	client := newFakeOpen115API()
	var refreshCalls int
	var callbackCalls int
	var savedAccessToken, savedRefreshToken, savedExpiresAt string
	session := &open115Session{
		client:       client,
		accessToken:  "expired-access",
		refreshToken: "refresh-old",
		expiresAt:    time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		onTokenRefreshed: func(accessToken, refreshToken, expiresAt string) {
			callbackCalls++
			savedAccessToken = accessToken
			savedRefreshToken = refreshToken
			savedExpiresAt = expiresAt
		},
	}
	session.refresh = func(context.Context) (*sdk115.RefreshTokenResp, error) {
		refreshCalls++
		return &sdk115.RefreshTokenResp{AccessToken: "access-new", RefreshToken: "refresh-new", ExpiresIn: 3600}, nil
	}

	const callers = 5
	errorsCh := make(chan error, callers)
	for index := 0; index < callers; index++ {
		go func() {
			_, err := session.UserInfo(context.Background())
			errorsCh <- err
		}()
	}
	for index := 0; index < callers; index++ {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls=%d, want 1", refreshCalls)
	}
	if callbackCalls != 1 || savedAccessToken != "access-new" || savedRefreshToken != "refresh-new" {
		t.Fatalf("unexpected refresh callback: calls=%d access=%q refresh=%q", callbackCalls, savedAccessToken, savedRefreshToken)
	}
	expiry, err := time.Parse(time.RFC3339, savedExpiresAt)
	if err != nil || expiry.Before(time.Now().Add(50*time.Minute)) {
		t.Fatalf("unexpected refreshed expiry: value=%q err=%v", savedExpiresAt, err)
	}
}

func TestOpen115SessionDoesNotRefreshUsableToken(t *testing.T) {
	session := &open115Session{
		client:       newFakeOpen115API(),
		accessToken:  "access-current",
		refreshToken: "refresh-current",
		expiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	session.refresh = func(context.Context) (*sdk115.RefreshTokenResp, error) {
		t.Fatal("usable access token must not be refreshed")
		return nil, nil
	}
	if _, err := session.UserInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type directPathOpen115API struct {
	*fakeOpen115API
	infos         map[string]*open115PathInfo
	pathInfoCalls int
	getFilesCalls int
}

func (f *directPathOpen115API) GetInfoByPath(_ context.Context, providerPath string) (*open115PathInfo, error) {
	f.pathInfoCalls++
	if info := f.infos[normalize115Path(providerPath)]; info != nil {
		copy := *info
		return &copy, nil
	}
	return nil, &sdk115.Error{Code: open115PathNotFoundCode, Message: "not found"}
}

func (f *directPathOpen115API) GetFiles(ctx context.Context, request *sdk115.GetFilesReq) (*sdk115.GetFilesResp, error) {
	f.getFilesCalls++
	return f.fakeOpen115API.GetFiles(ctx, request)
}

type memoryOpen115Cache struct {
	values map[string]string
}

func newMemoryOpen115Cache() *memoryOpen115Cache {
	return &memoryOpen115Cache{values: make(map[string]string)}
}

func (c *memoryOpen115Cache) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := c.values[key]
	return value, ok, nil
}

func (c *memoryOpen115Cache) Set(_ context.Context, key, value string) error {
	c.values[key] = value
	return nil
}

func (c *memoryOpen115Cache) SetWithTTL(ctx context.Context, key, value string, _ time.Duration) error {
	return c.Set(ctx, key, value)
}

func (c *memoryOpen115Cache) Delete(_ context.Context, key string) error {
	delete(c.values, key)
	return nil
}

func TestOpen115ResolveDirectoryUsesPathInfoBeforeListing(t *testing.T) {
	client := &directPathOpen115API{
		fakeOpen115API: newFakeOpen115API(),
		infos: map[string]*open115PathInfo{
			"/Anime/Season 1": {FileID: "season-1", FileName: "Season 1", FileCategory: "0"},
		},
	}
	provider := newFakeOpen115Provider(client)
	id, err := provider.resolveDirectory(context.Background(), "/Anime/Season 1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "season-1" || client.pathInfoCalls != 1 || client.getFilesCalls != 0 {
		t.Fatalf("id=%q path calls=%d list calls=%d", id, client.pathInfoCalls, client.getFilesCalls)
	}
}

func TestOpen115ResolveDirectoryFallsBackToRootTraversal(t *testing.T) {
	base := newFakeOpen115API()
	base.files["0"] = []sdk115.GetFilesResp_File{{Fid: "anime", Pid: "0", Fn: "Anime", Fc: "0"}}
	base.files["anime"] = []sdk115.GetFilesResp_File{{Fid: "season-1", Pid: "anime", Fn: "Season 1", Fc: "0"}}
	client := &directPathOpen115API{fakeOpen115API: base, infos: map[string]*open115PathInfo{}}
	provider := newFakeOpen115Provider(client)
	id, err := provider.resolveDirectory(context.Background(), "/Anime/Season 1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "season-1" || client.pathInfoCalls != 2 || client.getFilesCalls != 2 {
		t.Fatalf("id=%q path calls=%d list calls=%d", id, client.pathInfoCalls, client.getFilesCalls)
	}
}

func TestOpen115DirectoryAndChildrenCacheSurviveProviderRecreation(t *testing.T) {
	cache := newMemoryOpen115Cache()
	client := &directPathOpen115API{
		fakeOpen115API: newFakeOpen115API(),
		infos: map[string]*open115PathInfo{
			"/Anime": {FileID: "anime", FileName: "Anime", FileCategory: "0"},
		},
	}
	client.files["anime"] = []sdk115.GetFilesResp_File{{Fid: "season-1", Pid: "anime", Fn: "Season 1", Fc: "0"}}
	first := newFakeOpen115Provider(client)
	first.cacheStore = cache
	items, err := first.List(context.Background(), "/Anime")
	if err != nil || len(items) != 1 {
		t.Fatalf("first list items=%+v err=%v", items, err)
	}
	if client.pathInfoCalls != 1 || client.getFilesCalls != 1 {
		t.Fatalf("first list path calls=%d list calls=%d", client.pathInfoCalls, client.getFilesCalls)
	}

	second := newFakeOpen115Provider(client)
	second.cacheStore = cache
	items, err = second.List(context.Background(), "/Anime")
	if err != nil || len(items) != 1 {
		t.Fatalf("second list items=%+v err=%v", items, err)
	}
	if client.pathInfoCalls != 1 || client.getFilesCalls != 1 {
		t.Fatalf("cached list made remote calls: path=%d list=%d cache=%v", client.pathInfoCalls, client.getFilesCalls, cache.values)
	}
}

func TestOpen115SessionRequestsPathInfoByFullPath(t *testing.T) {
	session := newOpen115Session("access", "refresh", "", "", nil)
	client, ok := session.client.(*sdk115.Client)
	if !ok {
		t.Fatalf("session client type=%T", session.client)
	}
	client.SetHttpClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/open/folder/get_info" {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := request.Form.Get("path"); got != "/Anime/Season 1" {
			t.Fatalf("path form=%q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"state":true,"code":0,
				"data":{"file_id":"season-1","file_name":"Season 1","file_category":"0","size":"0"}
			}`)),
			Request: request,
		}, nil
	})})
	info, err := session.GetInfoByPath(context.Background(), "/Anime/Season 1")
	if err != nil {
		t.Fatal(err)
	}
	if info.FileID != "season-1" || info.FileCategory != "0" {
		t.Fatalf("path info=%+v", info)
	}
}

func TestOpen115SessionAcceptsArrayPathInfo(t *testing.T) {
	session := newOpen115Session("access", "refresh", "", "", nil)
	client := session.client.(*sdk115.Client)
	client.SetHttpClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"state":true,"code":0,
				"data":[{"file_id":"poster-1","file_name":"poster.jpg","file_category":"1","size":"123"}]
			}`)),
			Request: request,
		}, nil
	})})

	info, err := session.GetInfoByPath(context.Background(), "/Anime/poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if info.FileID != "poster-1" || info.FileName != "poster.jpg" || info.Size != "123" {
		t.Fatalf("path info=%+v", info)
	}
}

func TestDecodeOpen115PathInfoFallsBackForAmbiguousArray(t *testing.T) {
	_, err := decodeOpen115PathInfo("/Anime/poster.jpg", json.RawMessage(`[
		{"file_id":"poster-1","file_name":"poster.jpg"},
		{"file_id":"poster-2","file_name":"poster.jpg"}
	]`))
	if !isOpen115PathNotFound(err) {
		t.Fatalf("error=%v, want path-not-found fallback", err)
	}
}

func TestOpen115WaitForFileRetriesUntilVisible(t *testing.T) {
	provider := newFakeOpen115Provider(newFakeOpen115API())
	lookupCalls := 0
	provider.lookupChild = func(context.Context, string, string) (sdk115.GetFilesResp_File, bool, error) {
		lookupCalls++
		if lookupCalls < 3 {
			return sdk115.GetFilesResp_File{}, false, nil
		}
		return sdk115.GetFilesResp_File{Fid: "visible-file", Fn: "poster.jpg", Fc: "1", FS: 123}, true, nil
	}

	remote, err := provider.waitForFile(context.Background(), "anime", "poster.jpg", 123)
	if err != nil {
		t.Fatal(err)
	}
	if lookupCalls != 3 || remote.ID != "visible-file" || remote.Size != 123 {
		t.Fatalf("lookup calls=%d remote=%+v", lookupCalls, remote)
	}
}

func TestOpen115WaitForFileFailsAfterVisibilityRetries(t *testing.T) {
	provider := newFakeOpen115Provider(newFakeOpen115API())
	lookupCalls := 0
	provider.lookupChild = func(context.Context, string, string) (sdk115.GetFilesResp_File, bool, error) {
		lookupCalls++
		return sdk115.GetFilesResp_File{}, false, nil
	}

	_, err := provider.waitForFile(context.Background(), "anime", "poster.jpg", 123)
	if !errors.Is(err, err115RemoteFileNotVisible) {
		t.Fatalf("error=%v, want remote-file-not-visible", err)
	}
	if lookupCalls != open115VisibilityAttempts {
		t.Fatalf("lookup calls=%d, want %d", lookupCalls, open115VisibilityAttempts)
	}
}
