package store

import "context"

type Artifact struct {
	ID          int64  `json:"id"`
	MediaFileID *int64 `json:"mediaFileId"`
	TaskID      *int64 `json:"taskId"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	CreatedAt   string `json:"createdAt"`
}

func (s *Store) ListArtifacts(ctx context.Context, limit int) ([]Artifact, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, media_file_id, task_id, type, path, source, created_at
FROM artifacts
ORDER BY id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Artifact, 0)
	for rows.Next() {
		var item Artifact
		var mediaFileID *int64
		var taskID *int64
		if err := rows.Scan(&item.ID, &mediaFileID, &taskID, &item.Type, &item.Path, &item.Source, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.MediaFileID = mediaFileID
		item.TaskID = taskID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListArtifactsByTask(ctx context.Context, taskID int64) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, media_file_id, task_id, type, path, source, created_at
FROM artifacts
WHERE task_id = ?
ORDER BY id ASC
`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Artifact, 0)
	for rows.Next() {
		var item Artifact
		var mediaFileID *int64
		var taskIDValue *int64
		if err := rows.Scan(&item.ID, &mediaFileID, &taskIDValue, &item.Type, &item.Path, &item.Source, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.MediaFileID = mediaFileID
		item.TaskID = taskIDValue
		items = append(items, item)
	}
	return items, rows.Err()
}
