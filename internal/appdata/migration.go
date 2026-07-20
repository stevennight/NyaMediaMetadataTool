package appdata

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"NyaMediaMetadataTool/internal/config"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const (
	legacyConfigName       = "config.yaml"
	migrationRecoveryTable = "__nyammd_desktop_migration_v1"
	migrationVersion       = 1
)

// MigrateLegacy copies data from the command-line application's working
// directory (or its executable directory) into the desktop data directory.
// Existing desktop data is never replaced; an interrupted migration may only
// restore the config embedded in a database installed by this package.
func MigrateLegacy(paths Paths) error {
	return MigrateLegacyContext(context.Background(), paths)
}

// MigrateLegacyContext is the cancellable form of MigrateLegacy. SQLite's
// busy timeout still bounds lock contention, while the snapshot itself may run
// as long as needed for a large legacy database.
func MigrateLegacyContext(ctx context.Context, paths Paths) error {
	if ctx == nil {
		ctx = context.Background()
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve legacy working directory: %w", err)
	}

	candidates := []string{workingDirectory}
	if executable, executableErr := os.Executable(); executableErr == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}
	return migrateLegacyFromContext(ctx, paths, candidates)
}

type legacyData struct {
	database string
	config   []byte
}

func migrateLegacyFrom(paths Paths, candidates []string) error {
	return migrateLegacyFromContext(context.Background(), paths, candidates)
}

func migrateLegacyFromContext(ctx context.Context, paths Paths, candidates []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	recovered, err := recoverInterruptedMigration(ctx, paths)
	if err != nil {
		return err
	}
	if recovered {
		return nil
	}

	initialized, err := desktopDataInitialized(paths)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}

	source, err := findLegacyData(paths, candidates)
	if err != nil {
		return err
	}
	if source == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		return fmt.Errorf("create desktop data directory: %w", err)
	}

	configStage, configData, err := stageConfig(paths, source)
	if err != nil {
		return err
	}
	defer os.Remove(configStage)

	databaseStage := ""
	if source.database != "" {
		databaseStage, err = stageSQLiteSnapshot(ctx, paths.Root, source.database)
		if err != nil {
			return fmt.Errorf("snapshot legacy database %q: %w", source.database, err)
		}
		defer removeSQLiteStage(databaseStage)
		if err := embedRecoveryConfig(ctx, databaseStage, configData); err != nil {
			return fmt.Errorf("prepare migration recovery metadata: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	databaseInstalled := false
	if databaseStage != "" {
		if err := installStagedFile(databaseStage, paths.Database, 0o600); err != nil {
			return fmt.Errorf("install migrated database: %w", err)
		}
		databaseInstalled = true
		removeSQLiteStage(databaseStage)
	}
	if err := installStagedFile(configStage, paths.Config, 0o600); err != nil {
		if databaseInstalled {
			_ = os.Remove(paths.Database)
		}
		return fmt.Errorf("install migrated config: %w", err)
	}
	if databaseInstalled {
		_ = clearRecoveryConfig(context.Background(), paths.Database)
	}
	return nil
}

func desktopDataInitialized(paths Paths) (bool, error) {
	for _, path := range []string{paths.Config, paths.Database} {
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return false, fmt.Errorf("inspect desktop data path %q: %w", path, err)
		}
	}
	return false, nil
}

func findLegacyData(paths Paths, candidates []string) (*legacyData, error) {
	type rankedLegacyData struct {
		priority int
		data     *legacyData
	}
	const (
		priorityConfigOnly = 1
		priorityDefaultDB  = 2
		priorityConfigDB   = 3
	)
	var best rankedLegacyData
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		root, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		key := comparablePath(root)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if samePath(root, paths.Root) {
			continue
		}

		configPath := filepath.Join(root, legacyConfigName)
		prepared, databasePath, recognized, err := prepareLegacyConfig(configPath, root, paths.Database)
		if err != nil {
			return nil, fmt.Errorf("read legacy config %q: %w", configPath, err)
		}
		if recognized {
			exists, err := regularFileStatus(databasePath)
			if err != nil {
				return nil, fmt.Errorf("inspect legacy database %q: %w", databasePath, err)
			}
			priority := priorityConfigOnly
			if !exists {
				databasePath = ""
			} else {
				priority = priorityConfigDB
			}
			if priority > best.priority {
				best = rankedLegacyData{priority: priority, data: &legacyData{database: databasePath, config: prepared}}
			}
			continue
		}

		defaultDatabase := filepath.Join(root, "data", "nyamedia.db")
		exists, err := regularFileStatus(defaultDatabase)
		if err != nil {
			return nil, fmt.Errorf("inspect default legacy database %q: %w", defaultDatabase, err)
		}
		if exists && priorityDefaultDB > best.priority {
			best = rankedLegacyData{priority: priorityDefaultDB, data: &legacyData{database: defaultDatabase}}
		}
	}
	return best.data, nil
}

func prepareLegacyConfig(path string, root string, desktopDatabase string) ([]byte, string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		if resemblesLegacyConfig(data) {
			return nil, "", false, fmt.Errorf("decode application YAML: %w", err)
		}
		// A generic config.yaml in the launch directory must not prevent startup.
		return nil, "", false, nil
	}
	mapping := documentMapping(&document)
	if mapping == nil || !looksLikeLegacyConfig(mapping) {
		return nil, "", false, nil
	}

	database := mappingValue(mapping, "database")
	if database != nil && database.Kind != yaml.MappingNode {
		return nil, "", false, errors.New("database config must be a mapping")
	}
	if database == nil {
		database = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "database"},
			database,
		)
	}
	pathNode := mappingValue(database, "path")
	legacyDatabase := filepath.Join(root, "data", "nyamedia.db")
	if pathNode != nil && pathNode.Kind != yaml.ScalarNode {
		return nil, "", false, errors.New("database.path must be a string")
	}
	if pathNode != nil && strings.TrimSpace(pathNode.Value) != "" {
		legacyDatabase = strings.TrimSpace(pathNode.Value)
		if !filepath.IsAbs(legacyDatabase) {
			legacyDatabase = filepath.Join(root, legacyDatabase)
		}
	}
	if pathNode == nil {
		pathNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str"}
		database.Content = append(database.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "path"},
			pathNode,
		)
	}
	pathNode.Kind = yaml.ScalarNode
	pathNode.Tag = "!!str"
	pathNode.Value = desktopDatabase
	pathNode.Style = 0

	prepared, err := yaml.Marshal(&document)
	if err != nil {
		return nil, "", false, err
	}
	return prepared, filepath.Clean(legacyDatabase), true, nil
}

func documentMapping(document *yaml.Node) *yaml.Node {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

func looksLikeLegacyConfig(mapping *yaml.Node) bool {
	appSpecific := map[string]struct{}{
		"tools": {}, "processing": {}, "scraping": {}, "watchDirs": {},
		"renaming": {}, "upload": {},
	}
	appSpecificCount := 0
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if _, ok := appSpecific[mapping.Content[i].Value]; ok {
			appSpecificCount++
		}
	}
	database := mappingValue(mapping, "database")
	if database != nil && database.Kind == yaml.MappingNode {
		path := mappingValue(database, "path")
		if path != nil && path.Kind == yaml.ScalarNode && strings.EqualFold(filepath.Base(strings.TrimSpace(path.Value)), "nyamedia.db") {
			return true
		}
	}
	return appSpecificCount >= 2
}

func resemblesLegacyConfig(data []byte) bool {
	markers := [][]byte{
		[]byte("nyamedia.db"),
		[]byte("processing:"),
		[]byte("scraping:"),
		[]byte("watchDirs:"),
	}
	matched := 0
	for _, marker := range markers {
		if bytes.Contains(data, marker) {
			matched++
		}
	}
	return matched >= 2 || bytes.Contains(data, []byte("nyamedia.db"))
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func stageConfig(paths Paths, source *legacyData) (string, []byte, error) {
	file, err := os.CreateTemp(paths.Root, ".legacy-config-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("stage migrated config: %w", err)
	}
	stage := file.Name()
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(stage)
		}
	}()

	configData := source.config
	if len(configData) == 0 {
		cfg := config.Default()
		cfg.Database.Path = paths.Database
		configData, err = yaml.Marshal(&cfg)
		if err != nil {
			return "", nil, fmt.Errorf("encode migrated config: %w", err)
		}
	}
	if _, err := file.Write(configData); err != nil {
		return "", nil, fmt.Errorf("write migrated config: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", nil, fmt.Errorf("sync migrated config: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", nil, fmt.Errorf("close migrated config: %w", err)
	}
	cleanup = false
	return stage, configData, nil
}

func stageSQLiteSnapshot(ctx context.Context, root string, source string) (string, error) {
	stageDir, err := os.MkdirTemp(root, ".legacy-database-*")
	if err != nil {
		return "", err
	}
	stage := filepath.Join(stageDir, "snapshot.db")
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stageDir)
		}
	}()

	database, err := sql.Open("sqlite", readOnlySQLiteDSN(source))
	if err != nil {
		return "", err
	}
	defer database.Close()

	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return "", err
	}
	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", stage); err != nil {
		return "", err
	}
	if err := database.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(stage, 0o600); err != nil {
		return "", err
	}
	cleanup = false
	return stage, nil
}

func removeSQLiteStage(stage string) {
	dir := filepath.Dir(stage)
	if strings.HasPrefix(filepath.Base(dir), ".legacy-database-") {
		_ = os.RemoveAll(dir)
		return
	}
	_ = os.Remove(stage)
}

func removeLinkedSQLiteStages(root string, databasePath string) {
	targetInfo, err := os.Stat(databasePath)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".legacy-database-") {
			continue
		}
		stageDir := filepath.Join(root, entry.Name())
		stageInfo, err := os.Stat(filepath.Join(stageDir, "snapshot.db"))
		if err == nil && os.SameFile(targetInfo, stageInfo) {
			_ = os.RemoveAll(stageDir)
		}
	}
}

func readOnlySQLiteDSN(path string) string {
	return sqliteFileDSN(path, "ro")
}

func readWriteSQLiteDSN(path string) string {
	return sqliteFileDSN(path, "rw")
}

func sqliteFileDSN(path string, mode string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	slashPath := filepath.ToSlash(absolute)
	escapedPath := (&url.URL{Path: slashPath}).EscapedPath()
	if runtime.GOOS == "windows" && strings.HasPrefix(slashPath, "//") {
		// Prevent SQLite from treating the UNC server as a URI authority.
		escapedPath = "//" + escapedPath
	}
	return "file:" + escapedPath + "?mode=" + url.QueryEscape(mode)
}

func embedRecoveryConfig(ctx context.Context, databasePath string, configData []byte) error {
	database, err := sql.Open("sqlite", readWriteSQLiteDSN(databasePath))
	if err != nil {
		return err
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE `+migrationRecoveryTable+` (
singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
version INTEGER NOT NULL,
config BLOB NOT NULL
)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+migrationRecoveryTable+` (singleton, version, config) VALUES (1, ?, ?)`, migrationVersion, configData); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := database.Close(); err != nil {
		return err
	}
	return os.Chmod(databasePath, 0o600)
}

func recoverInterruptedMigration(ctx context.Context, paths Paths) (bool, error) {
	if _, err := os.Stat(paths.Config); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect desktop config %q: %w", paths.Config, err)
	}
	if _, err := os.Stat(paths.Database); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect desktop database %q: %w", paths.Database, err)
	}
	removeLinkedSQLiteStages(paths.Root, paths.Database)

	configData, marked, err := readRecoveryConfig(ctx, paths.Database, paths.Database)
	if err != nil {
		return false, fmt.Errorf("inspect interrupted desktop migration: %w", err)
	}
	if !marked {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	stage, err := stageConfigBytes(paths.Root, configData)
	if err != nil {
		return false, err
	}
	defer os.Remove(stage)
	if err := installStagedFile(stage, paths.Config, 0o600); err != nil {
		return false, fmt.Errorf("recover migrated config: %w", err)
	}
	_ = clearRecoveryConfig(context.Background(), paths.Database)
	return true, nil
}

func readRecoveryConfig(ctx context.Context, databasePath string, expectedDatabase string) ([]byte, bool, error) {
	hasHeader, err := hasSQLiteHeader(databasePath)
	if err != nil || !hasHeader {
		return nil, false, err
	}
	database, err := sql.Open("sqlite", readOnlySQLiteDSN(databasePath))
	if err != nil {
		return nil, false, err
	}
	defer database.Close()
	var exists int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, migrationRecoveryTable).Scan(&exists)
	if err != nil {
		return nil, false, err
	}
	if exists == 0 {
		return nil, false, nil
	}
	var version int
	var configData []byte
	if err := database.QueryRowContext(ctx, `SELECT version, config FROM `+migrationRecoveryTable+` WHERE singleton = 1`).Scan(&version, &configData); err != nil {
		return nil, false, err
	}
	if version != migrationVersion {
		return nil, false, fmt.Errorf("unsupported migration recovery version %d", version)
	}
	if err := validateRecoveryConfig(configData, expectedDatabase); err != nil {
		return nil, false, err
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheck); err != nil {
		return nil, false, err
	}
	if !strings.EqualFold(strings.TrimSpace(quickCheck), "ok") {
		return nil, false, fmt.Errorf("migrated database integrity check failed: %s", quickCheck)
	}
	return configData, true, nil
}

func validateRecoveryConfig(data []byte, expectedDatabase string) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode recovery config: %w", err)
	}
	mapping := documentMapping(&document)
	if mapping == nil {
		return errors.New("recovery config must be a mapping")
	}
	database := mappingValue(mapping, "database")
	if database == nil || database.Kind != yaml.MappingNode {
		return errors.New("recovery config database must be a mapping")
	}
	path := mappingValue(database, "path")
	if path == nil || path.Kind != yaml.ScalarNode || !samePath(path.Value, expectedDatabase) {
		return errors.New("recovery config database path does not match the desktop database")
	}
	return nil
}

func clearRecoveryConfig(ctx context.Context, databasePath string) error {
	database, err := sql.Open("sqlite", readWriteSQLiteDSN(databasePath))
	if err != nil {
		return err
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `DROP TABLE IF EXISTS `+migrationRecoveryTable)
	return err
}

func hasSQLiteHeader(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	header := make([]byte, len("SQLite format 3\x00"))
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	return string(header) == "SQLite format 3\x00", nil
}

func stageConfigBytes(root string, data []byte) (string, error) {
	file, err := os.CreateTemp(root, ".legacy-recovery-config-*.yaml")
	if err != nil {
		return "", err
	}
	stage := file.Name()
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(stage)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	cleanup = false
	return stage, nil
}

func installStagedFile(stage string, destination string, mode os.FileMode) error {
	if err := os.Link(stage, destination); err == nil {
		if err := os.Chmod(destination, mode); err != nil {
			_ = os.Remove(destination)
			return err
		}
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("destination %q already exists", destination)
	}

	source, err := os.Open(stage)
	if err != nil {
		return err
	}
	defer source.Close()
	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	installed := false
	defer func() {
		_ = destinationFile.Close()
		if !installed {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(destinationFile, source); err != nil {
		return err
	}
	if err := destinationFile.Sync(); err != nil {
		return err
	}
	if err := destinationFile.Close(); err != nil {
		return err
	}
	installed = true
	return nil
}

func regularFileStatus(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	return classifyRegularFile(path, info, err)
}

func classifyRegularFile(path string, info os.FileInfo, err error) (bool, error) {
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info == nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("%q is not a regular file", path)
	}
	return true, nil
}

func samePath(left string, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return comparablePath(leftAbsolute) == comparablePath(rightAbsolute)
}

func comparablePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
