package store

import "context"

const (
	ScanRunStatusCollecting = "collecting"
	ScanRunStatusRunning    = "running"
	ScanRunStatusCompleted  = "completed"
	ScanRunStatusFailed     = "failed"
	ScanRunStatusCanceled   = "canceled"
	ScanRunStatusEmpty      = "empty"
)

type ScanRunSummary struct {
	ID             string `json:"id"`
	Source         string `json:"source"`
	ScopePath      string `json:"scopePath"`
	Status         string `json:"status"`
	Total          int    `json:"total"`
	Active         int    `json:"active"`
	Completed      int    `json:"completed"`
	Failed         int    `json:"failed"`
	Canceled       int    `json:"canceled"`
	Ignored        int    `json:"ignored"`
	ErrorSummary   string `json:"errorSummary"`
	CreatedAt      string `json:"createdAt"`
	ScanFinishedAt string `json:"scanFinishedAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func (s *Store) BeginScanRun(ctx context.Context, id string, source string, scopePath string) error {
	result, err := s.db.ExecContext(ctx, `
INSERT INTO scan_runs (id, source, scope_path)
VALUES (?, ?, ?)
`, id, source, scopePath)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if rows > 0 {
		s.notifyTaskChanges()
	}
	return nil
}

func (s *Store) FinishScanRun(ctx context.Context, id string, errorSummary string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE scan_runs
SET error_summary = ?, sealed_at = COALESCE(sealed_at, CURRENT_TIMESTAMP)
WHERE id = ?
`, errorSummary, id)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if rows > 0 {
		s.notifyTaskChanges()
	}
	return nil
}

func (s *Store) ListScanRunSummaries(ctx context.Context, limit int) ([]ScanRunSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id,
       r.source,
       r.scope_path,
       COUNT(t.id),
       COALESCE(SUM(CASE WHEN t.status IN ('pending', 'running') THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN t.status = 'completed' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN t.status = 'failed' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN t.status = 'canceled' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN t.status = 'ignored' THEN 1 ELSE 0 END), 0),
       r.error_summary,
       r.created_at,
       COALESCE(r.sealed_at, ''),
       MAX(COALESCE(MAX(t.updated_at), ''), COALESCE(r.sealed_at, ''), r.created_at)
FROM (
  SELECT sr.id, sr.source, sr.scope_path, sr.error_summary, sr.sealed_at, sr.created_at
  FROM scan_runs sr
  ORDER BY MAX(
             COALESCE((SELECT MAX(recent.updated_at) FROM tasks recent WHERE recent.scan_run_id = sr.id), ''),
             COALESCE(sr.sealed_at, ''),
             sr.created_at
           ) DESC,
           sr.id DESC
  LIMIT ?
) r
LEFT JOIN tasks t ON t.scan_run_id = r.id
GROUP BY r.id, r.source, r.scope_path, r.error_summary, r.created_at, r.sealed_at
ORDER BY MAX(COALESCE(MAX(t.updated_at), ''), COALESCE(r.sealed_at, ''), r.created_at) DESC, r.id DESC
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]ScanRunSummary, 0)
	for rows.Next() {
		var run ScanRunSummary
		if err := rows.Scan(
			&run.ID,
			&run.Source,
			&run.ScopePath,
			&run.Total,
			&run.Active,
			&run.Completed,
			&run.Failed,
			&run.Canceled,
			&run.Ignored,
			&run.ErrorSummary,
			&run.CreatedAt,
			&run.ScanFinishedAt,
			&run.UpdatedAt,
		); err != nil {
			return nil, err
		}
		run.Status = scanRunStatus(run)
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func scanRunStatus(run ScanRunSummary) string {
	if run.ScanFinishedAt == "" {
		return ScanRunStatusCollecting
	}
	if run.Active > 0 {
		return ScanRunStatusRunning
	}
	if run.Failed > 0 || run.ErrorSummary != "" {
		return ScanRunStatusFailed
	}
	if run.Total == 0 {
		return ScanRunStatusEmpty
	}
	if run.Canceled > 0 {
		return ScanRunStatusCanceled
	}
	return ScanRunStatusCompleted
}
