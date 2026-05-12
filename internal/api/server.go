package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/bootstrap"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/renamer"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/tmdb"
	"NyaMediaMetadataTool/internal/tools"
	"NyaMediaMetadataTool/web"
)

type Server struct {
	cfgMu      sync.RWMutex
	cfg        config.Config
	configPath string
	store      *store.Store
	logger     *slog.Logger
	mux        *http.ServeMux
}

func NewServer(cfg config.Config, configPath string, store *store.Store, logger *slog.Logger) http.Handler {
	server := &Server{
		cfg:        cfg,
		configPath: configPath,
		store:      store,
		logger:     logger,
		mux:        http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/tools/status", s.handleToolsStatus)
	s.mux.HandleFunc("POST /api/tools/check", s.handleToolsCheck)
	s.mux.HandleFunc("GET /api/tasks", s.handleTasks)
	s.mux.HandleFunc("GET /api/tasks/", s.handleTaskDetail)
	s.mux.HandleFunc("GET /api/artifacts", s.handleArtifacts)
	s.mux.HandleFunc("GET /api/fs/directories", s.handleListDirectories)
	s.mux.HandleFunc("POST /api/rename/preview", s.handleRenamePreview)
	s.mux.HandleFunc("POST /api/rename/preview/stream", s.handleRenamePreviewStream)
	s.mux.HandleFunc("POST /api/rename/preview/item", s.handleRenamePreviewItem)
	s.mux.HandleFunc("GET /api/tmdb/search-tv", s.handleTMDBSearchTV)
	s.mux.HandleFunc("GET /api/watch-dirs", s.handleListWatchDirs)
	s.mux.HandleFunc("POST /api/watch-dirs", s.handleCreateWatchDir)
	s.mux.HandleFunc("PUT /api/watch-dirs/", s.handleUpdateWatchDir)
	s.mux.HandleFunc("DELETE /api/watch-dirs/", s.handleDeleteWatchDir)
	s.mux.HandleFunc("POST /api/tasks/rescan", s.handleRescan)
	s.mux.Handle("/", web.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	status := "ok"
	if err := s.store.Ping(ctx); err != nil {
		status = "degraded"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.snapshotConfig())
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	dirs, err := s.store.ListWatchDirs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	next.WatchDirs = make([]config.WatchDir, 0, len(dirs))
	for _, dir := range dirs {
		next.WatchDirs = append(next.WatchDirs, config.WatchDir{Path: dir.Path, Recursive: dir.Recursive, Enabled: dir.Enabled})
	}

	if err := config.Save(s.configPath, next); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.cfgMu.Lock()
	s.cfg = next
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"config": next, "restartRequired": true})
}

func (s *Server) handleToolsStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshotConfig()
	statuses, err := s.store.ListToolStatuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if len(statuses) == 0 {
		statuses = tools.CheckAll(r.Context(), cfg.Tools)
	}
	writeJSON(w, http.StatusOK, statuses)

}

func (s *Server) handleToolsCheck(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshotConfig()
	statuses := tools.CheckAll(r.Context(), cfg.Tools)
	if err := s.store.SaveToolStatuses(r.Context(), statuses); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize == 0 {
		pageSize, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	filters := store.TaskListFilters{
		Page:     page,
		PageSize: pageSize,
		Path:     r.URL.Query().Get("path"),
		From:     r.URL.Query().Get("from"),
		To:       r.URL.Query().Get("to"),
	}
	tasks, err := s.store.ListTasksFiltered(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromPath(w, r, "/api/tasks/")
	if !ok {
		return
	}
	detail, err := s.store.GetTaskDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	artifacts, err := s.store.ListArtifacts(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, artifacts)
}

func (s *Server) handleRenamePreview(w http.ResponseWriter, r *http.Request) {
	var input renamer.PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := renamer.Preview(r.Context(), s.snapshotConfig(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRenamePreviewStream(w http.ResponseWriter, r *http.Request) {
	var input renamer.PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	count := 0
	err := renamer.PreviewEach(r.Context(), s.snapshotConfig(), input, func(item renamer.PreviewItem) error {
		count++
		if err := encoder.Encode(map[string]any{"type": "item", "item": item, "count": count}); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = encoder.Encode(map[string]any{"type": "error", "error": err.Error(), "count": count})
	} else {
		_ = encoder.Encode(map[string]any{"type": "done", "count": count})
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) handleRenamePreviewItem(w http.ResponseWriter, r *http.Request) {
	var input renamer.PreviewItemRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	item, err := renamer.PreviewSingle(r.Context(), s.snapshotConfig(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleTMDBSearchTV(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshotConfig()
	if language := strings.TrimSpace(r.URL.Query().Get("language")); language != "" {
		cfg.Scraping.Language = language
	}
	client, err := tmdb.NewClient(cfg.Scraping)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	results, err := client.SearchTV(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *Server) handleListWatchDirs(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListWatchDirs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, dirs)
}

func (s *Server) handleCreateWatchDir(w http.ResponseWriter, r *http.Request) {
	var input store.WatchDir
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	created, err := s.store.CreateWatchDir(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateWatchDir(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromPath(w, r, "/api/watch-dirs/")
	if !ok {
		return
	}
	var input store.WatchDir
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.ID = id
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	updated, err := s.store.UpdateWatchDir(r.Context(), input)
	if err != nil {
		if errors.Is(err, store.ErrWatchDirNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteWatchDir(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromPath(w, r, "/api/watch-dirs/")
	if !ok {
		return
	}
	if err := s.store.DeleteWatchDir(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrWatchDirNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshotConfig()
	type request struct {
		WatchDirID int64  `json:"watchDirId"`
		Path       string `json:"path"`
		Strategy   string `json:"strategy"`
	}
	var input request
	_ = json.NewDecoder(r.Body).Decode(&input)
	input.Path = strings.TrimSpace(input.Path)
	options := scanOptionsFromStrategy(input.Strategy)

	if input.Path != "" {
		go func() {
			if err := bootstrap.ScanPath(context.Background(), cfg, s.store, s.logger, input.Path, options); err != nil {
				s.logger.Warn("manual path rescan failed", "path", input.Path, "error", err)
			}
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "count": 1})
		return
	}

	var dirs []store.WatchDir
	if input.WatchDirID > 0 {
		dir, err := s.store.GetWatchDir(r.Context(), input.WatchDirID)
		if err != nil {
			if errors.Is(err, store.ErrWatchDirNotFound) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		dirs = []store.WatchDir{dir}
	} else {
		allDirs, err := s.store.ListWatchDirs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		dirs = allDirs
	}

	go func() {
		for _, dir := range dirs {
			cfgDir := config.WatchDir{Path: dir.Path, Recursive: dir.Recursive, Enabled: dir.Enabled}
			if err := bootstrap.ScanWatchDir(context.Background(), cfg, s.store, s.logger, cfgDir, options); err != nil {
				s.logger.Warn("manual rescan failed", "path", dir.Path, "error", err)
			}
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "count": len(dirs)})
}

func scanOptionsFromStrategy(strategy string) bootstrap.ScanOptions {
	switch strings.TrimSpace(strategy) {
	case "force":
		return bootstrap.ScanOptions{OverwriteExisting: true, Force: true}
	case "missing", "":
		return bootstrap.ScanOptions{MissingOnly: true}
	default:
		return bootstrap.ScanOptions{MissingOnly: true}
	}
}

func (s *Server) snapshotConfig() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func parseIDFromPath(w http.ResponseWriter, r *http.Request, prefix string) (int64, bool) {
	value := strings.TrimPrefix(r.URL.Path, prefix)
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}
