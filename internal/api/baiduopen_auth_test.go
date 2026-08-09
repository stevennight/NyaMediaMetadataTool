package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
)

type baiduOpenRoundTripFunc func(*http.Request) (*http.Response, error)

func (f baiduOpenRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newBaiduOpenAuthTestServer(t *testing.T) (*Server, *store.Store, store.UploadProvider) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := database.CreateUploadProvider(ctx, store.UploadProvider{Name: "Baidu Auth", Type: store.UploadProviderTypeBaiduPan, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithContext(ctx, config.Default(), filepath.Join(t.TempDir(), "config.yaml"), database, nil, nil, slog.Default(), upload.New(database, slog.Default()))
	return server, database, provider
}

func baiduOpenJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestBaiduOpenOfficialAuthStartUsesSingleCallbackURL(t *testing.T) {
	server, database, provider := newBaiduOpenAuthTestServer(t)
	ctx := context.Background()
	if err := database.SetUploadProviderBaiduOpenApplicationCredentials(ctx, provider.ID, "client-id", "client-secret"); err != nil {
		t.Fatal(err)
	}
	configRequest := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", nil)
	configRequest.Host = "127.0.0.1:18880"
	configResponse := httptest.NewRecorder()
	server.ServeHTTP(configResponse, configRequest)
	if configResponse.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", configResponse.Code, configResponse.Body.String())
	}
	var authConfig baiduOpenAuthConfigResponse
	if err := json.Unmarshal(configResponse.Body.Bytes(), &authConfig); err != nil {
		t.Fatal(err)
	}
	wantCallback := "http://127.0.0.1:18880/api/upload/providers/" + jsonNumber(provider.ID) + "/auth/baiduopen/callback"
	if authConfig.CallbackURL != wantCallback {
		t.Fatalf("callback URL=%q, want %q", authConfig.CallbackURL, wantCallback)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", bytes.NewBufferString(`{"mode":"official"}`))
	request.Host = "127.0.0.1:18880"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", response.Code, response.Body.String())
	}
	var auth baiduOpenAuthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Mode != baiduOpenAuthModeOfficial || auth.CallbackURL != "http://127.0.0.1:18880/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen/callback" {
		t.Fatalf("unexpected callback response: %#v", auth)
	}
	parsed, err := url.Parse(auth.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("client_id") != "client-id" || parsed.Query().Get("redirect_uri") != auth.CallbackURL || parsed.Query().Get("state") == "" {
		t.Fatalf("unexpected official authorization URL: %s", auth.AuthorizationURL)
	}
	server.baiduOAuthHTTPClient = &http.Client{Transport: baiduOpenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Host != "openapi.baidu.com" || request.URL.Path != "/oauth/2.0/token" {
			t.Fatalf("unexpected Baidu OAuth request: %s %s", request.Method, request.URL.String())
		}
		return baiduOpenJSONResponse(http.StatusOK, `{"access_token":"saved-access","refresh_token":"saved-refresh","expires_in":3600}`), nil
	})}
	callback := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen/callback?state="+url.QueryEscape(auth.SessionID)+"&code=authorization-code", nil)
	callback.Host = "127.0.0.1:18880"
	callbackResponse := httptest.NewRecorder()
	server.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", callbackResponse.Code, callbackResponse.Body.String())
	}
	if accessToken, err := database.GetUploadProviderSecret(ctx, provider.ID, "access_token"); err != nil || accessToken != "saved-access" {
		t.Fatalf("saved access token=%q err=%v", accessToken, err)
	}
}

func TestBaiduOpenAuthConfigUsesForwardedHost(t *testing.T) {
	server, _, provider := newBaiduOpenAuthTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", nil)
	request.Host = "wails.localhost"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-Host", "127.0.0.1:18880")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", response.Code, response.Body.String())
	}
	var authConfig baiduOpenAuthConfigResponse
	if err := json.Unmarshal(response.Body.Bytes(), &authConfig); err != nil {
		t.Fatal(err)
	}
	wantCallback := "http://127.0.0.1:18880/api/upload/providers/" + jsonNumber(provider.ID) + "/auth/baiduopen/callback"
	if authConfig.CallbackURL != wantCallback {
		t.Fatalf("callback URL=%q, want %q", authConfig.CallbackURL, wantCallback)
	}
}

func TestBaiduOpenBrokerRelayAndTokenExchange(t *testing.T) {
	server, database, provider := newBaiduOpenAuthTestServer(t)
	ctx := context.Background()
	if err := database.SetUploadProviderBaiduOpenApplicationCredentials(ctx, provider.ID, "client-id", "client-secret"); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"oauth_broker_base_url":  "https://broker.example",
		"oauth_broker_client_id": "broker-client",
		"oauth_broker_token":     "broker-token",
	} {
		if err := database.SetUploadProviderSecret(ctx, provider.ID, key, value); err != nil {
			t.Fatal(err)
		}
	}
	configRequest := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", nil)
	configRequest.Host = "127.0.0.1:18880"
	configResponse := httptest.NewRecorder()
	server.ServeHTTP(configResponse, configRequest)
	var authConfig baiduOpenAuthConfigResponse
	if err := json.Unmarshal(configResponse.Body.Bytes(), &authConfig); err != nil {
		t.Fatal(err)
	}
	if authConfig.BrokerCallbackURL != "https://broker.example/v1/callbacks/baidu" {
		t.Fatalf("broker callback URL=%q", authConfig.BrokerCallbackURL)
	}
	var calls []string
	server.oauthBrokerHTTPClient = &http.Client{Transport: baiduOpenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.Path)
		if request.Header.Get("Authorization") != "Bearer broker-token" || request.Header.Get("X-Broker-Client-ID") != "broker-client" {
			t.Fatalf("broker credentials were not sent")
		}
		switch request.Method + " " + request.URL.Path {
		case "POST /v1/relay/sessions":
			return baiduOpenJSONResponse(http.StatusCreated, `{"session_id":"relay-1","state":"broker-state","redirect_uri":"https://broker.example/v1/callbacks/baidu","expires_at":"2099-01-01T00:00:00Z"}`), nil
		case "POST /v1/token-exchange/sessions":
			return baiduOpenJSONResponse(http.StatusCreated, `{"session_id":"exchange-1","start_url":"https://broker.example/start/one-time","expires_at":"2099-01-01T00:00:00Z"}`), nil
		case "GET /v1/token-exchange/sessions/exchange-1":
			return baiduOpenJSONResponse(http.StatusOK, `{"session_id":"exchange-1","provider":"baidu","status":"completed","expires_at":"2099-01-01T00:00:00Z","completed_at":"2099-01-01T00:00:01Z"}`), nil
		default:
			return baiduOpenJSONResponse(http.StatusNotFound, `{}`), nil
		}
	})}

	relayRequest := httptest.NewRequest(http.MethodPost, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", bytes.NewBufferString(`{"mode":"broker_relay"}`))
	relayResponse := httptest.NewRecorder()
	server.ServeHTTP(relayResponse, relayRequest)
	if relayResponse.Code != http.StatusOK {
		t.Fatalf("relay start status=%d body=%s", relayResponse.Code, relayResponse.Body.String())
	}
	var relay baiduOpenAuthResponse
	if err := json.Unmarshal(relayResponse.Body.Bytes(), &relay); err != nil {
		t.Fatal(err)
	}
	if relay.Mode != baiduOpenAuthModeBrokerRelay || !strings.Contains(relay.AuthorizationURL, "redirect_uri=https%3A%2F%2Fbroker.example%2Fv1%2Fcallbacks%2Fbaidu") {
		t.Fatalf("unexpected relay response: %#v", relay)
	}

	exchangeRequest := httptest.NewRequest(http.MethodPost, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen", bytes.NewBufferString(`{"mode":"broker_token_exchange"}`))
	exchangeResponse := httptest.NewRecorder()
	server.ServeHTTP(exchangeResponse, exchangeRequest)
	if exchangeResponse.Code != http.StatusOK {
		t.Fatalf("exchange start status=%d body=%s", exchangeResponse.Code, exchangeResponse.Body.String())
	}
	var exchange baiduOpenAuthResponse
	if err := json.Unmarshal(exchangeResponse.Body.Bytes(), &exchange); err != nil {
		t.Fatal(err)
	}
	if exchange.Mode != baiduOpenAuthModeTokenExchange || exchange.AuthorizationURL != "https://broker.example/start/one-time" {
		t.Fatalf("unexpected exchange response: %#v", exchange)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/upload/providers/"+jsonNumber(provider.ID)+"/auth/baiduopen?sessionId="+url.QueryEscape(exchange.SessionID), nil)
	statusResponse := httptest.NewRecorder()
	server.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	var status baiduOpenAuthResponse
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "completed" || status.AuthorizationURL != "" {
		t.Fatalf("unexpected exchange status response: %#v", status)
	}
	if len(calls) != 3 {
		t.Fatalf("broker calls=%v, want relay start, exchange start, exchange status", calls)
	}
}
