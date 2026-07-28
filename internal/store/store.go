package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"NyaMediaMetadataTool/internal/config"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type UploadRuntimeOptions struct {
	Concurrency int
	QuietPeriod time.Duration
	MaxAttempts int
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.configure(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := restrictDatabasePermissions(path); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func restrictDatabasePermissions(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restrict database permissions for %q: %w", candidate, err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}

	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("apply sqlite pragma %q: %w", pragma, err)
		}
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context, legacyUploads ...*config.LegacyUploadConfig) error {
	var legacyUpload *config.LegacyUploadConfig
	if len(legacyUploads) > 0 {
		legacyUpload = legacyUploads[0]
	}
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return err
	}
	if err := s.ensureTaskOverwriteColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureTaskScanRunColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureTaskScanRunIndex(ctx); err != nil {
		return err
	}
	if err := s.backfillScanRuns(ctx); err != nil {
		return err
	}
	if err := s.sealInterruptedScanRuns(ctx); err != nil {
		return err
	}
	if err := s.ensureTaskProcessingConfigColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureScanScopeTaskColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureEmbyAPIKeyNoteColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureWatchDirSplitColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureWatchDirProcessingColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureUploadColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureUploadRouteIndexes(ctx); err != nil {
		return err
	}
	if err := s.migrateUploadRoutesToWatchDirs(ctx); err != nil {
		return err
	}
	if err := s.migrateLegacyUploadSettings(ctx, legacyUpload); err != nil {
		return err
	}
	return s.cancelLegacyUploadWork(ctx)
}

func (s *Store) ensureTaskOverwriteColumn(ctx context.Context) error {
	return s.ensureTaskColumn(ctx, "overwrite_existing", `ALTER TABLE tasks ADD COLUMN overwrite_existing INTEGER NOT NULL DEFAULT 0`)
}

func (s *Store) ensureTaskScanRunColumn(ctx context.Context) error {
	return s.ensureTaskColumn(ctx, "scan_run_id", `ALTER TABLE tasks ADD COLUMN scan_run_id TEXT NOT NULL DEFAULT ''`)
}

func (s *Store) ensureTaskScanRunIndex(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_scan_run_status ON tasks(scan_run_id, status)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_tasks_scan_run_updated ON tasks(scan_run_id, updated_at DESC)`)
	return err
}

func (s *Store) backfillScanRuns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO scan_runs (id, source, scope_path, error_summary, sealed_at, created_at)
SELECT scan_run_id, 'legacy', '', '', MAX(updated_at), MIN(created_at)
FROM tasks
WHERE scan_run_id != ''
GROUP BY scan_run_id
`)
	return err
}

func (s *Store) sealInterruptedScanRuns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE scan_runs
SET error_summary = CASE
      WHEN error_summary = '' THEN '扫描因应用重启而中断'
      ELSE error_summary
    END,
    sealed_at = CURRENT_TIMESTAMP
WHERE sealed_at IS NULL
`)
	return err
}

func (s *Store) ensureTaskProcessingConfigColumn(ctx context.Context) error {
	return s.ensureTaskColumn(ctx, "processing_config", `ALTER TABLE tasks ADD COLUMN processing_config TEXT NOT NULL DEFAULT ''`)
}

func (s *Store) ensureScanScopeTaskColumn(ctx context.Context) error {
	return s.ensureColumn(ctx, "scan_scopes", "task_id", `ALTER TABLE scan_scopes ADD COLUMN task_id INTEGER NOT NULL DEFAULT 0`)
}

func (s *Store) ensureEmbyAPIKeyNoteColumn(ctx context.Context) error {
	return s.ensureColumn(ctx, "emby_api_keys", "note", `ALTER TABLE emby_api_keys ADD COLUMN note TEXT NOT NULL DEFAULT ''`)
}

func (s *Store) ensureWatchDirSplitColumns(ctx context.Context) error {
	hasWatchEnabled, err := s.hasColumn(ctx, "watch_dirs", "watch_enabled")
	if err != nil {
		return err
	}
	if !hasWatchEnabled {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE watch_dirs ADD COLUMN watch_enabled INTEGER NOT NULL DEFAULT 1`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE watch_dirs SET watch_enabled = enabled WHERE enabled IN (0, 1)`); err != nil {
			return err
		}
	}

	hasScanOnStart, err := s.hasColumn(ctx, "watch_dirs", "scan_on_start")
	if err != nil {
		return err
	}
	if !hasScanOnStart {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE watch_dirs ADD COLUMN scan_on_start INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureWatchDirProcessingColumns(ctx context.Context) error {
	if err := s.ensureColumn(ctx, "watch_dirs", "use_global_processing", `ALTER TABLE watch_dirs ADD COLUMN use_global_processing INTEGER NOT NULL DEFAULT 1`); err != nil {
		return err
	}
	return s.ensureColumn(ctx, "watch_dirs", "processing_config", `ALTER TABLE watch_dirs ADD COLUMN processing_config TEXT NOT NULL DEFAULT ''`)
}

func (s *Store) ensureUploadColumns(ctx context.Context) error {
	columns := []struct {
		table  string
		name   string
		alter  string
		update string
	}{
		{"upload_providers", "auth_device", `ALTER TABLE upload_providers ADD COLUMN auth_device TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_providers", "request_interval_ms", `ALTER TABLE upload_providers ADD COLUMN request_interval_ms INTEGER NOT NULL DEFAULT 500`, ""},
		{"upload_notification_templates", "headers_template", `ALTER TABLE upload_notification_templates ADD COLUMN headers_template TEXT NOT NULL DEFAULT '{}'`, ""},
		{"upload_provider_routes", "notification_template_id", `ALTER TABLE upload_provider_routes ADD COLUMN notification_template_id INTEGER`, ""},
		{"upload_provider_routes", "notification_variables", `ALTER TABLE upload_provider_routes ADD COLUMN notification_variables TEXT NOT NULL DEFAULT '{}'`, ""},
		{"upload_batches", "upload_route_id", `ALTER TABLE upload_batches ADD COLUMN upload_route_id INTEGER`, ""},
		{"upload_batch_files", "sha1", `ALTER TABLE upload_batch_files ADD COLUMN sha1 TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_batch_targets", "include_types", `ALTER TABLE upload_batch_targets ADD COLUMN include_types TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_batch_targets", "retryable", `ALTER TABLE upload_batch_targets ADD COLUMN retryable INTEGER NOT NULL DEFAULT 1`, ""},
		{"upload_batch_targets", "notification_template_id", `ALTER TABLE upload_batch_targets ADD COLUMN notification_template_id INTEGER`, ""},
		{"upload_batch_targets", "notification_variables", `ALTER TABLE upload_batch_targets ADD COLUMN notification_variables TEXT NOT NULL DEFAULT '{}'`, ""},
		{"upload_batch_targets", "request_interval_ms", `ALTER TABLE upload_batch_targets ADD COLUMN request_interval_ms INTEGER NOT NULL DEFAULT 500`, ""},
		{"upload_transfers", "outcome", `ALTER TABLE upload_transfers ADD COLUMN outcome TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_transfers", "remote_sha1", `ALTER TABLE upload_transfers ADD COLUMN remote_sha1 TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_events", "attempts", `ALTER TABLE upload_events ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`, ""},
		{"upload_events", "available_at", `ALTER TABLE upload_events ADD COLUMN available_at TEXT NOT NULL DEFAULT ''`, `UPDATE upload_events SET available_at = created_at WHERE available_at = ''`},
		{"upload_events", "lease_id", `ALTER TABLE upload_events ADD COLUMN lease_id TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_events", "lease_until", `ALTER TABLE upload_events ADD COLUMN lease_until TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_events", "error_summary", `ALTER TABLE upload_events ADD COLUMN error_summary TEXT NOT NULL DEFAULT ''`, ""},
		{"upload_events", "updated_at", `ALTER TABLE upload_events ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`, `UPDATE upload_events SET updated_at = created_at WHERE updated_at = ''`},
		{"upload_notifications", "headers", `ALTER TABLE upload_notifications ADD COLUMN headers TEXT NOT NULL DEFAULT '{}'`, ""},
	}
	for _, column := range columns {
		exists, err := s.hasColumn(ctx, column.table, column.name)
		if err != nil {
			return err
		}
		if exists {
			if column.update != "" {
				if _, err := s.db.ExecContext(ctx, column.update); err != nil {
					return err
				}
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.alter); err != nil {
			return err
		}
		if column.update != "" {
			if _, err := s.db.ExecContext(ctx, column.update); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) ensureUploadRouteIndexes(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_upload_provider_routes_watch`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_upload_provider_routes_watch
ON upload_provider_routes(provider_id, watch_dir_id)
`)
	return err
}

func (s *Store) migrateUploadRoutesToWatchDirs(ctx context.Context) error {
	const migration = "upload-routes-to-watch-dirs-v1"
	var applied int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, migration).Scan(&applied)
	if err != nil || applied > 0 {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Materialize the legacy provider-wide fallback for every directory that
	// does not already have a scoped override. Providers without routes used
	// their provider defaults globally, so they are expanded in the same way.
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO upload_provider_routes
  (provider_id, watch_dir_id, enabled, remote_root, collision_policy, include_types, updated_at)
SELECT p.id, wd.id, 1,
       COALESCE(global_route.remote_root, p.remote_root),
       COALESCE(global_route.collision_policy, p.collision_policy),
       COALESCE(global_route.include_types, ''),
       CURRENT_TIMESTAMP
FROM upload_providers p
CROSS JOIN watch_dirs wd
LEFT JOIN upload_provider_routes scoped
  ON scoped.provider_id = p.id AND scoped.watch_dir_id = wd.id
LEFT JOIN upload_provider_routes global_route
  ON global_route.provider_id = p.id AND global_route.watch_dir_id IS NULL
WHERE scoped.id IS NULL
  AND (
    (global_route.id IS NOT NULL AND global_route.enabled = 1)
    OR (
      global_route.id IS NULL
      AND NOT EXISTS (
        SELECT 1 FROM upload_provider_routes any_route WHERE any_route.provider_id = p.id
      )
    )
  )
`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM upload_provider_routes WHERE watch_dir_id IS NULL`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_upload_provider_routes_global`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, migration); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateLegacyUploadSettings(ctx context.Context, legacy *config.LegacyUploadConfig) error {
	const migration = "legacy-upload-settings-to-sqlite-v1"
	var applied int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, migration).Scan(&applied)
	if err != nil || applied > 0 {
		return err
	}

	includeTypes := append([]string{}, uploadFileTypes...)
	options := UploadRuntimeOptions{Concurrency: 1, QuietPeriod: 2 * time.Minute, MaxAttempts: 3}
	if legacy != nil {
		includeTypes = normalizeStoredUploadTypes(legacy.IncludeTypes)
		if len(includeTypes) == 0 {
			includeTypes = append([]string{}, uploadFileTypes...)
		}
		options = UploadRuntimeOptions{
			Concurrency: legacy.Concurrency,
			QuietPeriod: legacy.QuietPeriod,
			MaxAttempts: legacy.MaxAttempts,
		}
	}
	options = normalizeUploadRuntimeOptions(options)
	encodedTypes, err := json.Marshal(includeTypes)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if legacy != nil {
		if _, err := tx.ExecContext(ctx, `
UPDATE upload_runtime_options
SET concurrency = ?, quiet_period_ns = ?, max_attempts = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = 1
`, options.Concurrency, int64(options.QuietPeriod), options.MaxAttempts); err != nil {
			return err
		}
	}
	// Before directory-scoped upload existed, a missing upload block had the
	// same behavior as enabled=false. Preserve that safe default during upgrade.
	if legacy == nil || !legacy.Enabled {
		if _, err := tx.ExecContext(ctx, `UPDATE upload_provider_routes SET enabled = 0, updated_at = CURRENT_TIMESTAMP`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_provider_routes
SET include_types = ?, updated_at = CURRENT_TIMESTAMP
WHERE TRIM(include_types) IN ('', '[]', 'null')
`, string(encodedTypes)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, migration); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) cancelLegacyUploadWork(ctx context.Context) error {
	const migration = "cancel-legacy-upload-work-v1"
	var applied int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, migration).Scan(&applied)
	if err != nil || applied > 0 {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET retryable = 0, updated_at = CURRENT_TIMESTAMP
WHERE status <> ?
`, UploadTargetCompleted); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?, error_summary = CASE WHEN error_summary = '' THEN 'canceled during directory upload migration' ELSE error_summary END,
    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
WHERE status IN (?, ?)
`, UploadTransferCanceled, UploadTransferPending, UploadTransferRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, error_summary = CASE WHEN error_summary = '' THEN 'canceled during directory upload migration' ELSE error_summary END,
    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
WHERE status IN (?, ?, ?)
`, UploadTargetCanceled, UploadTargetWaiting, UploadTargetPending, UploadTargetRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batches
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE status IN (?, ?, ?)
`, UploadBatchCanceled, UploadBatchCollecting, UploadBatchPending, UploadBatchRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, migration); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUploadRuntimeOptions(ctx context.Context) (UploadRuntimeOptions, error) {
	var options UploadRuntimeOptions
	var quietPeriodNS int64
	err := s.db.QueryRowContext(ctx, `
SELECT concurrency, quiet_period_ns, max_attempts
FROM upload_runtime_options
WHERE id = 1
`).Scan(&options.Concurrency, &quietPeriodNS, &options.MaxAttempts)
	if err != nil {
		return UploadRuntimeOptions{}, err
	}
	options.QuietPeriod = time.Duration(quietPeriodNS)
	return normalizeUploadRuntimeOptions(options), nil
}

func normalizeUploadRuntimeOptions(options UploadRuntimeOptions) UploadRuntimeOptions {
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}
	if options.QuietPeriod <= 0 {
		options.QuietPeriod = 2 * time.Minute
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	return options
}

func (s *Store) ensureTaskColumn(ctx context.Context, column string, statement string) error {
	return s.ensureColumn(ctx, "tasks", column, statement)
}

func (s *Store) ensureColumn(ctx context.Context, table string, column string, statement string) error {
	exists, err := s.hasColumn(ctx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.db.ExecContext(ctx, statement)
	return err
}

func (s *Store) hasColumn(ctx context.Context, table string, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  name TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS upload_runtime_options (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  concurrency INTEGER NOT NULL,
  quiet_period_ns INTEGER NOT NULL,
  max_attempts INTEGER NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO upload_runtime_options (id, concurrency, quiet_period_ns, max_attempts)
VALUES (1, 1, 120000000000, 3);

CREATE TABLE IF NOT EXISTS watch_dirs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  recursive INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  watch_enabled INTEGER NOT NULL DEFAULT 1,
  scan_on_start INTEGER NOT NULL DEFAULT 0,
  use_global_processing INTEGER NOT NULL DEFAULT 1,
  processing_config TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS media_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  size INTEGER NOT NULL DEFAULT 0,
  modified_at TEXT NOT NULL,
  fingerprint TEXT NOT NULL DEFAULT '',
  last_processed_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  media_file_id INTEGER,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  overwrite_existing INTEGER NOT NULL DEFAULT 0,
  scan_run_id TEXT NOT NULL DEFAULT '',
  processing_config TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(media_file_id) REFERENCES media_files(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_media_file_id ON tasks(media_file_id);

CREATE TABLE IF NOT EXISTS scan_runs (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL DEFAULT 'scan',
  scope_path TEXT NOT NULL DEFAULT '',
  error_summary TEXT NOT NULL DEFAULT '',
  sealed_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS scan_scopes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scan_run_id TEXT NOT NULL,
  scope_type TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  task_id INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(scan_run_id, scope_type, scope_key)
);

CREATE TABLE IF NOT EXISTS task_stage_successes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  stage TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(task_id, stage),
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  level TEXT NOT NULL,
  message TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  media_file_id INTEGER,
  task_id INTEGER,
  type TEXT NOT NULL,
  path TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'generated',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(media_file_id) REFERENCES media_files(id) ON DELETE SET NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_artifacts_media_file_id ON artifacts(media_file_id);

CREATE TABLE IF NOT EXISTS upload_providers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  remote_root TEXT NOT NULL DEFAULT '/',
  user_agent TEXT NOT NULL DEFAULT '',
  collision_policy TEXT NOT NULL DEFAULT 'replace',
  auth_device TEXT NOT NULL DEFAULT '',
  request_interval_ms INTEGER NOT NULL DEFAULT 500,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_upload_providers_name ON upload_providers(name);

CREATE TABLE IF NOT EXISTS upload_provider_secrets (
  provider_id INTEGER NOT NULL,
  secret_key TEXT NOT NULL,
  secret_value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(provider_id, secret_key),
  FOREIGN KEY(provider_id) REFERENCES upload_providers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS upload_notification_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  headers_template TEXT NOT NULL DEFAULT '{}',
  payload_template TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS upload_provider_routes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider_id INTEGER NOT NULL,
  watch_dir_id INTEGER NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  remote_root TEXT NOT NULL DEFAULT '/',
  collision_policy TEXT NOT NULL DEFAULT 'fail',
  include_types TEXT NOT NULL DEFAULT '',
  notification_template_id INTEGER,
  notification_variables TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(provider_id) REFERENCES upload_providers(id) ON DELETE CASCADE,
  FOREIGN KEY(watch_dir_id) REFERENCES watch_dirs(id) ON DELETE CASCADE,
  FOREIGN KEY(notification_template_id) REFERENCES upload_notification_templates(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_upload_provider_routes_watch
  ON upload_provider_routes(provider_id, watch_dir_id)
  WHERE watch_dir_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_upload_provider_routes_watch_enabled
  ON upload_provider_routes(watch_dir_id, enabled);

CREATE TRIGGER IF NOT EXISTS trg_upload_provider_routes_watch_insert
BEFORE INSERT ON upload_provider_routes
WHEN NEW.watch_dir_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'upload configuration requires a watch directory');
END;

CREATE TRIGGER IF NOT EXISTS trg_upload_provider_routes_watch_update
BEFORE UPDATE OF watch_dir_id ON upload_provider_routes
WHEN NEW.watch_dir_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'upload configuration requires a watch directory');
END;

CREATE TABLE IF NOT EXISTS upload_batches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  watch_dir_id INTEGER,
  upload_route_id INTEGER,
  series_key TEXT NOT NULL,
  series_path TEXT NOT NULL,
  status TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  ready_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(watch_dir_id) REFERENCES watch_dirs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_upload_batches_status_ready ON upload_batches(status, ready_at);
CREATE INDEX IF NOT EXISTS idx_upload_batches_series_key ON upload_batches(series_key, id DESC);

CREATE TABLE IF NOT EXISTS upload_batch_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  local_path TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  file_type TEXT NOT NULL,
  size INTEGER NOT NULL,
  modified_at TEXT NOT NULL,
  sha1 TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(batch_id, local_path),
  FOREIGN KEY(batch_id) REFERENCES upload_batches(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_upload_batch_files_local_fingerprint
  ON upload_batch_files(local_path, size, modified_at, id DESC);

CREATE TABLE IF NOT EXISTS upload_batch_targets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  provider_id INTEGER NOT NULL,
  provider_name TEXT NOT NULL,
  provider_type TEXT NOT NULL,
  remote_root TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  collision_policy TEXT NOT NULL DEFAULT 'fail',
  include_types TEXT NOT NULL DEFAULT '',
  retryable INTEGER NOT NULL DEFAULT 1,
  notification_template_id INTEGER,
  notification_variables TEXT NOT NULL DEFAULT '{}',
  request_interval_ms INTEGER NOT NULL DEFAULT 500,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  available_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(batch_id, provider_id),
  FOREIGN KEY(batch_id) REFERENCES upload_batches(id) ON DELETE CASCADE,
  FOREIGN KEY(provider_id) REFERENCES upload_providers(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_upload_batch_targets_status_available ON upload_batch_targets(status, available_at);

CREATE TABLE IF NOT EXISTS upload_transfers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_target_id INTEGER NOT NULL,
  batch_file_id INTEGER NOT NULL,
  remote_path TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  bytes_total INTEGER NOT NULL DEFAULT 0,
  bytes_transferred INTEGER NOT NULL DEFAULT 0,
  remote_id TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL DEFAULT '',
  remote_sha1 TEXT NOT NULL DEFAULT '',
  error_summary TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(batch_target_id, batch_file_id),
  FOREIGN KEY(batch_target_id) REFERENCES upload_batch_targets(id) ON DELETE CASCADE,
  FOREIGN KEY(batch_file_id) REFERENCES upload_batch_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_upload_transfers_target_status ON upload_transfers(batch_target_id, status);

CREATE TABLE IF NOT EXISTS upload_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_target_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_id TEXT NOT NULL DEFAULT '',
  lease_until TEXT NOT NULL DEFAULT '',
  error_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  delivered_at TEXT,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(batch_target_id, type),
  FOREIGN KEY(batch_target_id) REFERENCES upload_batch_targets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_upload_events_status ON upload_events(status, id);

CREATE TABLE IF NOT EXISTS upload_notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_target_id INTEGER NOT NULL UNIQUE,
  template_id INTEGER NOT NULL,
  template_name TEXT NOT NULL,
  url TEXT NOT NULL,
  headers TEXT NOT NULL DEFAULT '{}',
  payload TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  response_status INTEGER NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  delivered_at TEXT,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(batch_target_id) REFERENCES upload_batch_targets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_upload_notifications_status_available
  ON upload_notifications(status, available_at, id);

CREATE TABLE IF NOT EXISTS tool_status (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  path TEXT NOT NULL DEFAULT '',
  available INTEGER NOT NULL DEFAULT 0,
  version TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS scrape_cache (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  payload_type TEXT NOT NULL,
  request_key TEXT NOT NULL,
  payload TEXT NOT NULL,
  expires_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(source, external_id, payload_type, request_key)
);

CREATE TABLE IF NOT EXISTS emby_api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL UNIQUE,
  api_key TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
