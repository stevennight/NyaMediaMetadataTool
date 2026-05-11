package store

import (
	"context"
	"os"
	"time"
)

func (s *Store) UpsertMediaFile(ctx context.Context, path string, info os.FileInfo) (int64, error) {
	modifiedAt := info.ModTime().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx, `
INSERT INTO media_files (path, size, modified_at, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(path) DO UPDATE SET
  size = excluded.size,
  modified_at = excluded.modified_at,
  updated_at = CURRENT_TIMESTAMP
`, path, info.Size(), modifiedAt)
	if err != nil {
		return 0, err
	}

	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM media_files WHERE path = ?`, path).Scan(&id)
	return id, err
}

func (s *Store) EnqueueMediaTask(ctx context.Context, mediaFileID int64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (media_file_id, type, status)
SELECT ?, 'media_process', 'pending'
WHERE EXISTS (
  SELECT 1
  FROM media_files
  WHERE id = ?
    AND (last_processed_at IS NULL OR modified_at > last_processed_at)
)
AND NOT EXISTS (
  SELECT 1
  FROM tasks
  WHERE media_file_id = ? AND type = 'media_process' AND status IN ('pending', 'running')
)
`, mediaFileID, mediaFileID, mediaFileID)
	return err
}
