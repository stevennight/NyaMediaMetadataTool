package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
)

const (
	baiduOpenAuthModeOfficial      = "official"
	baiduOpenAuthModeBrokerRelay   = "broker_relay"
	baiduOpenAuthModeTokenExchange = "broker_token_exchange"
)

type baiduOpenAuthFlow struct {
	ID               string
	ProviderID       int64
	Mode             string
	ClientID         string
	RedirectURI      string
	CallbackURL      string
	AuthorizationURL string
	BrokerSessionID  string
	ExpiresAt        string
	State            string
	Message          string
	CreatedAt        string
	UpdatedAt        string
	CompletedAt      string
}

type baiduOpenAuthResponse struct {
	SessionID        string `json:"sessionId"`
	ProviderID       int64  `json:"providerId"`
	Mode             string `json:"mode"`
	AuthorizationURL string `json:"authorizationUrl,omitempty"`
	RedirectURI      string `json:"redirectUri,omitempty"`
	CallbackURL      string `json:"callbackUrl,omitempty"`
	ExpiresAt        string `json:"expiresAt,omitempty"`
	State            string `json:"state"`
	Message          string `json:"message,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
	CompletedAt      string `json:"completedAt,omitempty"`
}

type baiduOpenAuthConfigResponse struct {
	ProviderID             int64  `json:"providerId"`
	ClientID               string `json:"clientId,omitempty"`
	ClientSecretConfigured bool   `json:"clientSecretConfigured"`
	AccessTokenConfigured  bool   `json:"accessTokenConfigured"`
	RefreshTokenConfigured bool   `json:"refreshTokenConfigured"`
	AuthMode               string `json:"authMode"`
	BrokerBaseURL          string `json:"brokerBaseUrl,omitempty"`
	BrokerClientID         string `json:"brokerClientId,omitempty"`
	BrokerTokenConfigured  bool   `json:"brokerTokenConfigured"`
	BrokerConfigured       bool   `json:"brokerConfigured"`
}

type baiduOpenCredentialsPayload struct {
	ClientID          string `json:"clientId"`
	ClientIDSnake     string `json:"client_id"`
	ClientSecret      string `json:"clientSecret"`
	ClientSecretSnake string `json:"client_secret"`
}

type baiduOpenAuthStartPayload struct {
	Mode string `json:"mode"`
}

type baiduOpenBrokerPayload struct {
	BaseURL       string `json:"baseUrl"`
	BaseURLSnake  string `json:"base_url"`
	ClientID      string `json:"clientId"`
	ClientIDSnake string `json:"client_id"`
	Token         string `json:"token"`
}

type baiduOpenTokenPayload struct {
	AccessToken       string `json:"accessToken"`
	AccessTokenSnake  string `json:"access_token"`
	RefreshToken      string `json:"refreshToken"`
	RefreshTokenSnake string `json:"refresh_token"`
}

type baiduOpenModePayload struct {
	Mode string `json:"mode"`
}

func (s *Server) handleUploadProviderBaiduOpenAuth(w http.ResponseWriter, r *http.Request, providerID int64) {
	provider, err := s.getBaiduOpenProvider(r.Context(), providerID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if sessionID := firstQueryValue(r, "sessionId", "session_id"); sessionID != "" {
			s.handleUploadProviderBaiduOpenAuthStatus(w, r, *provider, sessionID)
			return
		}
		secrets, err := s.loadBaiduOpenSecrets(r.Context(), providerID)
		if err != nil {
			s.writeBaiduOpenStoreError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, toBaiduOpenAuthConfigResponse(*provider, secrets))
	case http.MethodPut:
		s.handleUploadProviderBaiduOpenCredentials(w, r, *provider)
	case http.MethodPost:
		s.handleUploadProviderBaiduOpenAuthStart(w, r, *provider)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleUploadProviderBaiduOpenCredentials(w http.ResponseWriter, r *http.Request, provider store.UploadProvider) {
	var payload baiduOpenCredentialsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		clientID = strings.TrimSpace(payload.ClientIDSnake)
	}
	clientSecret := strings.TrimSpace(payload.ClientSecret)
	if clientSecret == "" {
		clientSecret = strings.TrimSpace(payload.ClientSecretSnake)
	}
	if err := s.store.SetUploadProviderBaiduOpenApplicationCredentials(r.Context(), provider.ID, clientID, clientSecret); err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	secrets, err := s.loadBaiduOpenSecrets(r.Context(), provider.ID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, toBaiduOpenAuthConfigResponse(provider, secrets))
}

func (s *Server) handleUploadProviderBaiduOpenBroker(w http.ResponseWriter, r *http.Request, providerID int64) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if _, err := s.getBaiduOpenProvider(r.Context(), providerID); err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	secrets, err := s.loadBaiduOpenSecrets(r.Context(), providerID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	if r.Method == http.MethodPut {
		var payload baiduOpenBrokerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		baseURL := strings.TrimSpace(payload.BaseURL)
		if baseURL == "" {
			baseURL = strings.TrimSpace(payload.BaseURLSnake)
		}
		baseURL, err = validateOAuthBrokerBaseURL(baseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		clientID := strings.TrimSpace(payload.ClientID)
		if clientID == "" {
			clientID = strings.TrimSpace(payload.ClientIDSnake)
		}
		token := strings.TrimSpace(payload.Token)
		if token == "" {
			token = strings.TrimSpace(secrets["oauth_broker_token"])
		}
		if clientID == "" || token == "" {
			writeError(w, http.StatusBadRequest, errors.New("oauth broker client ID and token are required"))
			return
		}
		for key, value := range map[string]string{
			"oauth_broker_base_url":  baseURL,
			"oauth_broker_client_id": clientID,
			"oauth_broker_token":     token,
		} {
			if err := s.store.SetUploadProviderSecret(r.Context(), providerID, key, value); err != nil {
				s.writeBaiduOpenStoreError(w, err)
				return
			}
			secrets[key] = value
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"baseUrl":         strings.TrimSpace(secrets["oauth_broker_base_url"]),
		"clientId":        strings.TrimSpace(secrets["oauth_broker_client_id"]),
		"tokenConfigured": strings.TrimSpace(secrets["oauth_broker_token"]) != "",
		"configured":      strings.TrimSpace(secrets["oauth_broker_base_url"]) != "" && strings.TrimSpace(secrets["oauth_broker_client_id"]) != "" && strings.TrimSpace(secrets["oauth_broker_token"]) != "",
	})
}

func (s *Server) handleUploadProviderBaiduOpenMode(w http.ResponseWriter, r *http.Request, providerID int64) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if _, err := s.getBaiduOpenProvider(r.Context(), providerID); err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	var payload baiduOpenModePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	mode := strings.TrimSpace(payload.Mode)
	if !isBaiduOpenAuthMode(mode) {
		writeError(w, http.StatusBadRequest, errors.New("mode must be official, broker_relay, or broker_token_exchange"))
		return
	}
	if err := s.store.SetUploadProviderSecret(r.Context(), providerID, "auth_mode", mode); err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providerId": providerID, "authMode": mode})
}

func (s *Server) handleUploadProviderBaiduOpenTokens(w http.ResponseWriter, r *http.Request, providerID int64) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	provider, err := s.getBaiduOpenProvider(r.Context(), providerID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	var payload baiduOpenTokenPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(payload.AccessTokenSnake)
	}
	refreshToken := strings.TrimSpace(payload.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(payload.RefreshTokenSnake)
	}
	if accessToken == "" || refreshToken == "" {
		writeError(w, http.StatusBadRequest, errors.New("access token and refresh token are required"))
		return
	}
	secrets, err := s.loadBaiduOpenSecrets(r.Context(), providerID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	clientID := strings.TrimSpace(secrets["client_id"])
	clientSecret := strings.TrimSpace(secrets["client_secret"])
	if clientID == "" || clientSecret == "" {
		writeError(w, http.StatusBadRequest, errors.New("save the matching Baidu client ID and client secret before importing tokens"))
		return
	}
	if err := upload.ValidateBaiduOpenAccessToken(r.Context(), s.baiduOAuthHTTPClient, accessToken); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("Baidu access token validation failed: %w", err))
		return
	}
	refreshed, err := upload.RefreshBaiduOpenOAuthToken(r.Context(), s.baiduOAuthHTTPClient, clientID, clientSecret, refreshToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("Baidu token refresh validation failed: %w", err))
		return
	}
	if strings.TrimSpace(refreshed.RefreshToken) == "" {
		refreshed.RefreshToken = refreshToken
	}
	expiresAt := baiduOpenAccessTokenExpiresAt(refreshed.ExpiresIn, time.Now())
	if err := s.store.SetUploadProviderBaiduPanRefreshedTokens(r.Context(), providerID, refreshed.AccessToken, refreshed.RefreshToken, expiresAt); err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	secrets["access_token"] = refreshed.AccessToken
	secrets["refresh_token"] = refreshed.RefreshToken
	secrets["access_token_expires_at"] = expiresAt
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, toBaiduOpenAuthConfigResponse(*provider, secrets))
}

func (s *Server) handleUploadProviderBaiduOpenAuthStart(w http.ResponseWriter, r *http.Request, provider store.UploadProvider) {
	var payload baiduOpenAuthStartPayload
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	secrets, err := s.loadBaiduOpenSecrets(r.Context(), provider.ID)
	if err != nil {
		s.writeBaiduOpenStoreError(w, err)
		return
	}
	mode := strings.TrimSpace(payload.Mode)
	if mode == "" {
		mode = baiduOpenAuthModeFromSecrets(secrets)
	}
	if !isBaiduOpenAuthMode(mode) {
		writeError(w, http.StatusBadRequest, errors.New("mode must be official, broker_relay, or broker_token_exchange"))
		return
	}
	clientID := strings.TrimSpace(secrets["client_id"])
	clientSecret := strings.TrimSpace(secrets["client_secret"])
	if mode != baiduOpenAuthModeTokenExchange && (clientID == "" || clientSecret == "") {
		writeError(w, http.StatusBadRequest, errors.New("Baidu client ID and client secret are required before this authorization mode"))
		return
	}
	callbackURL := ""
	if mode != baiduOpenAuthModeTokenExchange {
		callbackURL, err = s.baiduOpenCallbackURL(r, provider.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	sessionID := newBaiduOpenAuthID()
	now := time.Now().UTC().Format(time.RFC3339)
	flow := &baiduOpenAuthFlow{
		ID: sessionID, ProviderID: provider.ID, Mode: mode, ClientID: clientID,
		CallbackURL: callbackURL, RedirectURI: callbackURL, State: "pending",
		Message: "Waiting for Baidu authorization", CreatedAt: now, UpdatedAt: now,
	}
	switch mode {
	case baiduOpenAuthModeOfficial:
		flow.AuthorizationURL = upload.BaiduOpenAuthorizationURL(clientID, callbackURL, sessionID)
	case baiduOpenAuthModeBrokerRelay:
		brokerCredentials, err := baiduOpenBrokerCredentialsFromSecrets(secrets)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		relay, err := s.createOAuthBrokerRelaySession(r.Context(), brokerCredentials, callbackURL, sessionID)
		if err != nil {
			writeError(w, oauthBrokerHTTPStatus(err), err)
			return
		}
		flow.BrokerSessionID = relay.SessionID
		flow.RedirectURI = relay.RedirectURI
		flow.ExpiresAt = relay.ExpiresAt
		flow.AuthorizationURL = upload.BaiduOpenAuthorizationURL(clientID, relay.RedirectURI, relay.State)
	case baiduOpenAuthModeTokenExchange:
		brokerCredentials, err := baiduOpenBrokerCredentialsFromSecrets(secrets)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		exchange, err := s.createOAuthBrokerTokenExchangeSession(r.Context(), brokerCredentials)
		if err != nil {
			writeError(w, oauthBrokerHTTPStatus(err), err)
			return
		}
		flow.BrokerSessionID = exchange.SessionID
		flow.ExpiresAt = exchange.ExpiresAt
		flow.AuthorizationURL = exchange.StartURL
		flow.RedirectURI = ""
		flow.Message = "Waiting for authorization in OAuth Broker"
	}
	response := toBaiduOpenAuthResponse(flow)
	if mode == baiduOpenAuthModeTokenExchange {
		flow.AuthorizationURL = ""
	}
	s.baiduAuthMu.Lock()
	s.pruneBaiduOpenAuthFlowsLocked()
	s.baiduAuthFlows[sessionID] = flow
	s.baiduAuthMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleUploadProviderBaiduOpenAuthStatus(w http.ResponseWriter, r *http.Request, provider store.UploadProvider, sessionID string) {
	flow, ok := s.getBaiduOpenAuthFlow(provider.ID, sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("Baidu Open authorization session not found"))
		return
	}
	if flow.Mode == baiduOpenAuthModeTokenExchange && flow.State == "pending" {
		secrets, err := s.loadBaiduOpenSecrets(r.Context(), provider.ID)
		if err != nil {
			s.writeBaiduOpenStoreError(w, err)
			return
		}
		credentials, err := baiduOpenBrokerCredentialsFromSecrets(secrets)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		status, err := s.getOAuthBrokerTokenExchangeStatus(r.Context(), credentials, flow.BrokerSessionID)
		if err != nil {
			writeError(w, oauthBrokerHTTPStatus(err), err)
			return
		}
		flow = s.updateBaiduOpenTokenExchangeFlow(flow.ID, status)
		if flow == nil {
			writeError(w, http.StatusNotFound, errors.New("Baidu Open authorization session not found"))
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, toBaiduOpenAuthResponse(flow))
}

func (s *Server) handleUploadProviderBaiduOpenCallback(w http.ResponseWriter, r *http.Request, providerID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	clientState := strings.TrimSpace(r.URL.Query().Get("client_state"))
	if (state == "") == (clientState == "") {
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Authorization callback state is invalid")
		return
	}
	flowID := state
	if clientState != "" {
		flowID = clientState
	}
	flow, ok := s.claimBaiduOpenAuthFlow(providerID, flowID)
	if !ok {
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Authorization session is invalid or expired")
		return
	}
	if flow.Mode == baiduOpenAuthModeTokenExchange || (flow.Mode == baiduOpenAuthModeOfficial && clientState != "") || (flow.Mode == baiduOpenAuthModeBrokerRelay && state != "") {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "authorization callback mode mismatch")
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Authorization callback mode mismatch")
		return
	}
	if flow.Mode == baiduOpenAuthModeBrokerRelay && strings.TrimSpace(r.URL.Query().Get("provider")) != "baidu" {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "unexpected callback provider")
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Unexpected authorization provider")
		return
	}
	oauthError := strings.TrimSpace(r.URL.Query().Get("error"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if (oauthError == "") == (code == "") {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "callback must contain exactly one of code or error")
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Authorization callback must contain code or error")
		return
	}
	if oauthError != "" {
		message := oauthError
		if flow.Mode == baiduOpenAuthModeOfficial {
			if description := strings.TrimSpace(r.URL.Query().Get("error_description")); description != "" {
				message = description
			}
		} else if oauthError != "authorization_denied" && oauthError != "authorization_failed" {
			message = "authorization_failed"
		}
		s.finishBaiduOpenAuthFlow(flow.ID, "error", message)
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Baidu authorization failed: "+message)
		return
	}
	provider, err := s.getBaiduOpenProvider(r.Context(), providerID)
	if err != nil {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "provider is unavailable")
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Baidu provider is unavailable")
		return
	}
	secrets, err := s.loadBaiduOpenSecrets(r.Context(), providerID)
	if err != nil {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", err.Error())
		writeBaiduOpenCallbackPage(w, http.StatusInternalServerError, "Could not read Baidu credentials")
		return
	}
	clientID := strings.TrimSpace(secrets["client_id"])
	clientSecret := strings.TrimSpace(secrets["client_secret"])
	if clientID == "" || clientSecret == "" || clientID != flow.ClientID {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "application credentials changed during authorization")
		writeBaiduOpenCallbackPage(w, http.StatusBadRequest, "Baidu application credentials changed; start authorization again")
		return
	}
	token, err := upload.ExchangeBaiduOpenAuthorizationCode(r.Context(), s.baiduOAuthHTTPClient, clientID, clientSecret, code, flow.RedirectURI)
	if err != nil {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "Baidu token exchange failed")
		writeBaiduOpenCallbackPage(w, http.StatusBadGateway, "Could not exchange the Baidu authorization code")
		return
	}
	if err := s.store.SetUploadProviderBaiduPanRefreshedTokens(r.Context(), provider.ID, token.AccessToken, token.RefreshToken, baiduOpenAccessTokenExpiresAt(token.ExpiresIn, time.Now())); err != nil {
		s.finishBaiduOpenAuthFlow(flow.ID, "error", "failed to persist Baidu tokens")
		writeBaiduOpenCallbackPage(w, http.StatusConflict, "Baidu tokens could not be saved")
		return
	}
	s.finishBaiduOpenAuthFlow(flow.ID, "authorized", "Baidu authorization succeeded")
	writeBaiduOpenCallbackPage(w, http.StatusOK, "Baidu authorization succeeded. You can close this window.")
}

func (s *Server) getBaiduOpenProvider(ctx context.Context, providerID int64) (*store.UploadProvider, error) {
	provider, err := s.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider.Type != store.UploadProviderTypeBaiduPan {
		return nil, errors.New("provider does not use Baidu Open authentication")
	}
	return &provider, nil
}

func (s *Server) loadBaiduOpenSecrets(ctx context.Context, providerID int64) (map[string]string, error) {
	keys := []string{"client_id", "client_secret", "access_token", "refresh_token", "access_token_expires_at", "auth_mode", "oauth_broker_base_url", "oauth_broker_client_id", "oauth_broker_token"}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := s.store.GetUploadProviderSecret(ctx, providerID, key)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func (s *Server) writeBaiduOpenStoreError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, store.ErrUploadProviderNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err)
}

func toBaiduOpenAuthConfigResponse(provider store.UploadProvider, secrets map[string]string) baiduOpenAuthConfigResponse {
	baseURL := strings.TrimSpace(secrets["oauth_broker_base_url"])
	brokerClientID := strings.TrimSpace(secrets["oauth_broker_client_id"])
	brokerTokenConfigured := strings.TrimSpace(secrets["oauth_broker_token"]) != ""
	return baiduOpenAuthConfigResponse{
		ProviderID:             provider.ID,
		ClientID:               strings.TrimSpace(secrets["client_id"]),
		ClientSecretConfigured: strings.TrimSpace(secrets["client_secret"]) != "",
		AccessTokenConfigured:  strings.TrimSpace(secrets["access_token"]) != "",
		RefreshTokenConfigured: strings.TrimSpace(secrets["refresh_token"]) != "",
		AuthMode:               baiduOpenAuthModeFromSecrets(secrets),
		BrokerBaseURL:          baseURL,
		BrokerClientID:         brokerClientID,
		BrokerTokenConfigured:  brokerTokenConfigured,
		BrokerConfigured:       baseURL != "" && brokerClientID != "" && brokerTokenConfigured,
	}
}

func toBaiduOpenAuthResponse(flow *baiduOpenAuthFlow) baiduOpenAuthResponse {
	if flow == nil {
		return baiduOpenAuthResponse{}
	}
	return baiduOpenAuthResponse{SessionID: flow.ID, ProviderID: flow.ProviderID, Mode: flow.Mode, AuthorizationURL: flow.AuthorizationURL, RedirectURI: flow.RedirectURI, CallbackURL: flow.CallbackURL, ExpiresAt: flow.ExpiresAt, State: flow.State, Message: flow.Message, CreatedAt: flow.CreatedAt, UpdatedAt: flow.UpdatedAt, CompletedAt: flow.CompletedAt}
}

func baiduOpenBrokerCredentialsFromSecrets(secrets map[string]string) (oauthBrokerCredentials, error) {
	baseURL, err := validateOAuthBrokerBaseURL(secrets["oauth_broker_base_url"])
	if err != nil {
		return oauthBrokerCredentials{}, err
	}
	credentials := oauthBrokerCredentials{BaseURL: baseURL, ClientID: strings.TrimSpace(secrets["oauth_broker_client_id"]), Token: strings.TrimSpace(secrets["oauth_broker_token"])}
	if credentials.ClientID == "" || credentials.Token == "" {
		return oauthBrokerCredentials{}, errors.New("oauth broker client ID and token are required")
	}
	return credentials, nil
}

func baiduOpenAuthModeFromSecrets(secrets map[string]string) string {
	mode := strings.TrimSpace(secrets["auth_mode"])
	if isBaiduOpenAuthMode(mode) {
		return mode
	}
	return baiduOpenAuthModeOfficial
}

func isBaiduOpenAuthMode(mode string) bool {
	switch mode {
	case baiduOpenAuthModeOfficial, baiduOpenAuthModeBrokerRelay, baiduOpenAuthModeTokenExchange:
		return true
	default:
		return false
	}
}

func (s *Server) baiduOpenCallbackURL(r *http.Request, providerID int64) (string, error) {
	baseURL := strings.TrimSpace(s.snapshotConfig().Server.PublicBaseURL)
	if baseURL == "" {
		scheme := "http"
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "https" || forwarded == "http" {
			scheme = forwarded
		} else if r.TLS != nil {
			scheme = "https"
		}
		host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		if host == "" {
			host = strings.TrimSpace(r.Host)
		}
		if host == "" {
			return "", errors.New("request host is required to build the Baidu OAuth callback URL")
		}
		baseURL = scheme + "://" + host
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("server public base URL must be an absolute HTTP or HTTPS URL without query or fragment")
	}
	callback := strings.TrimRight(parsed.String(), "/") + fmt.Sprintf("/api/upload/providers/%d/auth/baiduopen/callback", providerID)
	return validateBaiduOpenRedirectURI(callback)
}

func validateBaiduOpenRedirectURI(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("redirect URI must be an absolute HTTP or HTTPS URL without user info or fragment")
	}
	return parsed.String(), nil
}

func (s *Server) getBaiduOpenAuthFlow(providerID int64, sessionID string) (*baiduOpenAuthFlow, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, false
	}
	s.baiduAuthMu.Lock()
	defer s.baiduAuthMu.Unlock()
	s.pruneBaiduOpenAuthFlowsLocked()
	flow, ok := s.baiduAuthFlows[sessionID]
	if !ok || flow.ProviderID != providerID {
		return nil, false
	}
	copy := *flow
	return &copy, true
}

func (s *Server) claimBaiduOpenAuthFlow(providerID int64, sessionID string) (*baiduOpenAuthFlow, bool) {
	sessionID = strings.TrimSpace(sessionID)
	s.baiduAuthMu.Lock()
	defer s.baiduAuthMu.Unlock()
	s.pruneBaiduOpenAuthFlowsLocked()
	flow, ok := s.baiduAuthFlows[sessionID]
	if !ok || flow.ProviderID != providerID || flow.State != "pending" {
		return nil, false
	}
	if flow.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, flow.ExpiresAt)
		if err != nil || !time.Now().UTC().Before(expiresAt) {
			flow.State, flow.Message = "expired", "Authorization session expired"
			flow.UpdatedAt, flow.CompletedAt = time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)
			return nil, false
		}
	}
	flow.State, flow.Message, flow.UpdatedAt = "processing", "Processing authorization callback", time.Now().UTC().Format(time.RFC3339)
	copy := *flow
	return &copy, true
}

func (s *Server) finishBaiduOpenAuthFlow(sessionID, state, message string) {
	s.baiduAuthMu.Lock()
	defer s.baiduAuthMu.Unlock()
	if flow := s.baiduAuthFlows[sessionID]; flow != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		flow.State, flow.Message, flow.UpdatedAt, flow.CompletedAt = state, strings.TrimSpace(message), now, now
	}
}

func (s *Server) updateBaiduOpenTokenExchangeFlow(sessionID string, status *oauthBrokerTokenExchangeStatus) *baiduOpenAuthFlow {
	if status == nil {
		return nil
	}
	s.baiduAuthMu.Lock()
	defer s.baiduAuthMu.Unlock()
	flow, ok := s.baiduAuthFlows[sessionID]
	if !ok {
		return nil
	}
	flow.State = status.Status
	flow.ExpiresAt = status.ExpiresAt
	flow.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	flow.CompletedAt = status.CompletedAt
	switch status.Status {
	case "pending":
		flow.Message = "Waiting for authorization in OAuth Broker"
	case "completed":
		flow.Message = "Broker completed authorization; import the displayed tokens"
	case "failed":
		flow.Message = "OAuth Broker authorization failed: " + strings.TrimSpace(status.FailureCode)
	case "expired":
		flow.Message = "OAuth Broker authorization session expired"
	}
	copy := *flow
	return &copy
}

func (s *Server) pruneBaiduOpenAuthFlowsLocked() {
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	for id, flow := range s.baiduAuthFlows {
		updatedAt, err := time.Parse(time.RFC3339, flow.UpdatedAt)
		if err != nil || updatedAt.Before(cutoff) {
			delete(s.baiduAuthFlows, id)
		}
	}
}

func newBaiduOpenAuthID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("baiduauth-%d", time.Now().UnixNano())
	}
	return "baiduauth-" + hex.EncodeToString(buffer)
}

func baiduOpenAccessTokenExpiresAt(expiresIn int64, now time.Time) string {
	if expiresIn <= 0 {
		return ""
	}
	return now.UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
}

func firstQueryValue(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func writeBaiduOpenCallbackPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Baidu Open authorization</title></head><body><p>%s</p></body></html>", html.EscapeString(message))
}
