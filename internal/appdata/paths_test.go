package appdata

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRootForUsesPlatformDataDirectory(t *testing.T) {
	tests := []struct {
		name string
		goos string
		env  map[string]string
		home string
		want string
	}{
		{name: "windows local app data", goos: "windows", env: map[string]string{"LOCALAPPDATA": `D:\Profiles\Me\Local`}, home: `D:\Profiles\Me`, want: filepath.Join(`D:\Profiles\Me\Local`, directoryName)},
		{name: "mac application support", goos: "darwin", home: "/Users/me", want: filepath.Join("/Users/me", "Library", "Application Support", directoryName)},
		{name: "linux xdg", goos: "linux", env: map[string]string{"XDG_DATA_HOME": "/data/me"}, home: "/home/me", want: filepath.Join("/data/me", "nya-media-metadata-tool")},
		{name: "linux fallback", goos: "linux", home: "/home/me", want: filepath.Join("/home/me", ".local", "share", "nya-media-metadata-tool")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := rootFor(test.goos, func(key string) string { return test.env[key] }, test.home)
			if err != nil {
				t.Fatalf("rootFor returned error: %v", err)
			}
			if filepath.Clean(got) != filepath.Clean(test.want) {
				t.Fatalf("rootFor() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultPathsHonorsDataDirectoryOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "portable-data")
	t.Setenv("NYAMMD_DATA_DIR", root)
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths returned error: %v", err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Root != absolute {
		t.Fatalf("Root = %q, want %q", paths.Root, absolute)
	}
	if _, err := os.Stat(paths.Root); !os.IsNotExist(err) {
		t.Fatalf("DefaultPaths should not create the directory")
	}
}

func TestEnsureCreatesDesktopConfig(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		Root:     root,
		Config:   filepath.Join(root, "config.yaml"),
		Database: filepath.Join(root, "nyamedia.db"),
		Logs:     filepath.Join(root, "logs"),
	}
	if err := Ensure(paths); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if _, err := filepath.Abs(paths.Config); err != nil {
		t.Fatalf("config path is invalid: %v", err)
	}
}

func TestEnsureContextDoesNotInitializeAfterCancellation(t *testing.T) {
	paths := pathsForRoot(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := EnsureContext(ctx, paths)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureContext error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("canceled initialization created config: %v", err)
	}
	if _, err := os.Stat(paths.Database); !os.IsNotExist(err) {
		t.Fatalf("canceled initialization created database: %v", err)
	}
}

func TestEnsureMigratesLegacyConfigAndSQLiteSnapshot(t *testing.T) {
	legacyRoot := filepath.Join(t.TempDir(), "legacy data")
	if err := os.MkdirAll(legacyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(legacyRoot)
	legacyDatabase := filepath.Join(legacyRoot, "data", "nyamedia.db")
	if err := os.MkdirAll(filepath.Dir(legacyDatabase), 0o755); err != nil {
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", legacyDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE legacy_items (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO legacy_items (value) VALUES ('from-wal')`); err != nil {
		t.Fatal(err)
	}

	legacyConfig := []byte(`# legacy comment
database:
  path: data/nyamedia.db
processing:
  concurrency: 7
futureDesktopSetting:
  preserved: true
`)
	legacyConfigPath := filepath.Join(legacyRoot, legacyConfigName)
	if err := os.WriteFile(legacyConfigPath, legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(t.TempDir(), "desktop-data")
	paths := pathsForRoot(targetRoot)
	if err := Ensure(paths); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	sourceAfter, err := os.ReadFile(legacyConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceAfter) != string(legacyConfig) {
		t.Fatal("legacy config was modified")
	}
	migratedConfig, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(migratedConfig, &document); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migratedConfig), "# legacy comment") {
		t.Fatal("legacy config comment was not preserved")
	}
	mapping := documentMapping(&document)
	if mappingValue(mapping, "futureDesktopSetting") == nil {
		t.Fatal("unknown legacy config field was not preserved")
	}
	databaseNode := mappingValue(mappingValue(mapping, "database"), "path")
	if databaseNode == nil || databaseNode.Value != paths.Database {
		t.Fatalf("migrated database path = %v, want %q", databaseNode, paths.Database)
	}

	migratedDatabase, err := sql.Open("sqlite", paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer migratedDatabase.Close()
	var value string
	if err := migratedDatabase.QueryRow(`SELECT value FROM legacy_items`).Scan(&value); err != nil {
		t.Fatalf("query migrated database: %v", err)
	}
	if value != "from-wal" {
		t.Fatalf("migrated value = %q, want from-wal", value)
	}
	var recoveryTables int
	if err := migratedDatabase.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, migrationRecoveryTable).Scan(&recoveryTables); err != nil {
		t.Fatal(err)
	}
	if recoveryTables != 0 {
		t.Fatal("completed migration left recovery metadata in the database")
	}

	if err := os.WriteFile(legacyConfigPath, []byte("processing:\n  concurrency: 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO legacy_items (value) VALUES ('late')`); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(paths); err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}
	configAfterSecondRun, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if string(configAfterSecondRun) != string(migratedConfig) {
		t.Fatal("second migration changed the desktop config")
	}
	var count int
	if err := migratedDatabase.QueryRow(`SELECT COUNT(*) FROM legacy_items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("second migration imported new rows: count = %d, want 1", count)
	}
}

func TestMigrateLegacySkipsWhenDesktopDataExists(t *testing.T) {
	legacyRoot := t.TempDir()
	legacyConfig := []byte("database:\n  path: data/nyamedia.db\nprocessing:\n  concurrency: 9\n")
	if err := os.WriteFile(filepath.Join(legacyRoot, legacyConfigName), legacyConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDatabase := filepath.Join(legacyRoot, "data", "nyamedia.db")
	createSQLiteValue(t, legacyDatabase, "legacy")

	tests := []struct {
		name           string
		initializePath func(Paths) string
		wantMissing    func(Paths) string
	}{
		{
			name:           "existing config prevents database import",
			initializePath: func(paths Paths) string { return paths.Config },
			wantMissing:    func(paths Paths) string { return paths.Database },
		},
		{
			name:           "existing database prevents config import",
			initializePath: func(paths Paths) string { return paths.Database },
			wantMissing:    func(paths Paths) string { return paths.Config },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := pathsForRoot(t.TempDir())
			initialized := test.initializePath(paths)
			if err := os.WriteFile(initialized, []byte("desktop"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := migrateLegacyFrom(paths, []string{legacyRoot}); err != nil {
				t.Fatalf("migrateLegacyFrom returned error: %v", err)
			}
			if _, err := os.Stat(test.wantMissing(paths)); !os.IsNotExist(err) {
				t.Fatalf("the missing destination was unexpectedly created: %v", err)
			}
			content, err := os.ReadFile(initialized)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "desktop" {
				t.Fatalf("existing desktop data changed to %q", content)
			}
		})
	}
}

func TestMigrateLegacyFindsDefaultDatabaseWithoutConfig(t *testing.T) {
	legacyRoot := t.TempDir()
	legacyDatabase := filepath.Join(legacyRoot, "data", "nyamedia.db")
	createSQLiteValue(t, legacyDatabase, "database-only")

	paths := pathsForRoot(t.TempDir())
	if err := migrateLegacyFrom(paths, []string{legacyRoot}); err != nil {
		t.Fatalf("migrateLegacyFrom returned error: %v", err)
	}
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), filepath.ToSlash(paths.Database)) && !strings.Contains(string(data), paths.Database) {
		t.Fatalf("generated config does not reference migrated database: %s", data)
	}

	database, err := sql.Open("sqlite", paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var value string
	if err := database.QueryRow(`SELECT value FROM legacy_value`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "database-only" {
		t.Fatalf("migrated value = %q", value)
	}
}

func TestMigrateLegacyIgnoresUnrelatedConfig(t *testing.T) {
	legacyRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyRoot, legacyConfigName), []byte("server:\n  port: 3000\nfeature: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := pathsForRoot(t.TempDir())
	if err := migrateLegacyFrom(paths, []string{legacyRoot}); err != nil {
		t.Fatalf("migrateLegacyFrom returned error: %v", err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("unrelated config was imported: %v", err)
	}
}

func TestMigrateLegacyRejectsCorruptApplicationConfig(t *testing.T) {
	legacyRoot := t.TempDir()
	data := []byte("database:\n  path: data/nyamedia.db\nprocessing: [\n")
	if err := os.WriteFile(filepath.Join(legacyRoot, legacyConfigName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	paths := pathsForRoot(t.TempDir())
	err := migrateLegacyFrom(paths, []string{legacyRoot})
	if err == nil || !strings.Contains(err.Error(), "decode application YAML") {
		t.Fatalf("migrateLegacyFrom error = %v, want application YAML error", err)
	}
	if _, statErr := os.Stat(paths.Config); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt config created desktop data: %v", statErr)
	}
}

func TestFindLegacyDataPrioritizesCompleteConfigAndDatabase(t *testing.T) {
	incompleteRoot := t.TempDir()
	completeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(incompleteRoot, legacyConfigName), []byte("database:\n  path: data/nyamedia.db\nprocessing:\n  concurrency: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(completeRoot, legacyConfigName), []byte("database:\n  path: data/nyamedia.db\nprocessing:\n  concurrency: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	completeDatabase := filepath.Join(completeRoot, "data", "nyamedia.db")
	createSQLiteValue(t, completeDatabase, "complete")

	source, err := findLegacyData(pathsForRoot(t.TempDir()), []string{incompleteRoot, completeRoot})
	if err != nil {
		t.Fatal(err)
	}
	if source == nil || filepath.Clean(source.database) != filepath.Clean(completeDatabase) {
		t.Fatalf("selected database = %#v, want %q", source, completeDatabase)
	}
	if !strings.Contains(string(source.config), "concurrency: 9") {
		t.Fatalf("selected the wrong config:\n%s", source.config)
	}
}

func TestFindLegacyDataPrioritizesDefaultDatabaseOverConfigOnly(t *testing.T) {
	configOnlyRoot := t.TempDir()
	databaseOnlyRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(configOnlyRoot, legacyConfigName), []byte("database:\n  path: missing.db\nprocessing:\n  concurrency: 4\nscraping:\n  language: zh-CN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defaultDatabase := filepath.Join(databaseOnlyRoot, "data", "nyamedia.db")
	createSQLiteValue(t, defaultDatabase, "default-database")

	source, err := findLegacyData(pathsForRoot(t.TempDir()), []string{configOnlyRoot, databaseOnlyRoot})
	if err != nil {
		t.Fatal(err)
	}
	if source == nil || filepath.Clean(source.database) != filepath.Clean(defaultDatabase) {
		t.Fatalf("selected source = %#v, want default database %q", source, defaultDatabase)
	}
	if len(source.config) != 0 {
		t.Fatalf("default database source unexpectedly reused config-only candidate:\n%s", source.config)
	}
}

func TestMigrateLegacyRejectsNonRegularDatabase(t *testing.T) {
	legacyRoot := t.TempDir()
	databasePath := filepath.Join(legacyRoot, "data", "nyamedia.db")
	if err := os.MkdirAll(databasePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, legacyConfigName), []byte("database:\n  path: data/nyamedia.db\nprocessing:\n  concurrency: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := pathsForRoot(t.TempDir())
	err := migrateLegacyFrom(paths, []string{legacyRoot})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("migrateLegacyFrom error = %v, want non-regular database error", err)
	}
	if _, statErr := os.Stat(paths.Config); !os.IsNotExist(statErr) {
		t.Fatalf("invalid legacy database created desktop config: %v", statErr)
	}
}

func TestClassifyRegularFilePreservesStatErrors(t *testing.T) {
	permissionErr := &os.PathError{Op: "stat", Path: "legacy.db", Err: fs.ErrPermission}
	exists, err := classifyRegularFile("legacy.db", nil, permissionErr)
	if exists || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("classifyRegularFile() = (%v, %v), want permission error", exists, err)
	}
}

func TestStageSQLiteSnapshotHonorsCancellation(t *testing.T) {
	legacyDatabase := filepath.Join(t.TempDir(), "legacy.db")
	createSQLiteValue(t, legacyDatabase, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stageSQLiteSnapshot(ctx, t.TempDir(), legacyDatabase)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stageSQLiteSnapshot error = %v, want context.Canceled", err)
	}
}

func TestReadOnlySQLiteDSNDoesNotCreateMissingSource(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing legacy.db")
	database, err := sql.Open("sqlite", readOnlySQLiteDSN(missing))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Ping(); err == nil {
		t.Fatal("opening a missing legacy database in read-only mode unexpectedly succeeded")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("read-only DSN created the missing source: %v", err)
	}
}

func TestMigrateLegacyRecoversConfigFromInstalledDatabase(t *testing.T) {
	legacyRoot := t.TempDir()
	legacyDatabase := filepath.Join(legacyRoot, "data", "nyamedia.db")
	createSQLiteValue(t, legacyDatabase, "recover")
	legacyConfigPath := filepath.Join(legacyRoot, legacyConfigName)
	if err := os.WriteFile(legacyConfigPath, []byte("database:\n  path: data/nyamedia.db\nprocessing:\n  concurrency: 6\nfutureSetting:\n  retained: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := pathsForRoot(t.TempDir())
	prepared, databasePath, recognized, err := prepareLegacyConfig(legacyConfigPath, legacyRoot, paths.Database)
	if err != nil || !recognized {
		t.Fatalf("prepareLegacyConfig() = recognized %v, error %v", recognized, err)
	}
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	stage, err := stageSQLiteSnapshot(context.Background(), paths.Root, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer removeSQLiteStage(stage)
	if err := embedRecoveryConfig(context.Background(), stage, prepared); err != nil {
		t.Fatal(err)
	}
	if err := installStagedFile(stage, paths.Database, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("config should be absent before recovery: %v", err)
	}

	if err := migrateLegacyFrom(paths, nil); err != nil {
		t.Fatalf("recover interrupted migration: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(stage)); !os.IsNotExist(err) {
		t.Fatalf("recovery did not remove linked staging directory: %v", err)
	}
	configData, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), "concurrency: 6") || !strings.Contains(string(configData), "futureSetting") {
		t.Fatalf("recovered config lost settings:\n%s", configData)
	}
	_, marked, err := readRecoveryConfig(context.Background(), paths.Database, paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	if marked {
		t.Fatal("recovered database still contains migration metadata")
	}
}

func TestMigrateLegacyDoesNotRecoverUnmarkedExistingDatabase(t *testing.T) {
	paths := pathsForRoot(t.TempDir())
	createSQLiteValue(t, paths.Database, "existing")
	if err := migrateLegacyFrom(paths, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("unmarked database unexpectedly created config: %v", err)
	}
}

func createSQLiteValue(t *testing.T, path string, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE legacy_value (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO legacy_value (value) VALUES (?)`, value); err != nil {
		t.Fatal(err)
	}
}
