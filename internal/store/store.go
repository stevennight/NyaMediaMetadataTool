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
	return err
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
`
