package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrEmbyAPIKeyNotFound = errors.New("emby api key not found")

type EmbyAPIKey struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	APIKey    string `json:"apiKey,omitempty"`
	Note      string `json:"note"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func (s *Store) ListEmbyAPIKeys(ctx context.Context) ([]EmbyAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, note, created_at, updated_at
FROM emby_api_keys
ORDER BY title
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []EmbyAPIKey
	for rows.Next() {
		var key EmbyAPIKey
		if err := rows.Scan(&key.ID, &key.Title, &key.Note, &key.CreatedAt, &key.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) GetEmbyAPIKey(ctx context.Context, id int64) (EmbyAPIKey, error) {
	var key EmbyAPIKey
	err := s.db.QueryRowContext(ctx, `
SELECT id, title, api_key, note, created_at, updated_at
FROM emby_api_keys
WHERE id = ?
`, id).Scan(&key.ID, &key.Title, &key.APIKey, &key.Note, &key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EmbyAPIKey{}, ErrEmbyAPIKeyNotFound
		}
		return EmbyAPIKey{}, err
	}
	return key, nil
}

func (s *Store) SaveEmbyAPIKey(ctx context.Context, key EmbyAPIKey) (EmbyAPIKey, error) {
	key.Title = strings.TrimSpace(key.Title)
	key.APIKey = strings.TrimSpace(key.APIKey)
	key.Note = strings.TrimSpace(key.Note)
	result, err := s.db.ExecContext(ctx, `
INSERT INTO emby_api_keys (title, api_key, note, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(title) DO UPDATE SET
  api_key = excluded.api_key,
  note = excluded.note,
  updated_at = CURRENT_TIMESTAMP
`, key.Title, key.APIKey, key.Note)
	if err != nil {
		return EmbyAPIKey{}, err
	}
	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		return s.getEmbyAPIKeyByTitle(ctx, key.Title)
	}
	return s.GetEmbyAPIKey(ctx, id)
}

func (s *Store) DeleteEmbyAPIKey(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM emby_api_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrEmbyAPIKeyNotFound
	}
	return nil
}

func (s *Store) getEmbyAPIKeyByTitle(ctx context.Context, title string) (EmbyAPIKey, error) {
	var key EmbyAPIKey
	err := s.db.QueryRowContext(ctx, `
SELECT id, title, api_key, note, created_at, updated_at
FROM emby_api_keys
WHERE title = ?
`, title).Scan(&key.ID, &key.Title, &key.APIKey, &key.Note, &key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		return EmbyAPIKey{}, err
	}
	return key, nil
}
