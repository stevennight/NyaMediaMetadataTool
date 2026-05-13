package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrWatchDirNotFound = errors.New("watch dir not found")

type WatchDir struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Enabled   bool   `json:"enabled"`
}

func (s *Store) ListWatchDirs(ctx context.Context) ([]WatchDir, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, path, recursive, enabled
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
		if err := rows.Scan(&dir.ID, &dir.Path, &recursive, &enabled); err != nil {
			return nil, err
		}
		dir.Recursive = recursive == 1
		dir.Enabled = enabled == 1
		dirs = append(dirs, dir)
	}
	return dirs, rows.Err()
}

func (s *Store) CreateWatchDir(ctx context.Context, dir WatchDir) (WatchDir, error) {
	recursive := boolToInt(dir.Recursive)
	enabled := boolToInt(dir.Enabled)
	result, err := s.db.ExecContext(ctx, `
INSERT INTO watch_dirs (path, recursive, enabled, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
`, dir.Path, recursive, enabled)
	if err != nil {
		return WatchDir{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return WatchDir{}, err
	}
	dir.ID = id
	return dir, nil
}

func (s *Store) UpdateWatchDir(ctx context.Context, dir WatchDir) (WatchDir, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE watch_dirs
SET path = ?, recursive = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, dir.Path, boolToInt(dir.Recursive), boolToInt(dir.Enabled), dir.ID)
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
	return dir, nil
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
	err := s.db.QueryRowContext(ctx, `
SELECT id, path, recursive, enabled
FROM watch_dirs
WHERE id = ?
`, id).Scan(&dir.ID, &dir.Path, &recursive, &enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return WatchDir{}, ErrWatchDirNotFound
		}
		return WatchDir{}, err
	}
	dir.Recursive = recursive == 1
	dir.Enabled = enabled == 1
	return dir, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
