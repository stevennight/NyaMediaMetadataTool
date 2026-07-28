package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Store) GetUploadProviderCache(ctx context.Context, providerID int64, key string) (string, bool, error) {
	key = strings.TrimSpace(key)
	if providerID <= 0 || key == "" {
		return "", false, nil
	}
	var value string
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.db.QueryRowContext(ctx, `
SELECT cache_value
FROM upload_provider_cache
WHERE provider_id = ? AND cache_key = ?
  AND (expires_at IS NULL OR expires_at > ?)
`, providerID, key, now).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get upload provider cache %d/%s: %w", providerID, key, err)
	}
	return value, true, nil
}

func (s *Store) SetUploadProviderCache(ctx context.Context, providerID int64, key, value string) error {
	return s.SetUploadProviderCacheWithTTL(ctx, providerID, key, value, 0)
}

func (s *Store) SetUploadProviderCacheWithTTL(ctx context.Context, providerID int64, key, value string, ttl time.Duration) error {
	key = strings.TrimSpace(key)
	if providerID <= 0 || key == "" {
		return fmt.Errorf("provider id and cache key are required")
	}
	var expiresAt any
	if ttl > 0 {
		expiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO upload_provider_cache (provider_id, cache_key, cache_value, expires_at, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider_id, cache_key) DO UPDATE SET
  cache_value = excluded.cache_value,
  expires_at = excluded.expires_at,
  updated_at = CURRENT_TIMESTAMP
`, providerID, key, value, expiresAt)
	if err != nil {
		return fmt.Errorf("set upload provider cache %d/%s: %w", providerID, key, err)
	}
	return nil
}

func deleteUploadProviderCacheTx(ctx context.Context, tx *sql.Tx, providerID int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM upload_provider_cache WHERE provider_id = ?`, providerID)
	return err
}

func (s *Store) DeleteUploadProviderCacheKey(ctx context.Context, providerID int64, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_provider_cache WHERE provider_id = ? AND cache_key = ?`, providerID, strings.TrimSpace(key))
	return err
}

func (s *Store) DeleteUploadProviderCache(ctx context.Context, providerID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_provider_cache WHERE provider_id = ?`, providerID)
	return err
}

func (s *Store) DeleteExpiredUploadProviderCache(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM upload_provider_cache WHERE expires_at IS NOT NULL AND expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
