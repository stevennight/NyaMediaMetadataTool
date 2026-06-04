package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
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

	return store, nil
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

func (s *Store) Migrate(ctx context.Context) error {
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
	if err := s.ensureScanScopeTaskColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureEmbyAPIKeyNoteColumn(ctx); err != nil {
		return err
	}
	return s.ensureWatchDirSplitColumns(ctx)
}

func (s *Store) ensureTaskOverwriteColumn(ctx context.Context) error {
	return s.ensureTaskColumn(ctx, "overwrite_existing", `ALTER TABLE tasks ADD COLUMN overwrite_existing INTEGER NOT NULL DEFAULT 0`)
}

func (s *Store) ensureTaskScanRunColumn(ctx context.Context) error {
	return s.ensureTaskColumn(ctx, "scan_run_id", `ALTER TABLE tasks ADD COLUMN scan_run_id TEXT NOT NULL DEFAULT ''`)
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
CREATE TABLE IF NOT EXISTS watch_dirs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  recursive INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  watch_enabled INTEGER NOT NULL DEFAULT 1,
  scan_on_start INTEGER NOT NULL DEFAULT 0,
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
