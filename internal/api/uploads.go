package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/upload"
)

const uploadProvidersPrefix = "/api/upload/providers/"

type uploadTransferDetailResponse struct {
	store.UploadTransfer
	Phase         string `json:"phase,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
	WaitingUntil  string `json:"waitingUntil,omitempty"`
}

type uploadBatchDetailResponse struct {
	Batch     store.UploadBatch              `json:"batch"`
	Files     []store.UploadBatchFile        `json:"files"`
	Targets   []store.UploadBatchTarget      `json:"targets"`
	Transfers []uploadTransferDetailResponse `json:"transfers"`
}

func (s *Server) handleUploadSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.store.GetUploadSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleUploadBatches(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize == 0 {
		pageSize, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	result, err := s.store.ListUploadBatches(r.Context(), store.UploadBatchFilters{
		Page:     page,
		PageSize: pageSize,
		Status:   r.URL.Query().Get("status"),
		Path:     r.URL.Query().Get("path"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUploadBatchDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUploadID(w, r.URL.Path, "/api/uploads/")
	if !ok {
		return
	}
	detail, err := s.store.GetUploadBatchDetail(r.Context(), id)
	if err != nil {
		writeUploadStoreError(w, err)
		return
	}
	runtimeStates := map[int64]upload.TransferRuntimeState{}
	if s.uploads != nil {
		runtimeStates = s.uploads.TransferRuntimeStates()
	}
	transfers := make([]uploadTransferDetailResponse, 0, len(detail.Transfers))
	for _, transfer := range detail.Transfers {
		state := runtimeStates[transfer.ID]
		transfer = uploadTransferWithRuntimeProgress(transfer, state)
		transfers = append(transfers, uploadTransferDetailResponse{
			UploadTransfer: transfer,
			Phase:          state.Phase,
			StatusMessage:  state.StatusMessage,
			WaitingUntil:   state.WaitingUntil,
		})
	}
	writeJSON(w, http.StatusOK, uploadBatchDetailResponse{
		Batch:     detail.Batch,
		Files:     detail.Files,
		Targets:   detail.Targets,
		Transfers: transfers,
	})
}

func uploadTransferWithRuntimeProgress(transfer store.UploadTransfer, state upload.TransferRuntimeState) store.UploadTransfer {
	bytesTransferred := state.BytesTransferred
	if transfer.BytesTotal > 0 && bytesTransferred > transfer.BytesTotal {
		bytesTransferred = transfer.BytesTotal
	}
	if bytesTransferred > transfer.BytesTransferred {
		transfer.BytesTransferred = bytesTransferred
	}
	return transfer
}

func (s *Server) handleUploadTargetAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/uploads/targets/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, errors.New("upload target action not found"))
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid upload target id"))
		return
	}
	switch parts[1] {
	case "retry":
		err = s.store.RetryUploadTarget(r.Context(), id)
	case "cancel":
		err = s.store.CancelUploadTarget(r.Context(), id)
	default:
		writeError(w, http.StatusNotFound, errors.New("upload target action not found"))
		return
	}
	if err != nil {
		writeUploadStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "targetId": id, "action": parts[1]})
}

func (s *Server) handleUploadEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.store.ListUploadEvents(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleUploadProviderTypes(w http.ResponseWriter, r *http.Request) {
	if s.uploads != nil {
		writeJSON(w, http.StatusOK, s.uploads.ProviderDescriptors())
		return
	}
	writeJSON(w, http.StatusOK, upload.ListProviderDescriptors())
}

func (s *Server) handleClaimUploadEvents(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Limit        int `json:"limit"`
		LeaseSeconds int `json:"leaseSeconds"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	lease := 5 * time.Minute
	if input.LeaseSeconds > 0 {
		lease = time.Duration(input.LeaseSeconds) * time.Second
	}
	events, err := s.store.ClaimUploadEvents(r.Context(), input.Limit, lease)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleUploadEventAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/upload/events/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, errors.New("upload event action not found"))
		return
	}
	eventID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || eventID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid upload event id"))
		return
	}
	var input struct {
		LeaseID    string `json:"leaseId"`
		Error      string `json:"error"`
		RetryAfter int    `json:"retryAfterSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	switch parts[1] {
	case "ack":
		err = s.store.AckUploadEvent(r.Context(), eventID, input.LeaseID)
	case "fail":
		var retryAt time.Time
		if input.RetryAfter > 0 {
			retryAt = time.Now().UTC().Add(time.Duration(input.RetryAfter) * time.Second)
		}
		err = s.store.FailUploadEvent(r.Context(), eventID, input.LeaseID, input.Error, retryAt)
	default:
		writeError(w, http.StatusNotFound, errors.New("upload event action not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "eventId": eventID, "action": parts[1]})
}

func (s *Server) handleListUploadProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListUploadProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleCreateUploadProvider(w http.ResponseWriter, r *http.Request) {
	var input store.UploadProvider
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.validateUploadProviderConfiguration(input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.AuthDevice = ""
	created, err := s.store.CreateUploadProvider(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUploadProviderRoute(w http.ResponseWriter, r *http.Request) {
	parts := splitUploadProviderPath(r.URL.Path)
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, errors.New("upload provider not found"))
		return
	}
	providerID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || providerID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid upload provider id"))
		return
	}
	if len(parts) == 1 {
		s.handleUploadProvider(w, r, providerID)
		return
	}
	if len(parts) == 2 && parts[1] == "cookie" {
		s.handleUploadProviderCookie(w, r, providerID)
		return
	}
	if len(parts) == 3 && parts[1] == "secrets" {
		s.handleUploadProviderSecret(w, r, providerID, parts[2])
		return
	}
	if len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost {
		s.handleUploadProviderCheck(w, r, providerID)
		return
	}
	if len(parts) == 2 && parts[1] == "directories" && r.Method == http.MethodGet {
		s.handleUploadProviderDirectory(w, r, providerID)
		return
	}
	if len(parts) == 3 && parts[1] == "auth" && parts[2] == "115open" {
		s.handleUploadProviderOpen115Auth(w, r, providerID)
		return
	}
	if len(parts) == 5 && parts[1] == "auth" && parts[2] == "115open" && parts[4] == "qrcode" && r.Method == http.MethodGet {
		s.handleUploadProviderOpen115QRCode(w, r, providerID, parts[3])
		return
	}
	if len(parts) == 3 && parts[1] == "auth" && parts[2] == "115cookie" {
		s.handleUploadProviderCookieAuth(w, r, providerID)
		return
	}
	if len(parts) == 5 && parts[1] == "auth" && parts[2] == "115cookie" && parts[4] == "qrcode" && r.Method == http.MethodGet {
		s.handleUploadProviderCookieQRCode(w, r, providerID, parts[3])
		return
	}
	writeError(w, http.StatusNotFound, errors.New("upload provider route not found"))
}

func (s *Server) handleUploadProvider(w http.ResponseWriter, r *http.Request, providerID int64) {
	switch r.Method {
	case http.MethodGet:
		provider, err := s.store.GetUploadProvider(r.Context(), providerID)
		if err != nil {
			writeUploadStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, provider)
	case http.MethodPut:
		var input store.UploadProvider
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.ID = providerID
		if err := s.validateUploadProviderConfiguration(input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := s.store.UpdateUploadProvider(r.Context(), input)
		if err != nil {
			writeUploadStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteUploadProvider(r.Context(), providerID); err != nil {
			writeUploadStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) validateUploadProviderConfiguration(provider store.UploadProvider) error {
	descriptor, found := s.uploadProviderDescriptor(provider.Type)
	if !found {
		return fmt.Errorf("unknown upload provider type %q", provider.Type)
	}
	if provider.Enabled && !descriptor.Implemented {
		return fmt.Errorf("upload provider type %q is not installed", descriptor.Type)
	}
	return nil
}

func (s *Server) uploadProviderDescriptor(providerType string) (upload.ProviderDescriptor, bool) {
	if s.uploads != nil {
		return s.uploads.ProviderDescriptor(providerType)
	}
	for _, descriptor := range upload.ListProviderDescriptors() {
		if descriptor.Type == strings.ToLower(strings.TrimSpace(providerType)) {
			return descriptor, true
		}
	}
	return upload.ProviderDescriptor{}, false
}

func providerSecretKeyAllowed(descriptor upload.ProviderDescriptor, key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, allowed := range descriptor.SecretKeys {
		if strings.EqualFold(allowed, key) {
			return true
		}
	}
	return false
}

func (s *Server) handleUploadProviderCookie(w http.ResponseWriter, r *http.Request, providerID int64) {
	switch r.Method {
	case http.MethodPut:
		var input struct {
			Cookie     string `json:"cookie"`
			AuthDevice string `json:"authDevice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(input.Cookie) == "" {
			writeError(w, http.StatusBadRequest, errors.New("cookie is required"))
			return
		}
		if !upload.IsSupported115CookieDevice(input.AuthDevice) {
			writeError(w, http.StatusBadRequest, errors.New("a supported 115 Cookie authDevice is required"))
			return
		}
		if err := s.store.SetUploadProviderCookie(r.Context(), providerID, input.Cookie, input.AuthDevice); err != nil {
			writeUploadStoreError(w, err)
			return
		}
		provider, _ := s.store.GetUploadProvider(r.Context(), providerID)
		writeJSON(w, http.StatusOK, provider)
	case http.MethodDelete:
		if err := s.store.DeleteUploadProviderSecret(r.Context(), providerID, "cookie"); err != nil {
			writeUploadStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

// handleUploadProviderSecret is the generic credential seam used by future
// Provider types. It deliberately has no GET form: credentials can be set or
// removed, but never read back through the HTTP API.
func (s *Server) handleUploadProviderSecret(w http.ResponseWriter, r *http.Request, providerID int64, key string) {
	key = strings.ToLower(strings.TrimSpace(key))
	provider, err := s.store.GetUploadProvider(r.Context(), providerID)
	if err != nil {
		writeUploadStoreError(w, err)
		return
	}
	descriptor, found := s.uploadProviderDescriptor(provider.Type)
	if !found || !providerSecretKeyAllowed(descriptor, key) {
		writeError(w, http.StatusBadRequest, errors.New("credential key is not supported by this provider"))
		return
	}
	if provider.Type == store.UploadProviderType115Cookie && key == "cookie" && (r.Method == http.MethodPut || r.Method == http.MethodDelete) {
		writeError(w, http.StatusBadRequest, store.ErrUploadProviderCookieOnly)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(input.Value) == "" {
			writeError(w, http.StatusBadRequest, errors.New("credential value is required"))
			return
		}
		if err := s.store.SetUploadProviderSecret(r.Context(), providerID, key, input.Value); err != nil {
			writeUploadStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeleteUploadProviderSecret(r.Context(), providerID, key); err != nil {
			writeUploadStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleUploadProviderCheck(w http.ResponseWriter, r *http.Request, providerID int64) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	if err := s.uploads.CheckProvider(r.Context(), providerID); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "providerId": providerID})
}

func (s *Server) handleUploadProviderDirectory(w http.ResponseWriter, r *http.Request, providerID int64) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	_, err := s.store.GetUploadProvider(r.Context(), providerID)
	if err != nil {
		writeUploadStoreError(w, err)
		return
	}
	remotePath, err := uploadProviderBrowsePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	entries, err := s.uploads.ListProviderDirectory(r.Context(), providerID, remotePath)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": remotePath, "entries": entries})
}

func (s *Server) handleUploadProviderOpen115Auth(w http.ResponseWriter, r *http.Request, providerID int64) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		var input struct {
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		status, err := s.uploads.StartOpen115Auth(r.Context(), providerID, input.ClientID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodPut:
		var input struct {
			ClientID     string `json:"clientId"`
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		provider, err := s.uploads.ImportOpen115Tokens(r.Context(), providerID, input.ClientID, input.AccessToken, input.RefreshToken)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, provider)
	case http.MethodGet:
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, errors.New("sessionId is required"))
			return
		}
		status, err := s.uploads.PollOpen115Auth(r.Context(), providerID, sessionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleUploadProviderOpen115QRCode(w http.ResponseWriter, r *http.Request, providerID int64, sessionID string) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	image, err := s.uploads.Open115AuthQRCode(r.Context(), providerID, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(image)
}

func (s *Server) handleUploadProviderCookieAuth(w http.ResponseWriter, r *http.Request, providerID int64) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		var input struct {
			Terminal string `json:"terminal"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		status, err := s.uploads.StartCookie115Auth(r.Context(), providerID, input.Terminal)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodGet:
		sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, errors.New("sessionId is required"))
			return
		}
		status, err := s.uploads.PollCookie115Auth(r.Context(), providerID, sessionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) handleUploadProviderCookieQRCode(w http.ResponseWriter, r *http.Request, providerID int64, sessionID string) {
	if s.uploads == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("upload manager is unavailable"))
		return
	}
	image, err := s.uploads.Cookie115AuthQRCode(r.Context(), providerID, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(image)
}

func parseUploadID(w http.ResponseWriter, value string, prefix string) (int64, bool) {
	idValue := strings.Trim(strings.TrimPrefix(value, prefix), "/")
	if idValue == "" || strings.Contains(idValue, "/") {
		writeError(w, http.StatusNotFound, errors.New("resource not found"))
		return 0, false
	}
	id, err := strconv.ParseInt(idValue, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid id"))
		return 0, false
	}
	return id, true
}

func splitUploadProviderPath(value string) []string {
	rest := strings.Trim(strings.TrimPrefix(value, uploadProvidersPrefix), "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

func uploadProviderBrowsePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "/" {
		return "/", nil
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", errors.New("path must not contain parent traversal")
		}
	}
	return pathpkg.Clean("/" + strings.TrimPrefix(value, "/")), nil
}

func writeUploadStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrUploadProviderInUse) || errors.Is(err, store.ErrUploadProviderTypeImmutable) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if errors.Is(err, store.ErrUploadTargetNotRetryable) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if errors.Is(err, store.ErrUploadProviderNotFound) || errors.Is(err, store.ErrUploadBatchNotFound) || errors.Is(err, store.ErrUploadTargetNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
