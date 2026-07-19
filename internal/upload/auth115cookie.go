package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"

	"NyaMediaMetadataTool/internal/store"
)

var supported115CookieTerminals = map[string]pan115.LoginApp{
	"web":        pan115.LoginAppWeb,
	"android":    pan115.LoginAppAndroid,
	"ios":        pan115.LoginAppIOS,
	"tv":         pan115.LoginAppTV,
	"alipaymini": pan115.LoginAppAlipayMini,
	"wechatmini": pan115.LoginAppWechatMini,
	"qandroid":   pan115.LoginQAppAndroid,
}

type Cookie115AuthStatus struct {
	SessionID   string `json:"sessionId"`
	ProviderID  int64  `json:"providerId"`
	Terminal    string `json:"terminal"`
	State       string `json:"state"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	CompletedAt string `json:"completedAt"`
}

type cookie115AuthFlow struct {
	Cookie115AuthStatus
	session *pan115.QRCodeSession
}

func (m *Manager) StartCookie115Auth(ctx context.Context, providerID int64, terminal string) (Cookie115AuthStatus, error) {
	provider, err := m.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return Cookie115AuthStatus{}, err
	}
	if provider.Type != store.UploadProviderType115Cookie {
		return Cookie115AuthStatus{}, fmt.Errorf("provider %q does not use 115 Cookie authentication", provider.Name)
	}
	terminal = strings.ToLower(strings.TrimSpace(terminal))
	if terminal == "" {
		terminal = "tv"
	}
	if _, ok := supported115CookieTerminals[terminal]; !ok {
		return Cookie115AuthStatus{}, fmt.Errorf("unsupported 115 Cookie terminal %q", terminal)
	}
	if err := ctx.Err(); err != nil {
		return Cookie115AuthStatus{}, err
	}
	session, err := pan115.New().QRCodeStart()
	if err != nil {
		return Cookie115AuthStatus{}, fmt.Errorf("start 115 QR authorization: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	flow := &cookie115AuthFlow{
		Cookie115AuthStatus: Cookie115AuthStatus{
			SessionID:  newAuthID(),
			ProviderID: providerID,
			Terminal:   terminal,
			State:      "pending",
			Message:    "等待扫码",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		session: session,
	}
	m.authMu.Lock()
	m.pruneAuthFlowsLocked()
	m.authFlows[flow.SessionID] = flow
	m.authMu.Unlock()
	return flow.Cookie115AuthStatus, nil
}

func (m *Manager) PollCookie115Auth(ctx context.Context, providerID int64, sessionID string) (Cookie115AuthStatus, error) {
	flow, ok := m.getAuthFlow(providerID, sessionID)
	if !ok {
		return Cookie115AuthStatus{}, errorsNew("115 Cookie authorization session not found")
	}
	if isCookieAuthTerminal(flow.State) {
		return flow.Cookie115AuthStatus, nil
	}
	if err := ctx.Err(); err != nil {
		return Cookie115AuthStatus{}, err
	}
	client := pan115.New()
	status, err := client.QRCodeStatus(flow.session)
	if err != nil {
		m.updateAuthFlow(sessionID, func(current *cookie115AuthFlow) {
			current.State = "error"
			current.Message = err.Error()
			current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			current.CompletedAt = current.UpdatedAt
		})
		updated, _ := m.getAuthFlow(providerID, sessionID)
		return updated.Cookie115AuthStatus, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	m.updateAuthFlow(sessionID, func(current *cookie115AuthFlow) {
		current.UpdatedAt = now
		switch {
		case status.IsExpired():
			current.State = "expired"
			current.Message = fallbackMessage(status.Msg, "二维码已过期")
			current.CompletedAt = now
		case status.IsCanceled():
			current.State = "cancelled"
			current.Message = fallbackMessage(status.Msg, "已取消授权")
			current.CompletedAt = now
		case status.IsScanned():
			current.State = "scanned"
			current.Message = fallbackMessage(status.Msg, "扫码成功，等待确认")
		case status.IsAllowed():
			current.State = "confirming"
			current.Message = "正在换取 Cookie"
		default:
			current.State = "pending"
			current.Message = fallbackMessage(status.Msg, "等待扫码")
		}
	})
	if !status.IsAllowed() {
		updated, _ := m.getAuthFlow(providerID, sessionID)
		return updated.Cookie115AuthStatus, nil
	}

	credential, err := client.QRCodeLoginWithApp(flow.session, supported115CookieTerminals[flow.Terminal])
	if err != nil {
		m.updateAuthFlow(sessionID, func(current *cookie115AuthFlow) {
			current.State = "error"
			current.Message = err.Error()
			current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			current.CompletedAt = current.UpdatedAt
		})
		updated, _ := m.getAuthFlow(providerID, sessionID)
		return updated.Cookie115AuthStatus, nil
	}
	if err := m.store.SetUploadProviderSecret(ctx, providerID, "cookie", credential.Cookie()); err != nil {
		return Cookie115AuthStatus{}, err
	}
	m.updateAuthFlow(sessionID, func(current *cookie115AuthFlow) {
		current.State = "authorized"
		current.Message = "登录成功"
		current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		current.CompletedAt = current.UpdatedAt
	})
	updated, _ := m.getAuthFlow(providerID, sessionID)
	return updated.Cookie115AuthStatus, nil
}

func (m *Manager) Cookie115AuthQRCode(ctx context.Context, providerID int64, sessionID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	flow, ok := m.getAuthFlow(providerID, sessionID)
	if !ok {
		return nil, errorsNew("115 Cookie authorization session not found")
	}
	if flow.session == nil {
		return nil, errorsNew("115 Cookie authorization QR data is unavailable")
	}
	return flow.session.QRCode()
}

func (m *Manager) initAuthFlows() {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	if m.authFlows == nil {
		m.authFlows = make(map[string]*cookie115AuthFlow)
	}
}

func (m *Manager) getAuthFlow(providerID int64, sessionID string) (*cookie115AuthFlow, bool) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	m.pruneAuthFlowsLocked()
	flow, ok := m.authFlows[sessionID]
	if !ok || flow.ProviderID != providerID {
		return nil, false
	}
	copy := *flow
	return &copy, true
}

func (m *Manager) updateAuthFlow(sessionID string, update func(*cookie115AuthFlow)) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	flow, ok := m.authFlows[sessionID]
	if ok {
		update(flow)
	}
}

func (m *Manager) pruneAuthFlowsLocked() {
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	for id, flow := range m.authFlows {
		updatedAt, err := time.Parse(time.RFC3339, flow.UpdatedAt)
		if err != nil || updatedAt.Before(cutoff) {
			delete(m.authFlows, id)
		}
	}
}

func newAuthID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("auth-%d", time.Now().UTC().UnixNano())
	}
	return "auth-" + hex.EncodeToString(bytes)
}

func isCookieAuthTerminal(state string) bool {
	switch state {
	case "authorized", "expired", "cancelled", "error":
		return true
	default:
		return false
	}
}

func fallbackMessage(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

// errorsNew keeps this file self-contained without exposing an auth-specific
// sentinel to callers.
func errorsNew(value string) error {
	return fmt.Errorf("%s", value)
}
