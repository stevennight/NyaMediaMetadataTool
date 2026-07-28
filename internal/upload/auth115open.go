package upload

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"NyaMediaMetadataTool/internal/store"
)

const (
	open115AuthURL       = "https://passportapi.115.com/open/authDeviceCode"
	open115TokenURL      = "https://passportapi.115.com/open/deviceCodeToToken"
	open115QRStatusURL   = "https://qrcodeapi.115.com/get/status/"
	open115AuthUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

type Open115AuthStatus struct {
	SessionID   string `json:"sessionId"`
	ProviderID  int64  `json:"providerId"`
	ClientID    string `json:"clientId"`
	State       string `json:"state"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	CompletedAt string `json:"completedAt"`
}

type open115AuthFlow struct {
	Open115AuthStatus
	uid, pollTime, sign, qrCode, codeVerifier string
	remote                                    open115AuthRemote
	inFlight                                  bool
}

type open115DeviceCode struct {
	UID, QRCode, Sign string
	Time              int64
}

type open115QRStatus struct {
	State  json.RawMessage
	Status int64
	Msg    string
}

type open115Tokens struct {
	AccessToken, RefreshToken string
	ExpiresIn                 int64
}

type open115AuthRemote interface {
	Start(context.Context, string, string) (open115DeviceCode, error)
	Poll(context.Context, string, string, string) (open115QRStatus, error)
	Exchange(context.Context, string, string) (open115Tokens, error)
}

var newOpen115AuthRemote = func(userAgent string) open115AuthRemote {
	if strings.TrimSpace(userAgent) == "" {
		userAgent = open115AuthUserAgent
	}
	return &open115HTTPAuthRemote{client: &http.Client{Timeout: 15 * time.Second}, userAgent: strings.TrimSpace(userAgent)}
}

func (m *Manager) StartOpen115Auth(ctx context.Context, providerID int64, clientID string) (Open115AuthStatus, error) {
	provider, err := m.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return Open115AuthStatus{}, err
	}
	if provider.Type != store.UploadProviderType115Open {
		return Open115AuthStatus{}, fmt.Errorf("provider %q does not use 115 Open authentication", provider.Name)
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID, err = m.store.GetUploadProviderSecret(ctx, providerID, "client_id")
		if err != nil {
			return Open115AuthStatus{}, err
		}
		clientID = strings.TrimSpace(clientID)
	}
	if clientID == "" {
		return Open115AuthStatus{}, fmt.Errorf("115 Open client_id is required")
	}
	verifier, err := newOpen115CodeVerifier()
	if err != nil {
		return Open115AuthStatus{}, err
	}
	remote := newOpen115AuthRemote(provider.UserAgent)
	device, err := remote.Start(ctx, clientID, open115CodeChallenge(verifier))
	if err != nil {
		return Open115AuthStatus{}, fmt.Errorf("start 115 Open authorization: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	flow := &open115AuthFlow{
		Open115AuthStatus: Open115AuthStatus{SessionID: newAuthID(), ProviderID: providerID, ClientID: clientID, State: "pending", Message: "等待扫码", CreatedAt: now, UpdatedAt: now},
		uid:               device.UID, pollTime: fmt.Sprintf("%d", device.Time), sign: device.Sign, qrCode: device.QRCode, codeVerifier: verifier, remote: remote,
	}
	m.open115AuthMu.Lock()
	m.pruneOpen115AuthFlowsLocked()
	m.open115AuthFlows[flow.SessionID] = flow
	m.open115AuthMu.Unlock()
	return flow.Open115AuthStatus, nil
}

func (m *Manager) ImportOpen115Tokens(ctx context.Context, providerID int64, clientID, accessToken, refreshToken string) (store.UploadProvider, error) {
	if err := m.store.SetUploadProvider115OpenTokens(ctx, providerID, clientID, accessToken, refreshToken); err != nil {
		return store.UploadProvider{}, err
	}
	m.open115Mu.Lock()
	delete(m.open115Sessions, providerID)
	m.open115Mu.Unlock()
	provider, err := m.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return store.UploadProvider{}, err
	}
	return provider, nil
}

func (m *Manager) PollOpen115Auth(ctx context.Context, providerID int64, sessionID string) (Open115AuthStatus, error) {
	flow, ok, claimed := m.claimOpen115AuthFlow(providerID, sessionID)
	if !ok {
		return Open115AuthStatus{}, errorsNew("115 Open authorization session not found")
	}
	if !claimed {
		return flow.Open115AuthStatus, nil
	}
	defer m.releaseOpen115AuthFlowClaim(sessionID)
	status, err := flow.remote.Poll(ctx, flow.uid, flow.pollTime, flow.sign)
	if err != nil {
		m.failOpen115AuthFlow(sessionID, err)
		updated, _ := m.getOpen115AuthFlow(providerID, sessionID)
		return updated.Open115AuthStatus, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	m.updateOpen115AuthFlow(sessionID, func(current *open115AuthFlow) {
		current.UpdatedAt = now
		switch {
		case open115QRStatusExpired(status):
			current.State, current.Message, current.CompletedAt = "expired", fallbackMessage(status.Msg, "二维码已过期"), now
		case status.Status == -2:
			current.State, current.Message, current.CompletedAt = "cancelled", fallbackMessage(status.Msg, "已取消授权"), now
		case status.Status == 1:
			current.State, current.Message = "scanned", fallbackMessage(status.Msg, "扫码成功，等待确认")
		case status.Status == 2:
			current.State, current.Message = "confirming", fallbackMessage(status.Msg, "已确认授权，正在换取令牌")
		default:
			current.State, current.Message = "pending", fallbackMessage(status.Msg, "等待扫码")
		}
	})
	if status.Status != 2 {
		updated, _ := m.getOpen115AuthFlow(providerID, sessionID)
		return updated.Open115AuthStatus, nil
	}
	tokens, err := flow.remote.Exchange(ctx, flow.uid, flow.codeVerifier)
	if err != nil {
		m.failOpen115AuthFlow(sessionID, err)
		updated, _ := m.getOpen115AuthFlow(providerID, sessionID)
		return updated.Open115AuthStatus, nil
	}
	if err := m.store.SetUploadProvider115OpenCredentialsWithExpiry(ctx, providerID, flow.ClientID, tokens.AccessToken, tokens.RefreshToken, open115ExpiresAt(tokens.ExpiresIn, time.Now())); err != nil {
		return Open115AuthStatus{}, err
	}
	m.open115Mu.Lock()
	delete(m.open115Sessions, providerID)
	m.open115Mu.Unlock()
	m.updateOpen115AuthFlow(sessionID, func(current *open115AuthFlow) {
		current.State, current.Message, current.UpdatedAt, current.CompletedAt = "authorized", "授权成功", now, now
	})
	updated, _ := m.getOpen115AuthFlow(providerID, sessionID)
	return updated.Open115AuthStatus, nil
}

func (m *Manager) Open115AuthQRCode(ctx context.Context, providerID int64, sessionID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	flow, ok := m.getOpen115AuthFlow(providerID, sessionID)
	if !ok {
		return nil, errorsNew("115 Open authorization session not found")
	}
	if strings.TrimSpace(flow.qrCode) == "" {
		return nil, errorsNew("115 Open authorization QR data is unavailable")
	}
	return qrcode.Encode(flow.qrCode, qrcode.Medium, 256)
}

func (m *Manager) getOpen115AuthFlow(providerID int64, sessionID string) (*open115AuthFlow, bool) {
	m.open115AuthMu.Lock()
	defer m.open115AuthMu.Unlock()
	m.pruneOpen115AuthFlowsLocked()
	flow, ok := m.open115AuthFlows[sessionID]
	if !ok || flow.ProviderID != providerID {
		return nil, false
	}
	copy := *flow
	return &copy, true
}

func (m *Manager) claimOpen115AuthFlow(providerID int64, sessionID string) (*open115AuthFlow, bool, bool) {
	m.open115AuthMu.Lock()
	defer m.open115AuthMu.Unlock()
	m.pruneOpen115AuthFlowsLocked()
	flow, ok := m.open115AuthFlows[sessionID]
	if !ok || flow.ProviderID != providerID {
		return nil, false, false
	}
	if isOpen115AuthTerminal(flow.State) || flow.inFlight {
		copy := *flow
		return &copy, true, false
	}
	flow.inFlight = true
	copy := *flow
	return &copy, true, true
}

func (m *Manager) releaseOpen115AuthFlowClaim(sessionID string) {
	m.open115AuthMu.Lock()
	defer m.open115AuthMu.Unlock()
	if flow := m.open115AuthFlows[sessionID]; flow != nil {
		flow.inFlight = false
	}
}

func (m *Manager) updateOpen115AuthFlow(sessionID string, update func(*open115AuthFlow)) {
	m.open115AuthMu.Lock()
	defer m.open115AuthMu.Unlock()
	if flow := m.open115AuthFlows[sessionID]; flow != nil {
		update(flow)
	}
}

func (m *Manager) failOpen115AuthFlow(sessionID string, err error) {
	m.updateOpen115AuthFlow(sessionID, func(flow *open115AuthFlow) {
		flow.State, flow.Message = "error", err.Error()
		flow.UpdatedAt, flow.CompletedAt = time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)
	})
}

func (m *Manager) pruneOpen115AuthFlowsLocked() {
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	for id, flow := range m.open115AuthFlows {
		updated, err := time.Parse(time.RFC3339, flow.UpdatedAt)
		if !flow.inFlight && (err != nil || updated.Before(cutoff)) {
			delete(m.open115AuthFlows, id)
		}
	}
}

func isOpen115AuthTerminal(state string) bool {
	return state == "authorized" || state == "expired" || state == "cancelled" || state == "error"
}

func open115QRStatusExpired(status open115QRStatus) bool {
	state := strings.TrimSpace(string(status.State))
	return status.Status == -1 || state == "0" || strings.EqualFold(state, "false")
}

func newOpen115CodeVerifier() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func open115CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type open115HTTPAuthRemote struct {
	client    *http.Client
	userAgent string
}

func (r *open115HTTPAuthRemote) Start(ctx context.Context, clientID, challenge string) (open115DeviceCode, error) {
	var result struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Errno   int64  `json:"errno"`
		Data    struct {
			UID    string `json:"uid"`
			Time   int64  `json:"time"`
			QRCode string `json:"qrcode"`
			Sign   string `json:"sign"`
		} `json:"data"`
	}
	err := r.post(ctx, open115AuthURL, url.Values{"client_id": {clientID}, "code_challenge": {challenge}, "code_challenge_method": {"sha256"}}, &result)
	if err != nil {
		return open115DeviceCode{}, err
	}
	if result.Code != 0 {
		return open115DeviceCode{}, open115AuthError(result.Code, result.Errno, result.Error, result.Message)
	}
	if result.Data.UID == "" || result.Data.QRCode == "" {
		return open115DeviceCode{}, fmt.Errorf("115 Open authorization returned empty QR data")
	}
	return open115DeviceCode{UID: result.Data.UID, Time: result.Data.Time, QRCode: result.Data.QRCode, Sign: result.Data.Sign}, nil
}

func (r *open115HTTPAuthRemote) Poll(ctx context.Context, uid, pollTime, sign string) (open115QRStatus, error) {
	values := url.Values{"uid": {uid}, "time": {pollTime}, "sign": {sign}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, open115QRStatusURL+"?"+values.Encode(), nil)
	if err != nil {
		return open115QRStatus{}, err
	}
	request.Header.Set("User-Agent", r.userAgent)
	response, err := r.client.Do(request)
	if err != nil {
		return open115QRStatus{}, err
	}
	defer response.Body.Close()
	if err := open115HTTPStatusError(response); err != nil {
		return open115QRStatus{}, err
	}
	var result struct {
		State json.RawMessage `json:"state"`
		Msg   string          `json:"msg"`
		Data  struct {
			Msg    string `json:"msg"`
			Status int64  `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return open115QRStatus{}, err
	}
	return open115QRStatus{State: result.State, Status: result.Data.Status, Msg: fallbackMessage(result.Data.Msg, result.Msg)}, nil
}

func (r *open115HTTPAuthRemote) Exchange(ctx context.Context, uid, verifier string) (open115Tokens, error) {
	var result struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Errno   int64  `json:"errno"`
		Data    struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"data"`
	}
	if err := r.post(ctx, open115TokenURL, url.Values{"uid": {uid}, "code_verifier": {verifier}}, &result); err != nil {
		return open115Tokens{}, err
	}
	if result.Code != 0 {
		return open115Tokens{}, open115AuthError(result.Code, result.Errno, result.Error, result.Message)
	}
	if strings.TrimSpace(result.Data.AccessToken) == "" || strings.TrimSpace(result.Data.RefreshToken) == "" {
		return open115Tokens{}, fmt.Errorf("115 Open token response is incomplete")
	}
	return open115Tokens{AccessToken: result.Data.AccessToken, RefreshToken: result.Data.RefreshToken, ExpiresIn: result.Data.ExpiresIn}, nil
}

func (r *open115HTTPAuthRemote) post(ctx context.Context, endpoint string, values url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", r.userAgent)
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := open115HTTPStatusError(response); err != nil {
		return err
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func open115HTTPStatusError(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("115 Open authorization returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
}

func open115AuthError(code, errno int64, remoteError, message string) error {
	if strings.TrimSpace(remoteError) != "" {
		return fmt.Errorf("115 Open authorization error code=%d errno=%d: %s", code, errno, remoteError)
	}
	return fmt.Errorf("115 Open authorization error code=%d: %s", code, message)
}
