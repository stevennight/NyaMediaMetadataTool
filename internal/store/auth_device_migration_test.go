package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAddsAuthDeviceWithoutGuessingLegacyCookieDevice(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "legacy-auth-device.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.db.ExecContext(ctx, `
CREATE TABLE upload_providers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  remote_root TEXT NOT NULL DEFAULT '/',
  user_agent TEXT NOT NULL DEFAULT '',
  collision_policy TEXT NOT NULL DEFAULT 'replace',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE upload_provider_secrets (
  provider_id INTEGER NOT NULL,
  secret_key TEXT NOT NULL,
  secret_value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(provider_id, secret_key),
  FOREIGN KEY(provider_id) REFERENCES upload_providers(id) ON DELETE CASCADE
);
INSERT INTO upload_providers (id, name, type, enabled) VALUES (1, 'Legacy 115', '115cookie', 1);
INSERT INTO upload_provider_secrets (provider_id, secret_key, secret_value) VALUES (1, 'cookie', 'UID=legacy');
`); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := st.GetUploadProvider(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !provider.HasCookie || provider.AuthDevice != "" {
		t.Fatalf("legacy Cookie device must remain unknown after migration: %#v", provider)
	}
}
