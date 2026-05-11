package store

import (
	"context"

	"NyaMediaMetadataTool/internal/tools"
)

func (s *Store) SaveToolStatuses(ctx context.Context, statuses []tools.Status) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO tool_status (name, path, available, version, checked_at, error)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  path = excluded.path,
  available = excluded.available,
  version = excluded.version,
  checked_at = excluded.checked_at,
  error = excluded.error
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, status := range statuses {
		available := 0
		if status.Available {
			available = 1
		}
		if _, err := stmt.ExecContext(ctx, status.Name, status.Path, available, status.Version, status.CheckedAt, status.Error); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListToolStatuses(ctx context.Context) ([]tools.Status, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, path, available, version, checked_at, error
FROM tool_status
ORDER BY name
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []tools.Status
	for rows.Next() {
		var status tools.Status
		var available int
		if err := rows.Scan(&status.Name, &status.Path, &available, &status.Version, &status.CheckedAt, &status.Error); err != nil {
			return nil, err
		}
		status.Available = available == 1
		statuses = append(statuses, status)
	}
	return statuses, rows.Err()
}
