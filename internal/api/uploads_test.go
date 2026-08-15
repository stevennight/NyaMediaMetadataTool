package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
)

func TestUploadTransferWithRuntimeProgress(t *testing.T) {
	transfer := store.UploadTransfer{BytesTotal: 100, BytesTransferred: 0}
	got := uploadTransferWithRuntimeProgress(transfer, upload.TransferRuntimeState{BytesTransferred: 45})
	if got.BytesTransferred != 45 {
		t.Fatalf("runtime progress=%d, want 45", got.BytesTransferred)
	}

	got = uploadTransferWithRuntimeProgress(transfer, upload.TransferRuntimeState{BytesTransferred: 150})
	if got.BytesTransferred != 100 {
		t.Fatalf("clamped runtime progress=%d, want 100", got.BytesTransferred)
	}

	completed := store.UploadTransfer{BytesTotal: 100, BytesTransferred: 100}
	got = uploadTransferWithRuntimeProgress(completed, upload.TransferRuntimeState{BytesTransferred: 40})
	if got.BytesTransferred != 100 {
		t.Fatalf("persisted progress regressed to %d", got.BytesTransferred)
	}
}

func TestUploadProviderCookieNeverAppearsInProviderResponse(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))

	create := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(`{"name":"115 Archive","type":"115cookie","enabled":true,"remoteRoot":"/Anime"}`))
	create.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var provider store.UploadProvider
	if err := json.Unmarshal(createResponse.Body.Bytes(), &provider); err != nil {
		t.Fatal(err)
	}

	cookie := "UID=private-cookie; CID=private"
	saveCookie := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/cookie", bytes.NewBufferString(`{"cookie":"`+cookie+`","authDevice":"android"}`))
	saveCookie.Header.Set("Content-Type", "application/json")
	saveCookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(saveCookieResponse, saveCookie)
	if saveCookieResponse.Code != http.StatusOK {
		t.Fatalf("save Cookie status=%d body=%s", saveCookieResponse.Code, saveCookieResponse.Body.String())
	}
	if bytes.Contains(saveCookieResponse.Body.Bytes(), []byte(cookie)) {
		t.Fatal("Cookie leaked through save response")
	}

	list := httptest.NewRequest(http.MethodGet, "/api/upload/providers", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	if bytes.Contains(listResponse.Body.Bytes(), []byte(cookie)) {
		t.Fatal("Cookie leaked through list response")
	}
	var providers []store.UploadProvider
	if err := json.Unmarshal(listResponse.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || !providers[0].HasCookie || providers[0].AuthDevice != "android" {
		t.Fatalf("unexpected providers: %#v", providers)
	}
}

func TestUploadProviderCookieRequiresSupportedAuthDevice(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 Device", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	for _, body := range []string{`{"cookie":"UID=test"}`, `{"cookie":"UID=test","authDevice":"windows"}`} {
		request := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/cookie", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestUploadProviderUpdateCannotOverwriteStoredAuthDevice(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 Protected", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=test", "ios"); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	body := `{"name":"115 Protected Updated","type":"115cookie","enabled":true,"authDevice":"web"}`
	request := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID), bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var updated store.UploadProvider
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.AuthDevice != "ios" {
		t.Fatalf("provider update overwrote auth device: %#v", updated)
	}
}

func TestUploadProviderTypeChangeReturnsConflictWithoutChangingCredentials(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 Fixed", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=original", "android"); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))

	request := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID), bytes.NewBufferString(`{"name":"Must Not Persist","type":"115open","enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("type change status=%d body=%s", response.Code, response.Body.String())
	}

	persisted, err := st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "115 Fixed" || persisted.Type != store.UploadProviderType115Cookie || persisted.AuthDevice != "android" || !persisted.HasCookie || cookie != "UID=original" {
		t.Fatalf("rejected type change altered provider credentials: provider=%#v cookie=%q", persisted, cookie)
	}
}

func TestUploadProviderGenericCookieSecretCannotBypassDedicatedEndpoint(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "115 Protected", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUploadProviderCookie(ctx, provider.ID, "UID=original", "ios"); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	path := "/api/upload/providers/" + jsonNumber(provider.ID) + "/secrets/cookie"

	put := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(`{"value":"UID=replaced"}`))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusBadRequest {
		t.Fatalf("generic Cookie PUT status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, path, nil)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("generic Cookie DELETE status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}

	cookie, err := st.GetUploadProviderSecret(ctx, provider.ID, "cookie")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := st.GetUploadProvider(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "UID=original" || !persisted.HasCookie || persisted.AuthDevice != "ios" {
		t.Fatalf("generic endpoint changed Cookie authorization: cookie=%q provider=%#v", cookie, persisted)
	}
}

func TestUploadProviderDirectoryReturnsProviderEntries(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{
		"/Anime": {
			{ID: "directory-1", Name: "Series", Path: "/Anime/Series", IsDir: true},
			{ID: "file-1", Name: "poster.jpg", Path: "/Anime/poster.jpg", Size: 2048},
		},
	}}
	handler, _, provider := newUploadDirectoryTestHandler(t, remote)

	request := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/directories?path=Anime//", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("directory list status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Path    string               `json:"path"`
		Entries []upload.RemoteEntry `json:"entries"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Path != "/Anime" {
		t.Fatalf("directory list path=%q, want /Anime", result.Path)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("directory list entries=%#v", result.Entries)
	}
	if entry := result.Entries[0]; entry.ID != "directory-1" || entry.Name != "Series" || entry.Path != "/Anime/Series" || !entry.IsDir || entry.Size != 0 {
		t.Fatalf("unexpected directory entry: %#v", entry)
	}
	if entry := result.Entries[1]; entry.ID != "file-1" || entry.Name != "poster.jpg" || entry.Path != "/Anime/poster.jpg" || entry.IsDir || entry.Size != 2048 {
		t.Fatalf("unexpected file entry: %#v", entry)
	}
	if len(remote.listedPaths) != 1 || remote.listedPaths[0] != "/Anime" {
		t.Fatalf("provider list paths=%#v, want [/Anime]", remote.listedPaths)
	}
}

func TestUploadProviderDirectoryMissingPathReturnsBadGateway(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/": nil}}
	handler, _, provider := newUploadDirectoryTestHandler(t, remote)

	request := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/directories?path=/Missing", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("missing directory status=%d body=%s", response.Code, response.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["error"] != `remote directory "/Missing" not found` {
		t.Fatalf("missing directory error=%q", result["error"])
	}
}

func TestUploadProviderDirectoryRejectsParentTraversal(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/": nil}}
	handler, _, provider := newUploadDirectoryTestHandler(t, remote)

	request := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/directories?path=/Anime/../Outside", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("parent traversal status=%d body=%s", response.Code, response.Body.String())
	}
	if len(remote.listedPaths) != 0 {
		t.Fatalf("provider was called for rejected path: %#v", remote.listedPaths)
	}
}

func TestWatchDirUploadConfigsRoundTripThroughAPI(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/TV": nil}}
	watchDir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: filepath.Join(t.TempDir(), "media"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), newDirectoryListingManager(st, remote))
	request := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(`{"name":"115 Archive","type":"115cookie","enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var provider store.UploadProvider
	if err := json.Unmarshal(response.Body.Bytes(), &provider); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("remoteRoot")) || bytes.Contains(response.Body.Bytes(), []byte("routes")) {
		t.Fatalf("provider response still exposes directory mapping fields: %s", response.Body.String())
	}

	watchBody := `{"path":` + strconv.Quote(watchDir.Path) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/TV","collisionPolicy":"fail","includeTypes":["video","nfo"]}]}`
	update := httptest.NewRequest(http.MethodPut, "/api/watch-dirs/"+jsonNumber(watchDir.ID), bytes.NewBufferString(watchBody))
	update.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated store.WatchDir
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.UploadConfigs) != 1 || updated.UploadConfigs[0].ProviderID != provider.ID || updated.UploadConfigs[0].RemoteRoot != "/TV" {
		t.Fatalf("unexpected directory upload configuration: %#v", updated.UploadConfigs)
	}

	providerUpdate := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID), bytes.NewBufferString(`{"name":"115 Archive Updated","type":"115cookie","enabled":true}`))
	providerUpdate.Header.Set("Content-Type", "application/json")
	providerUpdateResponse := httptest.NewRecorder()
	handler.ServeHTTP(providerUpdateResponse, providerUpdate)
	if providerUpdateResponse.Code != http.StatusOK {
		t.Fatalf("provider update status=%d body=%s", providerUpdateResponse.Code, providerUpdateResponse.Body.String())
	}
	persisted, err := st.GetWatchDir(ctx, watchDir.ID)
	if err != nil || len(persisted.UploadConfigs) != 1 || persisted.UploadConfigs[0].RemoteRoot != "/TV" {
		t.Fatalf("provider update changed directory upload configuration: %#v err=%v", persisted.UploadConfigs, err)
	}
}

func TestWatchDirCreateRejectsMissingRemoteRootWithoutPersisting(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
	handler, st, provider := newUploadDirectoryTestHandler(t, remote)

	body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Missing","collisionPolicy":"fail","includeTypes":["video"]}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("create with missing remote root status=%d body=%s", response.Code, response.Body.String())
	}
	dirs, err := st.ListWatchDirs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 0 {
		t.Fatalf("rejected create persisted watch directories: %#v", dirs)
	}
}

func TestWatchDirCreateAllowsDisabledMissingRemoteRoot(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
	handler, st, provider := newUploadDirectoryTestHandler(t, remote)

	body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":false,"remoteRoot":"/Missing","collisionPolicy":"fail","includeTypes":["video"]}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create with disabled missing remote root status=%d body=%s", response.Code, response.Body.String())
	}
	if len(remote.listedPaths) != 0 {
		t.Fatalf("disabled route unexpectedly accessed provider: %#v", remote.listedPaths)
	}
	dirs, err := st.ListWatchDirs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || len(dirs[0].UploadConfigs) != 1 || dirs[0].UploadConfigs[0].Enabled || dirs[0].UploadConfigs[0].RemoteRoot != "/Missing" {
		t.Fatalf("disabled route was not persisted as configured: %#v", dirs)
	}
}

func TestWatchDirCreateAllowsProviderWithMultipleRemoteRoots(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
	handler, st, provider := newUploadDirectoryTestHandler(t, remote)

	firstRoute := `{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Existing","collisionPolicy":"fail","includeTypes":["video"]}`
	secondRoute := `{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Existing/Backup","collisionPolicy":"fail","includeTypes":["video"]}`
	remote.directories["/Existing/Backup"] = nil
	body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[` + firstRoute + `,` + secondRoute + `]}`
	request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create with repeated provider status=%d body=%s", response.Code, response.Body.String())
	}
	if len(remote.listedPaths) != 2 {
		t.Fatalf("expected both remote roots to be checked: %#v", remote.listedPaths)
	}
	dirs, err := st.ListWatchDirs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || len(dirs[0].UploadConfigs) != 2 {
		t.Fatalf("provider routes were not persisted independently: %#v", dirs)
	}
}

func TestWatchDirCreateRejectsInvalidIncludeTypesBeforeRemoteAccess(t *testing.T) {
	for _, test := range []struct {
		name         string
		includeTypes string
	}{
		{name: "empty", includeTypes: `[]`},
		{name: "blank", includeTypes: `[""]`},
		{name: "unknown", includeTypes: `["not-a-file-type"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
			handler, st, provider := newUploadDirectoryTestHandler(t, remote)

			body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Existing","collisionPolicy":"fail","includeTypes":` + test.includeTypes + `}]}`
			request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("create with %s include types status=%d body=%s", test.name, response.Code, response.Body.String())
			}
			if len(remote.listedPaths) != 0 {
				t.Fatalf("structurally invalid include types accessed provider: %#v", remote.listedPaths)
			}
			dirs, err := st.ListWatchDirs(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(dirs) != 0 {
				t.Fatalf("invalid include types persisted watch directories: %#v", dirs)
			}
		})
	}
}

func TestWatchDirUpdateUnknownReturnsNotFoundBeforeRemoteAccess(t *testing.T) {
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
	handler, _, provider := newUploadDirectoryTestHandler(t, remote)

	body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Missing","collisionPolicy":"fail","includeTypes":["video"]}]}`
	request := httptest.NewRequest(http.MethodPut, "/api/watch-dirs/999999", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown watch directory update status=%d body=%s", response.Code, response.Body.String())
	}
	if len(remote.listedPaths) != 0 {
		t.Fatalf("unknown watch directory update accessed provider: %#v", remote.listedPaths)
	}
}

func TestWatchDirUpdateRejectsMissingRemoteRootWithoutOverwriting(t *testing.T) {
	ctx := context.Background()
	remote := &fakeDirectoryListingProvider{directories: map[string][]upload.RemoteEntry{"/Existing": nil}}
	handler, st, provider := newUploadDirectoryTestHandler(t, remote)
	original, err := st.CreateWatchDir(ctx, store.WatchDir{
		Path:                filepath.Join(t.TempDir(), "original"),
		Recursive:           true,
		WatchEnabled:        true,
		UseGlobalProcessing: true,
		UploadConfigs: []store.UploadProviderRoute{{
			ProviderID:      provider.ID,
			Enabled:         true,
			RemoteRoot:      "/Existing",
			CollisionPolicy: "fail",
			IncludeTypes:    []string{"video"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	attemptedPath := filepath.Join(t.TempDir(), "changed")
	body := `{"path":` + strconv.Quote(attemptedPath) + `,"recursive":false,"watchEnabled":false,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":` + jsonNumber(provider.ID) + `,"enabled":true,"remoteRoot":"/Missing","collisionPolicy":"skip","includeTypes":["nfo"]}]}`
	request := httptest.NewRequest(http.MethodPut, "/api/watch-dirs/"+jsonNumber(original.ID), bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("update with missing remote root status=%d body=%s", response.Code, response.Body.String())
	}
	persisted, err := st.GetWatchDir(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Path != original.Path || !persisted.Recursive || !persisted.WatchEnabled {
		t.Fatalf("rejected update changed watch directory: before=%#v after=%#v", original, persisted)
	}
	if len(persisted.UploadConfigs) != 1 || persisted.UploadConfigs[0].RemoteRoot != "/Existing" || persisted.UploadConfigs[0].CollisionPolicy != "fail" || len(persisted.UploadConfigs[0].IncludeTypes) != 1 || persisted.UploadConfigs[0].IncludeTypes[0] != "video" {
		t.Fatalf("rejected update changed upload configuration: %#v", persisted.UploadConfigs)
	}
}

func TestWatchDirCreateRollsBackInvalidUploadConfig(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	body := `{"path":` + strconv.Quote(filepath.Join(t.TempDir(), "media")) + `,"recursive":true,"watchEnabled":true,"useGlobalProcessing":true,"uploadConfigs":[{"providerId":999,"enabled":true,"remoteRoot":"/TV"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/watch-dirs", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	dirs, err := st.ListWatchDirs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 0 {
		t.Fatalf("failed upload configuration left a watch directory behind: %#v", dirs)
	}
}

func TestDeleteUploadProviderWithHistoryReturnsConflict(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Archive", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir, err := st.CreateWatchDir(ctx, store.WatchDir{
		Path: root, Recursive: true, WatchEnabled: true, UseGlobalProcessing: true,
		UploadConfigs: []store.UploadProviderRoute{{ProviderID: provider.ID, Enabled: true, RemoteRoot: "/Archive", CollisionPolicy: "fail", IncludeTypes: []string{"video"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(root, "Show")
	if _, created, err := st.CollectUploadBatch(ctx, store.UploadCollectionInput{
		WatchDirID: &dir.ID, SeriesKey: "show", SeriesPath: seriesPath, QuietPeriod: time.Minute,
		Files: []store.UploadCandidate{{LocalPath: filepath.Join(seriesPath, "S01E01.mkv"), RelativePath: "Show/S01E01.mkv", FileType: "video", Size: 100, ModifiedAt: time.Now()}},
	}); err != nil || !created {
		t.Fatalf("create provider history: created=%v err=%v", created, err)
	}

	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	request := httptest.NewRequest(http.MethodDelete, "/api/upload/providers/"+jsonNumber(provider.ID), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := st.GetUploadProvider(ctx, provider.ID); err != nil {
		t.Fatalf("provider with history should remain after conflict: %v", err)
	}
}

func TestUploadTargetNotRetryableMapsToConflict(t *testing.T) {
	response := httptest.NewRecorder()
	writeUploadStoreError(response, store.ErrUploadTargetNotRetryable)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUploadProviderTypesExposeFutureCapabilities(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(st, slog.Default()))
	request := httptest.NewRequest(http.MethodGet, "/api/upload/provider-types", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("provider types status=%d body=%s", response.Code, response.Body.String())
	}
	var descriptors []upload.ProviderDescriptor
	if err := json.Unmarshal(response.Body.Bytes(), &descriptors); err != nil {
		t.Fatal(err)
	}
	implemented := map[string]bool{}
	deviceNames := map[string]string{}
	for _, descriptor := range descriptors {
		implemented[descriptor.Type] = descriptor.Implemented
		if descriptor.Type == store.UploadProviderType115Cookie {
			for _, device := range descriptor.AuthDevices {
				deviceNames[device.Code] = device.Name
			}
		}
	}
	if !implemented[store.UploadProviderType115Cookie] || !implemented[store.UploadProviderType115Open] || implemented[store.UploadProviderType123Pan] || !implemented[store.UploadProviderTypeBaiduPan] || !implemented[store.UploadProviderTypeBaiduPCS] {
		t.Fatalf("unexpected provider descriptors: %#v", descriptors)
	}
	for _, code := range []string{"web", "android", "ios", "tv", "alipaymini", "wechatmini", "qandroid"} {
		if deviceNames[code] == "" {
			t.Fatalf("115 Cookie auth device %q missing: %#v", code, descriptors)
		}
	}
}

func TestUploadProviderRejectsUnknownOrUnavailableEnabledType(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := upload.New(st, slog.Default())
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), manager)
	for _, body := range []string{
		`{"name":"Future","type":"123pan","enabled":true,"remoteRoot":"/Archive"}`,
		`{"name":"Typo","type":"not-a-provider","enabled":true,"remoteRoot":"/Archive"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("provider body=%s status=%d body=%s", body, response.Code, response.Body.String())
		}
	}

	disabled := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(`{"name":"Future disabled","type":"123pan","enabled":false,"remoteRoot":"/Archive"}`))
	disabled.Header.Set("Content-Type", "application/json")
	disabledResponse := httptest.NewRecorder()
	handler.ServeHTTP(disabledResponse, disabled)
	if disabledResponse.Code != http.StatusCreated {
		t.Fatalf("disabled future provider status=%d body=%s", disabledResponse.Code, disabledResponse.Body.String())
	}
}

func TestUploadProviderTypesReflectRuntimeRegistration(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := upload.New(st, slog.Default())
	manager.RegisterProviderDescriptor(upload.ProviderDescriptor{Type: "testdrive", Name: "Test Drive", SecretKeys: []string{"access_token"}}, func(context.Context, store.UploadBatchTarget, upload.SecretLookup) (upload.Provider, error) {
		return nil, nil
	})
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), manager)
	request := httptest.NewRequest(http.MethodGet, "/api/upload/provider-types", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("provider types status=%d body=%s", response.Code, response.Body.String())
	}
	var descriptors []upload.ProviderDescriptor
	if err := json.Unmarshal(response.Body.Bytes(), &descriptors); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range descriptors {
		if descriptor.Type == "testdrive" && descriptor.Implemented && descriptor.Name == "Test Drive" && len(descriptor.SecretKeys) == 1 && descriptor.SecretKeys[0] == "access_token" {
			return
		}
	}
	t.Fatalf("runtime provider did not appear as implemented: %#v", descriptors)
}

func TestUploadProviderGenericSecretsUseDescriptorKeysWithoutResponseLeak(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	manager := upload.New(st, slog.Default())
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), manager)
	create := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(`{"name":"Future","type":"115open","enabled":false,"remoteRoot":"/Archive"}`))
	create.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var provider store.UploadProvider
	if err := json.Unmarshal(createResponse.Body.Bytes(), &provider); err != nil {
		t.Fatal(err)
	}

	secret := "private-access-token"
	put := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/secrets/access_token", bytes.NewBufferString(`{"value":"`+secret+`"}`))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusNoContent || bytes.Contains(putResponse.Body.Bytes(), []byte(secret)) {
		t.Fatalf("secret response leaked or failed: status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	stored, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token")
	if err != nil || stored != secret {
		t.Fatalf("generic secret was not stored: value=%q err=%v", stored, err)
	}

	invalid := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/secrets/not_allowed", bytes.NewBufferString(`{"value":"x"}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid credential key status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestUploadProviderOpen115TokenImport(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	manager := upload.New(st, slog.Default())
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), manager)
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Third Party", Type: store.UploadProviderType115Open, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/115open", bytes.NewBufferString(`{
		"accessToken":"access-third-party",
		"refreshToken":"refresh-third-party"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("access-third-party")) || bytes.Contains(response.Body.Bytes(), []byte("refresh-third-party")) {
		t.Fatalf("token import response leaked credentials: %s", response.Body.String())
	}
	var saved store.UploadProvider
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.HasCredentials {
		t.Fatalf("provider did not report imported credentials: %#v", saved)
	}
	for key, want := range map[string]string{"access_token": "access-third-party", "refresh_token": "refresh-third-party"} {
		got, err := st.GetUploadProviderSecret(ctx, provider.ID, key)
		if err != nil || got != want {
			t.Fatalf("secret %s=%q err=%v want=%q", key, got, err, want)
		}
	}

	partial := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/115open", bytes.NewBufferString(`{"refreshToken":"refresh-rotated"}`))
	partial.Header.Set("Content-Type", "application/json")
	partialResponse := httptest.NewRecorder()
	handler.ServeHTTP(partialResponse, partial)
	if partialResponse.Code != http.StatusOK {
		t.Fatalf("partial import status=%d body=%s", partialResponse.Code, partialResponse.Body.String())
	}
	accessToken, err := st.GetUploadProviderSecret(ctx, provider.ID, "access_token")
	if err != nil || accessToken != "access-third-party" {
		t.Fatalf("partial import changed access token: value=%q err=%v", accessToken, err)
	}
}

type fakeDirectoryListingProvider struct {
	directories map[string][]upload.RemoteEntry
	listedPaths []string
}

func (p *fakeDirectoryListingProvider) Check(context.Context) error {
	return nil
}

func (p *fakeDirectoryListingProvider) List(_ context.Context, remotePath string) ([]upload.RemoteEntry, error) {
	p.listedPaths = append(p.listedPaths, remotePath)
	entries, ok := p.directories[remotePath]
	if !ok {
		return nil, fmt.Errorf("remote directory %q not found", remotePath)
	}
	return append([]upload.RemoteEntry(nil), entries...), nil
}

func (p *fakeDirectoryListingProvider) Upload(context.Context, string, string, int64, string, string) (upload.RemoteFile, error) {
	return upload.RemoteFile{}, fmt.Errorf("unexpected upload through directory listing test provider")
}

func newDirectoryListingManager(st *store.Store, provider upload.Provider) *upload.Manager {
	return upload.NewWithFactory(upload.DefaultOptions(), st, slog.Default(), func(context.Context, store.UploadBatchTarget) (upload.Provider, error) {
		return provider, nil
	})
}

func newUploadDirectoryTestHandler(t *testing.T, remote *fakeDirectoryListingProvider) (http.Handler, *store.Store, store.UploadProvider) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.CreateUploadProvider(ctx, store.UploadProvider{Name: "Directory Test", Type: store.UploadProviderType115Cookie, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), newDirectoryListingManager(st, remote))
	return handler, st, provider
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
