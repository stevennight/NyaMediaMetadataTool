package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"NyaMediaMetadataTool/internal/store"
)

const uploadNotificationTemplatesPrefix = "/api/upload/notification-templates/"

func (s *Server) handleListUploadNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListUploadNotificationTemplates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateUploadNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	var input store.UploadNotificationTemplate
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	created, err := s.store.CreateUploadNotificationTemplate(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUploadNotificationTemplate(w http.ResponseWriter, r *http.Request) {
	value := strings.Trim(strings.TrimPrefix(r.URL.Path, uploadNotificationTemplatesPrefix), "/")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strings.Contains(value, "/") {
		writeError(w, http.StatusBadRequest, errors.New("invalid upload notification template id"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetUploadNotificationTemplate(r.Context(), id)
		if err != nil {
			writeUploadNotificationStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var input store.UploadNotificationTemplate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		input.ID = id
		updated, err := s.store.UpdateUploadNotificationTemplate(r.Context(), input)
		if err != nil {
			writeUploadNotificationStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.store.DeleteUploadNotificationTemplate(r.Context(), id); err != nil {
			writeUploadNotificationStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func writeUploadNotificationStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrUploadNotificationTemplateNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrUploadNotificationTemplateInUse):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}
