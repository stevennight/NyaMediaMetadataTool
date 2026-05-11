package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNoPendingTask = errors.New("no pending task")

type MediaFile struct {
	ID         int64
	Path       string
	Size       int64
	ModifiedAt time.Time
}

func (s *Store) ClaimNextPendingTask(ctx context.Context) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	var task Task
	var mediaFileID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT id, media_file_id, type, status, attempts, error_summary,
       COALESCE(started_at, ''), COALESCE(finished_at, ''), created_at, updated_at
FROM tasks
WHERE status = 'pending'
ORDER BY id ASC
LIMIT 1
`).Scan(
		&task.ID,
		&mediaFileID,
		&task.Type,
		&task.Status,
		&task.Attempts,
		&task.ErrorSummary,
		&task.StartedAt,
		&task.FinishedAt,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Task{}, ErrNoPendingTask
		}
		return Task{}, err
	}
	if mediaFileID.Valid {
		task.MediaFileID = &mediaFileID.Int64
	}

	_, err = tx.ExecContext(ctx, `
UPDATE tasks
SET status = 'running', attempts = attempts + 1, started_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, task.ID)
	if err != nil {
		return Task{}, err
	}

	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	task.Status = "running"
	task.Attempts++
	return task, nil
}

func (s *Store) ResetRunningTasks(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET status = 'pending', started_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE status = 'running'
`)
	return err
}

func (s *Store) CompleteTask(ctx context.Context, taskID int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET status = 'completed', error_summary = '', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, taskID)
	return err
}

func (s *Store) FailTask(ctx context.Context, taskID int64, summary string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET status = 'failed', error_summary = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, summary, taskID)
	return err
}

func (s *Store) GetMediaFileByID(ctx context.Context, id int64) (MediaFile, error) {
	var media MediaFile
	var modifiedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT id, path, size, modified_at
FROM media_files
WHERE id = ?
`, id).Scan(&media.ID, &media.Path, &media.Size, &modifiedAt)
	if err != nil {
		return MediaFile{}, err
	}
	media.ModifiedAt, err = parseStoreTime(modifiedAt)
	if err != nil {
		return MediaFile{}, err
	}
	return media, nil
}

func parseStoreTime(value string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func (s *Store) SaveArtifact(ctx context.Context, mediaFileID int64, taskID int64, artifactType string, path string, source string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (media_file_id, task_id, type, path, source)
VALUES (?, ?, ?, ?, ?)
`, mediaFileID, taskID, artifactType, path, source)
	return err
}

func (s *Store) TouchMediaProcessed(ctx context.Context, mediaFileID int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE media_files
SET last_processed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, mediaFileID)
	return err
}
