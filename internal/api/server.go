package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/bootstrap"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/fileaudit"
	"NyaMediaMetadataTool/internal/metadataaudit"
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
	tasks      TaskCanceller
	watcher    WatchDirReloader
	logger     *slog.Logger
	mux        *http.ServeMux
}

type TaskCanceller interface {
	CancelRunningTasks() int
}

type WatchDirReloader interface {
	ReloadWatchDirs(ctx context.Context) error
}

func NewServer(cfg config.Config, configPath string, store *store.Store, tasks TaskCanceller, watcher WatchDirReloader, logger *slog.Logger) http.Handler {
	server := &Server{
		cfg:        cfg,
		configPath: configPath,
		store:      store,
		tasks:      tasks,
		watcher:    watcher,
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
	s.mux.HandleFunc("POST /api/tasks/cancel-active", s.handleCancelActiveTasks)
	s.mux.HandleFunc("POST /api/tasks/retry", s.handleRetryTasks)
	s.mux.HandleFunc("POST /api/tasks/ignore", s.handleIgnoreTasks)
	s.mux.HandleFunc("GET /api/tasks/", s.handleTaskDetail)
	s.mux.HandleFunc("GET /api/artifacts", s.handleArtifacts)
	s.mux.HandleFunc("GET /api/emby-api-keys", s.handleListEmbyAPIKeys)
	s.mux.HandleFunc("POST /api/emby-api-keys", s.handleSaveEmbyAPIKey)
	s.mux.HandleFunc("DELETE /api/emby-api-keys/", s.handleDeleteEmbyAPIKey)
	s.mux.HandleFunc("GET /api/fs/directories", s.handleListDirectories)
	s.mux.HandleFunc("POST /api/rename/preview", s.handleRenamePreview)
	s.mux.HandleFunc("POST /api/rename/preview/stream", s.handleRenamePreviewStream)
	s.mux.HandleFunc("POST /api/rename/preview/item", s.handleRenamePreviewItem)
	s.mux.HandleFunc("POST /api/rename/apply", s.handleRenameApply)
	s.mux.HandleFunc("POST /api/audit/series", s.handleSeriesAudit)
	s.mux.HandleFunc("POST /api/audit/files", s.handleFileAudit)
	s.mux.HandleFunc("GET /api/rename/history", s.handleRenameHistory)
	s.mux.HandleFunc("GET /api/rename/history/{id}/undo-check", s.handleRenameHistoryUndoCheck)
	s.mux.HandleFunc("POST /api/rename/history/{id}/undo", s.handleRenameHistoryUndo)
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
		Status:   r.URL.Query().Get("status"),
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

func (s *Server) handleCancelActiveTasks(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.CancelActiveTasks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	running := 0
	if s.tasks != nil {
		running = s.tasks.CancelRunningTasks()
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "canceled", "count": count, "running": running})
}

func (s *Server) handleRetryTasks(w http.ResponseWriter, r *http.Request) {
	type request struct {
		IDs []int64 `json:"ids"`
	}
	var input request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("ids are required"))
		return
	}
	count, err := s.store.RetryTasks(r.Context(), input.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "count": count})
}

func (s *Server) handleIgnoreTasks(w http.ResponseWriter, r *http.Request) {
	type request struct {
		IDs []int64 `json:"ids"`
	}
	var input request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("ids are required"))
		return
	}
	count, err := s.store.IgnoreTasks(r.Context(), input.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "count": count})
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

func (s *Server) handleListEmbyAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListEmbyAPIKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleSaveEmbyAPIKey(w http.ResponseWriter, r *http.Request) {
	var input store.EmbyAPIKey
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.Title == "" || input.APIKey == "" {
		writeError(w, http.StatusBadRequest, errors.New("title and apiKey are required"))
		return
	}
	created, err := s.store.SaveEmbyAPIKey(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	created.APIKey = ""
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleDeleteEmbyAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDFromPath(w, r, "/api/emby-api-keys/")
	if !ok {
		return
	}
	if err := s.store.DeleteEmbyAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrEmbyAPIKeyNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) handleRenameApply(w http.ResponseWriter, r *http.Request) {
	var input renamer.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := renamer.Apply(renamer.HistoryPath(s.snapshotConfig()), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSeriesAudit(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Root         string `json:"root"`
		TMDBShowID   int    `json:"tmdbShowId"`
		EmbyItemURL  string `json:"embyItemUrl"`
		EmbyURL      string `json:"embyUrl"`
		EmbyAPIKey   string `json:"embyApiKey"`
		EmbyAPIKeyID int64  `json:"embyApiKeyId"`
		EmbySeriesID string `json:"embySeriesId"`
	}
	var input request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Root = strings.TrimSpace(input.Root)
	if input.Root == "" {
		writeError(w, http.StatusBadRequest, errors.New("root is required"))
		return
	}
	embyAPIKey := strings.TrimSpace(input.EmbyAPIKey)
	if embyAPIKey == "" && input.EmbyAPIKeyID > 0 {
		storedKey, err := s.store.GetEmbyAPIKey(r.Context(), input.EmbyAPIKeyID)
		if err != nil {
			if errors.Is(err, store.ErrEmbyAPIKeyNotFound) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		embyAPIKey = storedKey.APIKey
	}
	report, err := metadataaudit.Run(r.Context(), metadataaudit.Options{
		Root:         input.Root,
		Config:       s.snapshotConfig(),
		TMDBShowID:   input.TMDBShowID,
		EmbyItemURL:  input.EmbyItemURL,
		EmbyURL:      input.EmbyURL,
		EmbyAPIKey:   embyAPIKey,
		EmbySeriesID: input.EmbySeriesID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleFileAudit(w http.ResponseWriter, r *http.Request) {
	type request struct {
		LocalRoot              string `json:"localRoot"`
		RemoteRoot             string `json:"remoteRoot"`
		SFTPAddr               string `json:"sftpAddr"`
		SFTPUser               string `json:"sftpUser"`
		SFTPPassword           string `json:"sftpPassword"`
		SFTPKeyPath            string `json:"sftpKeyPath"`
		SFTPKnownHostsPath     string `json:"sftpKnownHostsPath"`
		SFTPInsecureIgnoreHost bool   `json:"sftpInsecureIgnoreHost"`
		AllowSTRMProxy         bool   `json:"allowStrmProxy"`
		CompareSize            bool   `json:"compareSize"`
		CompareMD5             bool   `json:"compareMd5"`
	}
	var input request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.LocalRoot = strings.TrimSpace(input.LocalRoot)
	input.RemoteRoot = strings.TrimSpace(input.RemoteRoot)
	if input.LocalRoot == "" || input.RemoteRoot == "" || strings.TrimSpace(input.SFTPAddr) == "" || strings.TrimSpace(input.SFTPUser) == "" {
		writeError(w, http.StatusBadRequest, errors.New("local root, remote root, sftp addr and sftp user are required"))
		return
	}

	remoteFS, err := fileaudit.NewSFTPFS(r.Context(), fileaudit.SFTPConfig{
		Addr:                  input.SFTPAddr,
		User:                  input.SFTPUser,
		Password:              input.SFTPPassword,
		KeyPath:               input.SFTPKeyPath,
		KnownHostsPath:        input.SFTPKnownHostsPath,
		InsecureIgnoreHostKey: input.SFTPInsecureIgnoreHost,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer remoteFS.Close()

	report, err := fileaudit.Compare(r.Context(), fileaudit.Options{
		LocalRoot:       input.LocalRoot,
		RemoteRoot:      input.RemoteRoot,
		RemoteFS:        remoteFS,
		VideoExtensions: s.snapshotConfig().Processing.Extensions,
		AllowSTRMProxy:  input.AllowSTRMProxy,
		CompareSize:     input.CompareSize,
		CompareMD5:      input.CompareMD5,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleRenameHistory(w http.ResponseWriter, r *http.Request) {
	items, err := renamer.ListHistory(renamer.HistoryPath(s.snapshotConfig()), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRenameHistoryUndoCheck(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/rename/history/")
	id = strings.TrimSuffix(id, "/undo-check")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, errors.New("history id is required"))
		return
	}
	result, err := renamer.CheckHistoryBatchUndo(renamer.HistoryPath(s.snapshotConfig()), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRenameHistoryUndo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/rename/history/")
	id = strings.TrimSuffix(id, "/undo")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, errors.New("history id is required"))
		return
	}
	batch, err := renamer.UndoHistoryBatch(renamer.HistoryPath(s.snapshotConfig()), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, batch)
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
	if err := s.reloadWatchDirs(r.Context()); err != nil {
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
	if err := s.reloadWatchDirs(r.Context()); err != nil {
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
	if err := s.reloadWatchDirs(r.Context()); err != nil {
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
		if _, err := os.Stat(input.Path); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
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
	for _, dir := range dirs {
		if _, err := os.Stat(dir.Path); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	go func() {
		for _, dir := range dirs {
			cfgDir := config.WatchDir{Path: dir.Path, Recursive: dir.Recursive, Enabled: true, WatchEnabled: dir.WatchEnabled, ScanOnStart: true}
			if err := bootstrap.ScanWatchDir(context.Background(), cfg, s.store, s.logger, cfgDir, options); err != nil {
				s.logger.Warn("manual rescan failed", "path", dir.Path, "error", err)
			}
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "count": len(dirs)})
}

func scanOptionsFromStrategy(strategy string) bootstrap.ScanOptions {
	return bootstrap.ScanOptionsFromStrategy(strategy)
}

func (s *Server) reloadWatchDirs(ctx context.Context) error {
	if s.watcher == nil {
		return nil
	}
	return s.watcher.ReloadWatchDirs(ctx)
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
