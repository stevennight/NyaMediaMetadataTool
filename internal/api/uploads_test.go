package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
)

func TestUploadProviderCookieNeverAppearsInProviderResponse(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(config.Default().Upload, st, slog.Default()))

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
	saveCookie := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID)+"/cookie", bytes.NewBufferString(`{"cookie":"`+cookie+`"}`))
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
	if len(providers) != 1 || !providers[0].HasCookie {
		t.Fatalf("unexpected providers: %#v", providers)
	}
}

func TestUploadProviderRoutesRoundTripThroughAPI(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	watchDir, err := st.CreateWatchDir(ctx, store.WatchDir{Path: filepath.Join(t.TempDir(), "media"), Recursive: true, WatchEnabled: true, UseGlobalProcessing: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(config.Default().Upload, st, slog.Default()))
	body := `{"name":"115 Archive","type":"115cookie","enabled":true,"remoteRoot":"/Archive","collisionPolicy":"fail","routes":[{"watchDirId":` + jsonNumber(watchDir.ID) + `,"enabled":true,"remoteRoot":"/TV","collisionPolicy":"fail","includeTypes":["video","nfo"]}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(body))
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
	if len(provider.Routes) != 2 {
		t.Fatalf("unexpected provider routes: %#v", provider.Routes)
	}
	var scoped *store.UploadProviderRoute
	var fallback *store.UploadProviderRoute
	for index := range provider.Routes {
		route := &provider.Routes[index]
		if route.WatchDirID == nil {
			fallback = route
		} else {
			scoped = route
		}
	}
	if scoped == nil || scoped.WatchDirID == nil || *scoped.WatchDirID != watchDir.ID || scoped.RemoteRoot != "/TV" || fallback == nil || fallback.Enabled {
		t.Fatalf("unexpected provider route fallback: %#v", provider.Routes)
	}

	update := httptest.NewRequest(http.MethodPut, "/api/upload/providers/"+jsonNumber(provider.ID), bytes.NewBufferString(`{"name":"115 Archive Updated","type":"115cookie","enabled":true,"remoteRoot":"/Archive","collisionPolicy":"fail"}`))
	update.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated store.UploadProvider
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Routes) != 2 {
		t.Fatalf("routes were cleared by an update without routes: %#v", updated.Routes)
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
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), upload.New(config.Default().Upload, st, slog.Default()))
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
	for _, descriptor := range descriptors {
		implemented[descriptor.Type] = descriptor.Implemented
	}
	if !implemented[store.UploadProviderType115Cookie] || implemented[store.UploadProviderType115Open] || implemented[store.UploadProviderType123Pan] || implemented[store.UploadProviderTypeBaiduPan] {
		t.Fatalf("unexpected provider descriptors: %#v", descriptors)
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
	manager := upload.New(config.Default().Upload, st, slog.Default())
	handler := NewServer(config.Default(), filepath.Join(t.TempDir(), "config.yaml"), st, nil, nil, slog.Default(), manager)
	for _, body := range []string{
		`{"name":"Future","type":"115open","enabled":true,"remoteRoot":"/Archive"}`,
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

	disabled := httptest.NewRequest(http.MethodPost, "/api/upload/providers", bytes.NewBufferString(`{"name":"Future disabled","type":"115open","enabled":false,"remoteRoot":"/Archive"}`))
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
	manager := upload.New(config.Default().Upload, st, slog.Default())
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
	manager := upload.New(config.Default().Upload, st, slog.Default())
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

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
