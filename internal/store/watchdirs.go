package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"NyaMediaMetadataTool/internal/config"
)

var (
	ErrWatchDirNotFound = errors.New("watch dir not found")
	ErrWatchDirExists   = errors.New("watch dir already exists")
)

type WatchDir struct {
	ID                  int64                         `json:"id"`
	Path                string                        `json:"path"`
	Recursive           bool                          `json:"recursive"`
	Enabled             bool                          `json:"enabled"`
	WatchEnabled        bool                          `json:"watchEnabled"`
	ScanOnStart         bool                          `json:"scanOnStart"`
	UseGlobalProcessing bool                          `json:"useGlobalProcessing"`
	Processing          config.OutputProcessingConfig `json:"processing"`
	UploadConfigs       []UploadProviderRoute         `json:"uploadConfigs"`
}

func (s *Store) ListWatchDirs(ctx context.Context) ([]WatchDir, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, path, recursive, enabled, watch_enabled, scan_on_start, use_global_processing, processing_config
FROM watch_dirs
ORDER BY path
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dirs := make([]WatchDir, 0)
	for rows.Next() {
		var dir WatchDir
		var recursive int
		var enabled int
		var watchEnabled int
		var scanOnStart int
		var useGlobalProcessing int
		var processingJSON string
		if err := rows.Scan(&dir.ID, &dir.Path, &recursive, &enabled, &watchEnabled, &scanOnStart, &useGlobalProcessing, &processingJSON); err != nil {
			return nil, err
		}
		dir.Recursive = recursive == 1
		dir.Enabled = enabled == 1
		dir.WatchEnabled = watchEnabled == 1
		dir.ScanOnStart = scanOnStart == 1
		dir.UseGlobalProcessing = useGlobalProcessing == 1
		if err := decodeWatchDirProcessing(processingJSON, &dir.Processing); err != nil {
			return nil, err
		}
		dirs = append(dirs, dir)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range dirs {
		dirs[index].UploadConfigs, err = s.ListWatchDirUploadConfigs(ctx, dirs[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return dirs, nil
}

func (s *Store) CreateWatchDir(ctx context.Context, dir WatchDir) (WatchDir, error) {
	dir.ScanOnStart = false
	dir.Enabled = dir.WatchEnabled
	processingJSON, err := encodeWatchDirProcessing(dir.Processing)
	if err != nil {
		return WatchDir{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WatchDir{}, err
	}
	defer tx.Rollback()
	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM watch_dirs WHERE path = ?`, dir.Path).Scan(&existingID)
	if err == nil {
		return WatchDir{}, ErrWatchDirExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return WatchDir{}, err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO watch_dirs (path, recursive, enabled, watch_enabled, scan_on_start, use_global_processing, processing_config, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`, dir.Path, boolToInt(dir.Recursive), boolToInt(dir.Enabled), boolToInt(dir.WatchEnabled), boolToInt(dir.ScanOnStart), boolToInt(dir.UseGlobalProcessing), processingJSON)
	if err != nil {
		return WatchDir{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return WatchDir{}, err
	}
	dir.ID = id
	if err := replaceWatchDirUploadConfigsTx(ctx, tx, dir.ID, dir.UploadConfigs); err != nil {
		return WatchDir{}, err
	}
	if err := tx.Commit(); err != nil {
		return WatchDir{}, err
	}
	return s.GetWatchDir(ctx, dir.ID)
}

func (s *Store) UpdateWatchDir(ctx context.Context, dir WatchDir) (WatchDir, error) {
	dir.ScanOnStart = false
	dir.Enabled = dir.WatchEnabled
	processingJSON, err := encodeWatchDirProcessing(dir.Processing)
	if err != nil {
		return WatchDir{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WatchDir{}, err
	}
	defer tx.Rollback()
	var existingID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM watch_dirs WHERE path = ? AND id != ?`, dir.Path, dir.ID).Scan(&existingID)
	if err == nil {
		return WatchDir{}, ErrWatchDirExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return WatchDir{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE watch_dirs
SET path = ?, recursive = ?, enabled = ?, watch_enabled = ?, scan_on_start = ?, use_global_processing = ?, processing_config = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, dir.Path, boolToInt(dir.Recursive), boolToInt(dir.Enabled), boolToInt(dir.WatchEnabled), boolToInt(dir.ScanOnStart), boolToInt(dir.UseGlobalProcessing), processingJSON, dir.ID)
	if err != nil {
		return WatchDir{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return WatchDir{}, err
	}
	if affected == 0 {
		return WatchDir{}, ErrWatchDirNotFound
	}
	if dir.UploadConfigs != nil {
		if err := replaceWatchDirUploadConfigsTx(ctx, tx, dir.ID, dir.UploadConfigs); err != nil {
			return WatchDir{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return WatchDir{}, err
	}
	return s.GetWatchDir(ctx, dir.ID)
}

func (s *Store) DisableWatchDirScanOnStart(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE watch_dirs
SET scan_on_start = 0, enabled = watch_enabled, updated_at = CURRENT_TIMESTAMP
WHERE scan_on_start != 0 OR enabled != watch_enabled
`)
	return err
}

func (s *Store) DeleteWatchDir(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM watch_dirs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrWatchDirNotFound
	}
	return nil
}

func (s *Store) GetWatchDir(ctx context.Context, id int64) (WatchDir, error) {
	var dir WatchDir
	var recursive int
	var enabled int
	var watchEnabled int
	var scanOnStart int
	var useGlobalProcessing int
	var processingJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT id, path, recursive, enabled, watch_enabled, scan_on_start, use_global_processing, processing_config
FROM watch_dirs
WHERE id = ?
`, id).Scan(&dir.ID, &dir.Path, &recursive, &enabled, &watchEnabled, &scanOnStart, &useGlobalProcessing, &processingJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return WatchDir{}, ErrWatchDirNotFound
		}
		return WatchDir{}, err
	}
	dir.Recursive = recursive == 1
	dir.Enabled = enabled == 1
	dir.WatchEnabled = watchEnabled == 1
	dir.ScanOnStart = scanOnStart == 1
	dir.UseGlobalProcessing = useGlobalProcessing == 1
	if err := decodeWatchDirProcessing(processingJSON, &dir.Processing); err != nil {
		return WatchDir{}, err
	}
	dir.UploadConfigs, err = s.ListWatchDirUploadConfigs(ctx, dir.ID)
	if err != nil {
		return WatchDir{}, err
	}
	return dir, nil
}

func (s *Store) FindWatchDirForPath(ctx context.Context, path string) (WatchDir, error) {
	dirs, err := s.ListWatchDirs(ctx)
	if err != nil {
		return WatchDir{}, err
	}
	cleanPath := filepath.Clean(path)
	var matched WatchDir
	for _, dir := range dirs {
		cleanRoot := filepath.Clean(dir.Path)
		if !watchDirContainsPath(cleanRoot, cleanPath) {
			continue
		}
		if matched.Path == "" || len(cleanRoot) > len(filepath.Clean(matched.Path)) {
			matched = dir
		}
	}
	if matched.Path == "" {
		return WatchDir{}, ErrWatchDirNotFound
	}
	return matched, nil
}

func watchDirContainsPath(root string, path string) bool {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		path = strings.ToLower(path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

func encodeWatchDirProcessing(processing config.OutputProcessingConfig) (string, error) {
	data, err := json.Marshal(processing)
	return string(data), err
}

func decodeWatchDirProcessing(value string, processing *config.OutputProcessingConfig) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return json.Unmarshal([]byte(value), processing)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
