package upload

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
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
		client:           client,
		requestGuard:     newCookie115RequestGuard(),
		requestInterval:  0,
		directoryIDs:     map[string]string{"/": "0"},
		uploadRetryDelay: func(int) time.Duration { return 0 },
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
