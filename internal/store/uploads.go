package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"
)

const (
	UploadProviderType115Cookie = "115cookie"
	UploadProviderType115Open   = "115open"
	UploadProviderType123Pan    = "123pan"
	UploadProviderTypeBaiduPan  = "baidupan"

	UploadBatchCollecting = "collecting"
	UploadBatchPending    = "pending"
	UploadBatchRunning    = "running"
	UploadBatchCompleted  = "completed"
	UploadBatchPartial    = "partial"
	UploadBatchFailed     = "failed"
	UploadBatchCanceled   = "canceled"

	UploadTargetWaiting   = "waiting"
	UploadTargetPending   = "pending"
	UploadTargetRunning   = "running"
	UploadTargetCompleted = "completed"
	UploadTargetFailed    = "failed"
	UploadTargetCanceled  = "canceled"

	UploadTransferPending   = "pending"
	UploadTransferRunning   = "running"
	UploadTransferCompleted = "completed"
	UploadTransferFailed    = "failed"
	UploadTransferCanceled  = "canceled"

	UploadOutcomeCreated   = "created"
	UploadOutcomeReplaced  = "replaced"
	UploadOutcomeUnchanged = "unchanged"
	UploadOutcomeSkipped   = "skipped"

	UploadEventTargetVerified = "upload_target_verified"

	UploadEventPending    = "pending"
	UploadEventProcessing = "processing"
	UploadEventDelivered  = "delivered"
	UploadEventFailed     = "failed"
)

var (
	ErrUploadProviderNotFound      = errors.New("upload provider not found")
	ErrUploadProviderInUse         = errors.New("upload provider has upload history")
	ErrUploadProviderTypeImmutable = errors.New("upload provider type cannot be changed")
	ErrUploadProviderCookieOnly    = errors.New("115 Cookie credentials must use the dedicated Cookie API")
	ErrInvalidUploadConfig         = errors.New("invalid upload configuration")
	ErrUploadBatchNotFound         = errors.New("upload batch not found")
	ErrUploadTargetNotFound        = errors.New("upload target not found")
	ErrUploadTargetNotRetryable    = errors.New("upload target is not retryable")
	ErrNoPendingUploadTarget       = errors.New("no pending upload target")
)

var uploadFileTypes = []string{
	"video", "mediainfo", "subtitle", "nfo", "thumb", "tvshow-nfo", "season-nfo",
	"bif", "poster", "fanart", "clearlogo", "clearart", "season-poster",
}

var uploadProviderAuthDevices = map[string]struct{}{
	"web": {}, "android": {}, "ios": {}, "tv": {}, "alipaymini": {}, "wechatmini": {}, "qandroid": {},
}

var uploadFileTypeSet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(uploadFileTypes))
	for _, fileType := range uploadFileTypes {
		result[fileType] = struct{}{}
	}
	return result
}()

// UploadProvider is one configured account instance. Secrets are deliberately
// excluded so API list responses cannot expose credentials.
type UploadProvider struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Enabled    bool   `json:"enabled"`
	UserAgent  string `json:"userAgent"`
	HasCookie  bool   `json:"hasCookie"`
	AuthDevice string `json:"authDevice"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// UploadProviderRoute is one upload step configured on a watch directory.
// WatchDirID is always populated; nil is accepted only while decoding a draft.
type UploadProviderRoute struct {
	ID                     int64             `json:"id"`
	ProviderID             int64             `json:"providerId"`
	WatchDirID             *int64            `json:"watchDirId"`
	Enabled                bool              `json:"enabled"`
	RemoteRoot             string            `json:"remoteRoot"`
	CollisionPolicy        string            `json:"collisionPolicy"`
	IncludeTypes           []string          `json:"includeTypes"`
	NotificationTemplateID *int64            `json:"notificationTemplateId,omitempty"`
	NotificationVariables  map[string]string `json:"notificationVariables,omitempty"`
	CreatedAt              string            `json:"createdAt,omitempty"`
	UpdatedAt              string            `json:"updatedAt,omitempty"`
}

type UploadBatch struct {
	ID                 int64  `json:"id"`
	WatchDirID         *int64 `json:"watchDirId"`
	UploadRouteID      *int64 `json:"uploadRouteId,omitempty"`
	SeriesKey          string `json:"seriesKey"`
	SeriesPath         string `json:"seriesPath"`
	Status             string `json:"status"`
	Revision           int    `json:"revision"`
	ReadyAt            string `json:"readyAt"`
	FileCount          int    `json:"fileCount"`
	ProviderName       string `json:"providerName"`
	RemoteRoot         string `json:"remoteRoot"`
	TargetCount        int    `json:"targetCount"`
	CompletedTargets   int    `json:"completedTargets"`
	FailedTargets      int    `json:"failedTargets"`
	TransferCount      int    `json:"transferCount"`
	CompletedTransfers int    `json:"completedTransfers"`
	FailedTransfers    int    `json:"failedTransfers"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type UploadBatchFile struct {
	ID           int64  `json:"id"`
	BatchID      int64  `json:"batchId"`
	LocalPath    string `json:"localPath"`
	RelativePath string `json:"relativePath"`
	FileType     string `json:"fileType"`
	Size         int64  `json:"size"`
	ModifiedAt   string `json:"modifiedAt"`
	SHA1         string `json:"sha1,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type UploadBatchTarget struct {
	ID                     int64             `json:"id"`
	BatchID                int64             `json:"batchId"`
	ProviderID             int64             `json:"providerId"`
	ProviderName           string            `json:"providerName"`
	ProviderType           string            `json:"providerType"`
	RemoteRoot             string            `json:"remoteRoot"`
	UserAgent              string            `json:"userAgent"`
	CollisionPolicy        string            `json:"collisionPolicy"`
	IncludeTypes           []string          `json:"includeTypes"`
	Retryable              bool              `json:"retryable"`
	NotificationTemplateID *int64            `json:"notificationTemplateId,omitempty"`
	NotificationVariables  map[string]string `json:"notificationVariables,omitempty"`
	Status                 string            `json:"status"`
	Attempts               int               `json:"attempts"`
	ErrorSummary           string            `json:"errorSummary"`
	AvailableAt            string            `json:"availableAt"`
	StartedAt              string            `json:"startedAt"`
	FinishedAt             string            `json:"finishedAt"`
	CreatedAt              string            `json:"createdAt"`
	UpdatedAt              string            `json:"updatedAt"`
}

type UploadTransfer struct {
	ID               int64  `json:"id"`
	BatchTargetID    int64  `json:"batchTargetId"`
	BatchFileID      int64  `json:"batchFileId"`
	LocalPath        string `json:"localPath"`
	RelativePath     string `json:"relativePath"`
	FileType         string `json:"fileType"`
	ModifiedAt       string `json:"modifiedAt"`
	RemotePath       string `json:"remotePath"`
	Status           string `json:"status"`
	Attempts         int    `json:"attempts"`
	BytesTotal       int64  `json:"bytesTotal"`
	BytesTransferred int64  `json:"bytesTransferred"`
	RemoteID         string `json:"remoteId"`
	Outcome          string `json:"outcome,omitempty"`
	LocalSHA1        string `json:"localSha1,omitempty"`
	RemoteSHA1       string `json:"remoteSha1,omitempty"`
	ErrorSummary     string `json:"errorSummary"`
	StartedAt        string `json:"startedAt"`
	FinishedAt       string `json:"finishedAt"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type UploadTransferCompletion struct {
	RemoteID   string
	Outcome    string
	LocalSHA1  string
	RemoteSHA1 string
}

// UploadOutcomeChangesRemote is the notification boundary for uploads.
// Future scan or notification consumers must only act on outcomes for which
// this function returns true.
func UploadOutcomeChangesRemote(outcome string) bool {
	switch normalizeUploadOutcome(outcome) {
	case UploadOutcomeCreated, UploadOutcomeReplaced:
		return true
	default:
		return false
	}
}

type UploadEvent struct {
	ID            int64  `json:"id"`
	BatchTargetID int64  `json:"batchTargetId"`
	Type          string `json:"type"`
	Payload       string `json:"payload"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	AvailableAt   string `json:"availableAt"`
	LeaseID       string `json:"leaseId,omitempty"`
	LeaseUntil    string `json:"leaseUntil,omitempty"`
	ErrorSummary  string `json:"errorSummary,omitempty"`
	CreatedAt     string `json:"createdAt"`
	DeliveredAt   string `json:"deliveredAt"`
}

type UploadBatchDetail struct {
	Batch     UploadBatch         `json:"batch"`
	Files     []UploadBatchFile   `json:"files"`
	Targets   []UploadBatchTarget `json:"targets"`
	Transfers []UploadTransfer    `json:"transfers"`
}

type UploadBatchFilters struct {
	Page     int
	PageSize int
	Status   string
	Path     string
}

type UploadBatchListResult struct {
	Items    []UploadBatch `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

type UploadSummary struct {
	Collecting int `json:"collecting"`
	Pending    int `json:"pending"`
	Running    int `json:"running"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
}

type UploadCandidate struct {
	LocalPath    string
	RelativePath string
	FileType     string
	Size         int64
	ModifiedAt   time.Time
}

type UploadCollectionInput struct {
	WatchDirID  *int64
	SeriesKey   string
	SeriesPath  string
	QuietPeriod time.Duration
	Files       []UploadCandidate
}

func (s *Store) CreateUploadProvider(ctx context.Context, provider UploadProvider) (UploadProvider, error) {
	if err := normalizeUploadProvider(&provider); err != nil {
		return UploadProvider{}, err
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO upload_providers (name, type, enabled, user_agent, auth_device, updated_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`, provider.Name, provider.Type, boolToInt(provider.Enabled), provider.UserAgent, provider.AuthDevice)
	if err != nil {
		return UploadProvider{}, err
	}
	provider.ID, err = result.LastInsertId()
	if err != nil {
		return UploadProvider{}, err
	}
	return s.GetUploadProvider(ctx, provider.ID)
}

func (s *Store) ListUploadProviders(ctx context.Context) ([]UploadProvider, error) {
	rows, err := s.db.QueryContext(ctx, uploadProviderSelect+` ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]UploadProvider, 0)
	for rows.Next() {
		provider, err := scanUploadProvider(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) GetUploadProvider(ctx context.Context, id int64) (UploadProvider, error) {
	provider, err := scanUploadProvider(s.db.QueryRowContext(ctx, uploadProviderSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadProvider{}, ErrUploadProviderNotFound
	}
	return provider, err
}

func (s *Store) UpdateUploadProvider(ctx context.Context, provider UploadProvider) (UploadProvider, error) {
	if provider.ID <= 0 {
		return UploadProvider{}, ErrUploadProviderNotFound
	}
	// Authentication metadata belongs to the dedicated Cookie flow. Ignoring a
	// stale form value here prevents a concurrent authorization from being lost.
	provider.AuthDevice = ""
	if err := normalizeUploadProvider(&provider); err != nil {
		return UploadProvider{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadProvider{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
UPDATE upload_providers
SET name = ?,
    enabled = ?,
    user_agent = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND type = ?
`, provider.Name, boolToInt(provider.Enabled), provider.UserAgent, provider.ID, provider.Type)
	if err != nil {
		return UploadProvider{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UploadProvider{}, err
	}
	if affected == 0 {
		var storedType string
		if err := tx.QueryRowContext(ctx, `SELECT type FROM upload_providers WHERE id = ?`, provider.ID).Scan(&storedType); errors.Is(err, sql.ErrNoRows) {
			return UploadProvider{}, ErrUploadProviderNotFound
		} else if err != nil {
			return UploadProvider{}, err
		}
		if storedType != provider.Type {
			return UploadProvider{}, ErrUploadProviderTypeImmutable
		}
	}
	if err := tx.Commit(); err != nil {
		return UploadProvider{}, err
	}
	return s.GetUploadProvider(ctx, provider.ID)
}

func (s *Store) DeleteUploadProvider(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var historyCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM upload_batch_targets WHERE provider_id = ?`, id).Scan(&historyCount); err != nil {
		return err
	}
	if historyCount > 0 {
		return ErrUploadProviderInUse
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM upload_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUploadProviderNotFound
	}
	return tx.Commit()
}

func (s *Store) ListWatchDirUploadConfigs(ctx context.Context, watchDirID int64) ([]UploadProviderRoute, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, provider_id, watch_dir_id, enabled, remote_root, collision_policy, include_types,
       notification_template_id, notification_variables, created_at, updated_at
FROM upload_provider_routes
WHERE watch_dir_id = ?
ORDER BY id
`, watchDirID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := make([]UploadProviderRoute, 0)
	for rows.Next() {
		route, err := scanUploadProviderRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func ValidateUploadProviderRoutes(routes []UploadProviderRoute) error {
	for _, route := range routes {
		if route.ProviderID <= 0 {
			return fmt.Errorf("%w: provider id is required", ErrInvalidUploadConfig)
		}
		if err := validateStoredUploadTypes(route.IncludeTypes); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidUploadConfig, err)
		}
		if len(normalizeStoredUploadTypes(route.IncludeTypes)) == 0 {
			return fmt.Errorf("%w: at least one include type is required", ErrInvalidUploadConfig)
		}
		if _, err := encodeNotificationVariables(route.NotificationVariables); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidUploadConfig, err)
		}
	}
	return nil
}

func replaceWatchDirUploadConfigsTx(ctx context.Context, tx *sql.Tx, watchDirID int64, routes []UploadProviderRoute) error {
	if err := ValidateUploadProviderRoutes(routes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM upload_provider_routes WHERE watch_dir_id = ?`, watchDirID); err != nil {
		return err
	}
	for _, route := range routes {
		var providerExists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM upload_providers WHERE id = ?`, route.ProviderID).Scan(&providerExists); errors.Is(err, sql.ErrNoRows) {
			return ErrUploadProviderNotFound
		} else if err != nil {
			return err
		}
		route.RemoteRoot = normalizeRemoteRoot(route.RemoteRoot)
		route.CollisionPolicy = normalizeCollisionPolicy(route.CollisionPolicy)
		route.IncludeTypes = normalizeStoredUploadTypes(route.IncludeTypes)
		encodedTypes, err := json.Marshal(route.IncludeTypes)
		if err != nil {
			return err
		}
		encodedVariables, err := encodeNotificationVariables(route.NotificationVariables)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidUploadConfig, err)
		}
		if route.NotificationTemplateID != nil {
			var payloadTemplate string
			err := tx.QueryRowContext(ctx, `
SELECT payload_template FROM upload_notification_templates WHERE id = ?
`, *route.NotificationTemplateID).Scan(&payloadTemplate)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUploadNotificationTemplateNotFound
			}
			if err != nil {
				return err
			}
			if err := validateNotificationPayloadVariables(payloadTemplate, route.NotificationVariables); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidUploadConfig, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO upload_provider_routes
  (provider_id, watch_dir_id, enabled, remote_root, collision_policy, include_types, notification_template_id, notification_variables, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`, route.ProviderID, watchDirID, boolToInt(route.Enabled), route.RemoteRoot, route.CollisionPolicy, string(encodedTypes), route.NotificationTemplateID, encodedVariables); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetUploadProviderSecret(ctx context.Context, providerID int64, key string, value string) error {
	key = strings.TrimSpace(key)
	if providerID <= 0 || key == "" {
		return errors.New("provider id and secret key are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var providerType string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM upload_providers WHERE id = ?`, providerID).Scan(&providerType); errors.Is(err, sql.ErrNoRows) {
		return ErrUploadProviderNotFound
	} else if err != nil {
		return err
	}
	if providerType == UploadProviderType115Cookie && strings.EqualFold(key, "cookie") {
		return ErrUploadProviderCookieOnly
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO upload_provider_secrets (provider_id, secret_key, secret_value, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider_id, secret_key) DO UPDATE SET secret_value = excluded.secret_value, updated_at = CURRENT_TIMESTAMP
`, providerID, key, value); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetUploadProviderCookie(ctx context.Context, providerID int64, cookie string, authDevice string) error {
	cookie = strings.TrimSpace(cookie)
	authDevice, err := normalizeUploadProviderAuthDevice(authDevice)
	if err != nil {
		return err
	}
	if providerID <= 0 || cookie == "" {
		return errors.New("provider id and cookie are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var providerType string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM upload_providers WHERE id = ?`, providerID).Scan(&providerType); errors.Is(err, sql.ErrNoRows) {
		return ErrUploadProviderNotFound
	} else if err != nil {
		return err
	}
	if providerType != UploadProviderType115Cookie {
		return errors.New("provider does not use 115 Cookie authentication")
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO upload_provider_secrets (provider_id, secret_key, secret_value, updated_at)
VALUES (?, 'cookie', ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider_id, secret_key) DO UPDATE SET secret_value = excluded.secret_value, updated_at = CURRENT_TIMESTAMP
`, providerID, cookie); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_providers SET auth_device = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, authDevice, providerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUploadProviderSecret(ctx context.Context, providerID int64, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `
SELECT secret_value
FROM upload_provider_secrets
WHERE provider_id = ? AND secret_key = ?
`, providerID, strings.TrimSpace(key)).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) DeleteUploadProviderSecret(ctx context.Context, providerID int64, key string) error {
	key = strings.TrimSpace(key)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM upload_provider_secrets WHERE provider_id = ? AND secret_key = ?`, providerID, key); err != nil {
		return err
	}
	if key == "cookie" {
		if _, err := tx.ExecContext(ctx, `UPDATE upload_providers SET auth_device = '', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, providerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CollectUploadBatch preserves the original single-result API for callers
// that only need to know whether upload work exists.
func (s *Store) CollectUploadBatch(ctx context.Context, input UploadCollectionInput) (UploadBatch, bool, error) {
	batches, created, err := s.CollectUploadBatches(ctx, input)
	if err != nil || len(batches) == 0 {
		return UploadBatch{}, false, err
	}
	return batches[0], created > 0, nil
}

// CollectUploadBatches coalesces files by watch-directory upload route and
// series until each route-specific batch is sealed.
func (s *Store) CollectUploadBatches(ctx context.Context, input UploadCollectionInput) ([]UploadBatch, int, error) {
	if strings.TrimSpace(input.SeriesKey) == "" || strings.TrimSpace(input.SeriesPath) == "" {
		return nil, 0, errors.New("series key and series path are required")
	}
	if input.WatchDirID == nil || *input.WatchDirID <= 0 {
		return nil, 0, nil
	}
	if len(input.Files) == 0 {
		return nil, 0, nil
	}
	if input.QuietPeriod <= 0 {
		input.QuietPeriod = 2 * time.Minute
	}
	for _, candidate := range input.Files {
		if err := validateUploadCandidate(candidate); err != nil {
			return nil, 0, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	configuredTargets, err := listEnabledWatchDirUploadTargetsTx(ctx, tx, *input.WatchDirID)
	if err != nil {
		return nil, 0, err
	}
	readyAt := formatStoreTime(time.Now().UTC().Add(input.QuietPeriod))
	batchIDs := make([]int64, 0, len(configuredTargets))
	created := 0

	for _, configured := range configuredTargets {
		if !hasAllowedUploadCandidate(input.Files, configured.route.IncludeTypes) {
			continue
		}

		var batchID int64
		existing := false
		err = tx.QueryRowContext(ctx, `
SELECT id
FROM upload_batches
WHERE watch_dir_id = ? AND upload_route_id = ? AND series_key = ? AND status = ?
ORDER BY id DESC
LIMIT 1
`, *input.WatchDirID, configured.route.ID, input.SeriesKey, UploadBatchCollecting).Scan(&batchID)
		if err == nil {
			existing = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, 0, err
		}

		if !existing {
			var revision int
			if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision), 0) + 1
FROM upload_batches
WHERE watch_dir_id = ? AND upload_route_id = ? AND series_key = ?
`, *input.WatchDirID, configured.route.ID, input.SeriesKey).Scan(&revision); err != nil {
				return nil, 0, err
			}
			result, err := tx.ExecContext(ctx, `
INSERT INTO upload_batches (watch_dir_id, upload_route_id, series_key, series_path, status, revision, ready_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
`, *input.WatchDirID, configured.route.ID, input.SeriesKey, input.SeriesPath, UploadBatchCollecting, revision, readyAt)
			if err != nil {
				return nil, 0, err
			}
			batchID, err = result.LastInsertId()
			if err != nil {
				return nil, 0, err
			}
			encodedTypes, err := json.Marshal(configured.route.IncludeTypes)
			if err != nil {
				return nil, 0, err
			}
			encodedVariables, err := encodeNotificationVariables(configured.route.NotificationVariables)
			if err != nil {
				return nil, 0, err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO upload_batch_targets
  (batch_id, provider_id, provider_name, provider_type, remote_root, user_agent, collision_policy, include_types,
   notification_template_id, notification_variables, status, available_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, batchID, configured.provider.ID, configured.provider.Name, configured.provider.Type, configured.route.RemoteRoot,
				configured.provider.UserAgent, configured.route.CollisionPolicy, string(encodedTypes),
				configured.route.NotificationTemplateID, encodedVariables, UploadTargetWaiting, readyAt); err != nil {
				return nil, 0, err
			}
			created++
		}

		targets, err := listUploadBatchTargetsTx(ctx, tx, batchID)
		if err != nil {
			return nil, 0, err
		}
		for _, candidate := range input.Files {
			if !uploadTypeAllowed(configured.route.IncludeTypes, candidate.FileType) {
				continue
			}
			var previousSize int64
			var previousModified string
			modifiedAt := formatStoreTime(candidate.ModifiedAt.UTC())
			previousErr := tx.QueryRowContext(ctx, `
SELECT size, modified_at FROM upload_batch_files WHERE batch_id = ? AND local_path = ?
`, batchID, candidate.LocalPath).Scan(&previousSize, &previousModified)
			fileChanged := previousErr == nil && (previousSize != candidate.Size || previousModified != modifiedAt)
			if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
				return nil, 0, previousErr
			}
			var cachedSHA1 string
			cacheErr := tx.QueryRowContext(ctx, `
SELECT sha1
FROM upload_batch_files
WHERE local_path = ? AND size = ? AND modified_at = ? AND sha1 != ''
ORDER BY id DESC
LIMIT 1
`, candidate.LocalPath, candidate.Size, modifiedAt).Scan(&cachedSHA1)
			if cacheErr != nil && !errors.Is(cacheErr, sql.ErrNoRows) {
				return nil, 0, cacheErr
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO upload_batch_files (batch_id, local_path, relative_path, file_type, size, modified_at, sha1, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(batch_id, local_path) DO UPDATE SET
  relative_path = excluded.relative_path,
  file_type = excluded.file_type,
  size = excluded.size,
  modified_at = excluded.modified_at,
  sha1 = excluded.sha1,
  updated_at = CURRENT_TIMESTAMP
`, batchID, candidate.LocalPath, candidate.RelativePath, candidate.FileType, candidate.Size, modifiedAt, strings.ToUpper(strings.TrimSpace(cachedSHA1))); err != nil {
				return nil, 0, err
			}
			var batchFileID int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM upload_batch_files WHERE batch_id = ? AND local_path = ?`, batchID, candidate.LocalPath).Scan(&batchFileID); err != nil {
				return nil, 0, err
			}
			for _, target := range targets {
				if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO upload_transfers (batch_target_id, batch_file_id, remote_path, status, bytes_total)
VALUES (?, ?, ?, ?, ?)
`, target.ID, batchFileID, joinRemotePath(target.RemoteRoot, candidate.RelativePath), UploadTransferPending, candidate.Size); err != nil {
					return nil, 0, err
				}
				if fileChanged {
					if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = CASE WHEN status = ? THEN status ELSE ? END,
    bytes_total = ?, bytes_transferred = 0, remote_id = '', outcome = '', remote_sha1 = '', error_summary = '',
    started_at = NULL, finished_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE batch_target_id = ? AND batch_file_id = ?
`, UploadTransferCanceled, UploadTransferPending, candidate.Size, target.ID, batchFileID); err != nil {
						return nil, 0, err
					}
				}
			}
		}

		if _, err := tx.ExecContext(ctx, `UPDATE upload_batches SET ready_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, readyAt, batchID); err != nil {
			return nil, 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE upload_batch_targets SET available_at = ?, updated_at = CURRENT_TIMESTAMP WHERE batch_id = ? AND status = ?`, readyAt, batchID, UploadTargetWaiting); err != nil {
			return nil, 0, err
		}
		batchIDs = append(batchIDs, batchID)
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	batches := make([]UploadBatch, 0, len(batchIDs))
	for _, batchID := range batchIDs {
		batch, err := s.GetUploadBatch(ctx, batchID)
		if err != nil {
			return nil, 0, err
		}
		batches = append(batches, batch)
	}
	return batches, created, nil
}

// SealDueUploadBatches moves quiet, fully processed show changes into the
// upload queue. It returns the number of batches made eligible for workers.
func (s *Store) SealDueUploadBatches(ctx context.Context, now time.Time, quietPeriod time.Duration) (int, error) {
	if quietPeriod <= 0 {
		quietPeriod = 2 * time.Minute
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, series_path
FROM upload_batches
WHERE status = ? AND ready_at <= ?
ORDER BY id ASC
`, UploadBatchCollecting, formatStoreTime(now.UTC()))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type dueBatch struct {
		id         int64
		seriesPath string
	}
	items := make([]dueBatch, 0)
	for rows.Next() {
		var item dueBatch
		if err := rows.Scan(&item.id, &item.seriesPath); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	sealed := 0
	for _, item := range items {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return sealed, err
		}
		active, err := hasActiveMediaTasksForSeriesTx(ctx, tx, item.seriesPath)
		if err != nil {
			tx.Rollback()
			return sealed, err
		}
		if active {
			_, err = tx.ExecContext(ctx, `UPDATE upload_batches SET ready_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`, formatStoreTime(now.UTC().Add(quietPeriod)), item.id, UploadBatchCollecting)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE upload_batch_targets SET status = ?, available_at = ?, updated_at = CURRENT_TIMESTAMP WHERE batch_id = ? AND status = ?`, UploadTargetPending, formatStoreTime(now.UTC()), item.id, UploadTargetWaiting)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE upload_batches SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`, UploadBatchPending, item.id, UploadBatchCollecting)
			}
		}
		if err != nil {
			tx.Rollback()
			return sealed, err
		}
		if err := tx.Commit(); err != nil {
			return sealed, err
		}
		if !active {
			sealed++
		}
	}
	return sealed, nil
}

func (s *Store) ClaimNextUploadTarget(ctx context.Context) (UploadBatchTarget, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadBatchTarget{}, err
	}
	defer tx.Rollback()

	target, err := scanUploadBatchTarget(tx.QueryRowContext(ctx, uploadBatchTargetSelect+`
WHERE t.status = ? AND t.available_at <= ? AND b.status IN (?, ?)
ORDER BY t.id ASC
LIMIT 1
`, UploadTargetPending, formatStoreTime(time.Now().UTC()), UploadBatchPending, UploadBatchRunning))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadBatchTarget{}, ErrNoPendingUploadTarget
	}
	if err != nil {
		return UploadBatchTarget{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, attempts = attempts + 1, started_at = CURRENT_TIMESTAMP, error_summary = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, UploadTargetRunning, target.ID, UploadTargetPending); err != nil {
		return UploadBatchTarget{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_batches SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`, UploadBatchRunning, target.BatchID, UploadBatchPending); err != nil {
		return UploadBatchTarget{}, err
	}
	if err := tx.Commit(); err != nil {
		return UploadBatchTarget{}, err
	}
	target.Status = UploadTargetRunning
	target.Attempts++
	target.ErrorSummary = ""
	return target, nil
}

func (s *Store) ListUploadTransfersByTarget(ctx context.Context, targetID int64) ([]UploadTransfer, error) {
	rows, err := s.db.QueryContext(ctx, uploadTransferSelect+` WHERE t.batch_target_id = ? ORDER BY t.id ASC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadTransfer, 0)
	for rows.Next() {
		item, err := scanUploadTransfer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) StartUploadTransfer(ctx context.Context, transferID int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?, attempts = attempts + 1, started_at = CURRENT_TIMESTAMP, error_summary = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status IN (?, ?)
`, UploadTransferRunning, transferID, UploadTransferPending, UploadTransferFailed)
	return err
}

func (s *Store) CompleteUploadTransfer(ctx context.Context, transferID int64, remoteID string) error {
	return s.CompleteUploadTransferWithResult(ctx, transferID, UploadTransferCompletion{
		RemoteID: remoteID,
		Outcome:  UploadOutcomeCreated,
	})
}

func (s *Store) CompleteUploadTransferWithResult(ctx context.Context, transferID int64, completion UploadTransferCompletion) error {
	outcome := normalizeUploadOutcome(completion.Outcome)
	if outcome == "" {
		outcome = UploadOutcomeCreated
	}
	remoteChanged := 0
	if UploadOutcomeChangesRemote(outcome) {
		remoteChanged = 1
	}
	localSHA1 := strings.ToUpper(strings.TrimSpace(completion.LocalSHA1))
	remoteSHA1 := strings.ToUpper(strings.TrimSpace(completion.RemoteSHA1))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?,
    bytes_transferred = CASE WHEN ? = 1 THEN bytes_total ELSE 0 END,
    remote_id = ?, outcome = ?, remote_sha1 = ?, error_summary = '',
    finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, UploadTransferCompleted, remoteChanged, strings.TrimSpace(completion.RemoteID), outcome, remoteSHA1, transferID); err != nil {
		return err
	}
	if localSHA1 != "" {
		if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_files
SET sha1 = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = (SELECT batch_file_id FROM upload_transfers WHERE id = ?)
`, localSHA1, transferID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FailUploadTransfer(ctx context.Context, transferID int64, summary string) error {
	return s.FailUploadTransferWithResult(ctx, transferID, summary, "", "")
}

func (s *Store) FailUploadTransferWithResult(ctx context.Context, transferID int64, summary string, outcome string, localSHA1 string) error {
	outcome = normalizeUploadOutcome(outcome)
	localSHA1 = strings.ToUpper(strings.TrimSpace(localSHA1))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?,
    outcome = CASE WHEN ? = '' THEN outcome ELSE ? END,
    error_summary = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, UploadTransferFailed, outcome, outcome, strings.TrimSpace(summary), transferID); err != nil {
		return err
	}
	if localSHA1 != "" {
		if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_files
SET sha1 = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = (SELECT batch_file_id FROM upload_transfers WHERE id = ?)
`, localSHA1, transferID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CompleteUploadTarget finalizes successful reconciliation. It writes an
// outbox event only when at least one transfer changed the remote namespace;
// notification delivery is intentionally owned by a future consumer.
func (s *Store) CompleteUploadTarget(ctx context.Context, targetID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var target UploadBatchTarget
	target, err = scanUploadBatchTarget(tx.QueryRowContext(ctx, uploadBatchTargetSelect+` WHERE t.id = ?`, targetID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUploadTargetNotFound
	}
	if err != nil {
		return err
	}
	var incomplete int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM upload_transfers WHERE batch_target_id = ? AND status != ?`, targetID, UploadTransferCompleted).Scan(&incomplete); err != nil {
		return err
	}
	if incomplete > 0 {
		return fmt.Errorf("upload target %d still has %d incomplete transfers", targetID, incomplete)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, error_summary = '', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, UploadTargetCompleted, targetID); err != nil {
		return err
	}
	payload, changedFiles, err := buildUploadEventPayloadTx(ctx, tx, target)
	if err != nil {
		return err
	}
	if changedFiles > 0 {
		if _, err := tx.ExecContext(ctx, `
	INSERT OR IGNORE INTO upload_events (batch_target_id, type, payload, status, available_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
`, targetID, UploadEventTargetVerified, string(payload), UploadEventPending); err != nil {
			return err
		}
		if err := enqueueUploadNotificationTx(ctx, tx, target); err != nil {
			return err
		}
	}
	if err := refreshUploadBatchStatusTx(ctx, tx, target.BatchID); err != nil {
		return err
	}
	return tx.Commit()
}

func buildUploadEventPayloadTx(ctx context.Context, tx *sql.Tx, target UploadBatchTarget) ([]byte, int, error) {
	var watchDirID sql.NullInt64
	var seriesKey, seriesPath string
	var revision int
	if err := tx.QueryRowContext(ctx, `
SELECT b.watch_dir_id, b.series_key, b.series_path, b.revision
FROM upload_batches b
WHERE b.id = ?
`, target.BatchID).Scan(&watchDirID, &seriesKey, &seriesPath, &revision); err != nil {
		return nil, 0, err
	}
	type eventFile struct {
		RelativePath string `json:"relativePath"`
		RemotePath   string `json:"remotePath"`
		FileType     string `json:"fileType"`
		Size         int64  `json:"size"`
		Outcome      string `json:"outcome"`
		SHA1         string `json:"sha1,omitempty"`
	}
	rows, err := tx.QueryContext(ctx, `
SELECT f.relative_path, t.remote_path, f.file_type, f.size, t.outcome, t.remote_sha1
FROM upload_transfers t
JOIN upload_batch_files f ON f.id = t.batch_file_id
WHERE t.batch_target_id = ? AND t.status = ?
ORDER BY f.relative_path ASC
`, target.ID, UploadTransferCompleted)
	if err != nil {
		return nil, 0, err
	}
	files := make([]eventFile, 0)
	for rows.Next() {
		var file eventFile
		if err := rows.Scan(&file.RelativePath, &file.RemotePath, &file.FileType, &file.Size, &file.Outcome, &file.SHA1); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if !UploadOutcomeChangesRemote(file.Outcome) {
			continue
		}
		files = append(files, file)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	var watchID any
	if watchDirID.Valid {
		watchID = watchDirID.Int64
	}
	payload, err := json.Marshal(map[string]any{
		"eventKey":      fmt.Sprintf("upload:%d:%d:%s", target.BatchID, target.ID, UploadEventTargetVerified),
		"remoteChanged": true,
		"batchId":       target.BatchID,
		"revision":      revision,
		"targetId":      target.ID,
		"providerId":    target.ProviderID,
		"provider":      target.ProviderName,
		"providerType":  target.ProviderType,
		"remoteRoot":    target.RemoteRoot,
		"watchDirId":    watchID,
		"seriesKey":     seriesKey,
		"seriesPath":    seriesPath,
		"files":         files,
	})
	return payload, len(files), err
}

func (s *Store) RetryUploadTarget(ctx context.Context, targetID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID int64
	var retryable int
	err = tx.QueryRowContext(ctx, `SELECT batch_id, retryable FROM upload_batch_targets WHERE id = ?`, targetID).Scan(&batchID, &retryable)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUploadTargetNotFound
	}
	if err != nil {
		return err
	}
	if retryable == 0 {
		return ErrUploadTargetNotRetryable
	}
	result, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, attempts = 0, error_summary = '', available_at = ?, started_at = NULL, finished_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status IN (?, ?)
`, UploadTargetPending, formatStoreTime(time.Now().UTC()), targetID, UploadTargetFailed, UploadTargetCanceled)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUploadTargetNotRetryable
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?, error_summary = '', started_at = NULL, finished_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE batch_target_id = ? AND status IN (?, ?)
`, UploadTransferPending, targetID, UploadTransferFailed, UploadTransferCanceled); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE upload_batches SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status IN (?, ?, ?)`, UploadBatchPending, batchID, UploadBatchFailed, UploadBatchPartial, UploadBatchCanceled); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelUploadTarget(ctx context.Context, targetID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID int64
	err = tx.QueryRowContext(ctx, `SELECT batch_id FROM upload_batch_targets WHERE id = ?`, targetID).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUploadTargetNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, error_summary = '已取消', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status IN (?, ?)
`, UploadTargetCanceled, targetID, UploadTargetWaiting, UploadTargetPending); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?, error_summary = '已取消', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE batch_target_id = ? AND status IN (?, ?)
`, UploadTransferCanceled, targetID, UploadTransferPending, UploadTransferFailed); err != nil {
		return err
	}
	if err := refreshUploadBatchStatusTx(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RescheduleUploadTarget(ctx context.Context, targetID int64, summary string, availableAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, error_summary = ?, available_at = ?, started_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, UploadTargetPending, strings.TrimSpace(summary), formatStoreTime(availableAt.UTC()), targetID, UploadTargetRunning)
	return err
}

func (s *Store) FailUploadTarget(ctx context.Context, targetID int64, summary string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID int64
	err = tx.QueryRowContext(ctx, `SELECT batch_id FROM upload_batch_targets WHERE id = ?`, targetID).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUploadTargetNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, error_summary = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, UploadTargetFailed, strings.TrimSpace(summary), targetID); err != nil {
		return err
	}
	if err := refreshUploadBatchStatusTx(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUploadBatch(ctx context.Context, id int64) (UploadBatch, error) {
	batch, err := scanUploadBatch(s.db.QueryRowContext(ctx, uploadBatchSelect+` WHERE b.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadBatch{}, ErrUploadBatchNotFound
	}
	return batch, err
}

func (s *Store) GetUploadBatchDetail(ctx context.Context, id int64) (UploadBatchDetail, error) {
	batch, err := s.GetUploadBatch(ctx, id)
	if err != nil {
		return UploadBatchDetail{}, err
	}
	files, err := s.listUploadBatchFiles(ctx, id)
	if err != nil {
		return UploadBatchDetail{}, err
	}
	targets, err := s.listUploadBatchTargets(ctx, id)
	if err != nil {
		return UploadBatchDetail{}, err
	}
	transfers, err := s.listUploadBatchTransfers(ctx, id)
	if err != nil {
		return UploadBatchDetail{}, err
	}
	return UploadBatchDetail{Batch: batch, Files: files, Targets: targets, Transfers: transfers}, nil
}

func (s *Store) ListUploadBatches(ctx context.Context, filters UploadBatchFilters) (UploadBatchListResult, error) {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 200 {
		filters.PageSize = 50
	}
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if status := strings.TrimSpace(filters.Status); status != "" && status != "all" {
		clauses = append(clauses, "b.status = ?")
		args = append(args, status)
	}
	if path := strings.TrimSpace(filters.Path); path != "" {
		clauses = append(clauses, "b.series_path LIKE ?")
		args = append(args, "%"+path+"%")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upload_batches b`+where, args...).Scan(&total); err != nil {
		return UploadBatchListResult{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, filters.PageSize, (filters.Page-1)*filters.PageSize)
	rows, err := s.db.QueryContext(ctx, uploadBatchSelect+where+` ORDER BY b.id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return UploadBatchListResult{}, err
	}
	defer rows.Close()
	items := make([]UploadBatch, 0)
	for rows.Next() {
		batch, err := scanUploadBatch(rows)
		if err != nil {
			return UploadBatchListResult{}, err
		}
		items = append(items, batch)
	}
	if err := rows.Err(); err != nil {
		return UploadBatchListResult{}, err
	}
	return UploadBatchListResult{Items: items, Total: total, Page: filters.Page, PageSize: filters.PageSize}, nil
}

func (s *Store) GetUploadSummary(ctx context.Context) (UploadSummary, error) {
	var summary UploadSummary
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM upload_batches GROUP BY status`)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return summary, err
		}
		switch status {
		case UploadBatchCollecting:
			summary.Collecting = count
		case UploadBatchPending:
			summary.Pending = count
		case UploadBatchRunning:
			summary.Running = count
		case UploadBatchCompleted:
			summary.Completed = count
		case UploadBatchFailed, UploadBatchPartial:
			summary.Failed += count
		}
	}
	return summary, rows.Err()
}

func (s *Store) ListUploadEvents(ctx context.Context, status string, limit int) ([]UploadEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := uploadEventSelect
	args := []any{}
	if strings.TrimSpace(status) != "" && status != "all" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]UploadEvent, 0)
	for rows.Next() {
		var event UploadEvent
		if err := scanUploadEvent(rows, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// ClaimUploadEvents leases outbox events to a notification consumer. The
// lease makes a crashed consumer recoverable without allowing two consumers
// to send the same event concurrently.
func (s *Store) ClaimUploadEvents(ctx context.Context, limit int, leaseDuration time.Duration) ([]UploadEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	nowValue := formatStoreTime(now)
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_events
SET status = ?, lease_id = '', lease_until = '', available_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE status = ? AND lease_until <> '' AND lease_until <= ?
`, UploadEventPending, nowValue, UploadEventProcessing, nowValue); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, uploadEventSelect+` WHERE status = ? AND available_at <= ? ORDER BY id ASC LIMIT ?`, UploadEventPending, nowValue, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var event UploadEvent
		if err := scanUploadEvent(rows, &event); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, event.ID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	claimed := make([]UploadEvent, 0, len(ids))
	leaseUntil := formatStoreTime(now.Add(leaseDuration))
	for _, id := range ids {
		leaseID := newUploadLeaseID()
		result, err := tx.ExecContext(ctx, `
UPDATE upload_events
SET status = ?, attempts = attempts + 1, lease_id = ?, lease_until = ?, error_summary = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, UploadEventProcessing, leaseID, leaseUntil, id, UploadEventPending)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		var event UploadEvent
		if err := scanUploadEvent(tx.QueryRowContext(ctx, uploadEventSelect+` WHERE id = ?`, id), &event); err != nil {
			return nil, err
		}
		event.LeaseID = leaseID
		claimed = append(claimed, event)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) AckUploadEvent(ctx context.Context, eventID int64, leaseID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE upload_events
SET status = ?, delivered_at = CURRENT_TIMESTAMP, lease_id = '', lease_until = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ? AND lease_id = ?
`, UploadEventDelivered, eventID, UploadEventProcessing, strings.TrimSpace(leaseID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM upload_events WHERE id = ?`, eventID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("upload event not found")
		}
		if err == nil && status == UploadEventDelivered {
			return nil
		}
		return errors.New("upload event lease is invalid")
	}
	return nil
}

func (s *Store) FailUploadEvent(ctx context.Context, eventID int64, leaseID string, summary string, retryAt time.Time) error {
	status := UploadEventFailed
	availableAt := formatStoreTime(time.Now().UTC())
	if !retryAt.IsZero() {
		status = UploadEventPending
		availableAt = formatStoreTime(retryAt.UTC())
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE upload_events
SET status = ?, available_at = ?, lease_id = '', lease_until = '', error_summary = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ? AND lease_id = ?
`, status, availableAt, strings.TrimSpace(summary), eventID, UploadEventProcessing, strings.TrimSpace(leaseID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("upload event lease is invalid")
	}
	return nil
}

const uploadEventSelect = `
SELECT id, batch_target_id, type, payload, status, attempts, available_at, lease_id, lease_until,
       error_summary, created_at, COALESCE(delivered_at, '')
FROM upload_events`

type uploadEventScanner interface {
	Scan(dest ...any) error
}

func scanUploadEvent(scanner uploadEventScanner, event *UploadEvent) error {
	return scanner.Scan(&event.ID, &event.BatchTargetID, &event.Type, &event.Payload, &event.Status, &event.Attempts, &event.AvailableAt, &event.LeaseID, &event.LeaseUntil, &event.ErrorSummary, &event.CreatedAt, &event.DeliveredAt)
}

func newUploadLeaseID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return "lease-" + hex.EncodeToString(buf)
	}
	return fmt.Sprintf("lease-%d", time.Now().UTC().UnixNano())
}

func (s *Store) ResetRunningUploadWork(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_transfers
SET status = ?, started_at = NULL, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadTransferPending, UploadTransferRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batch_targets
SET status = ?, started_at = NULL, available_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadTargetPending, UploadTargetRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_batches
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadBatchPending, UploadBatchRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_events
SET status = ?, lease_id = '', lease_until = '', available_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadEventPending, UploadEventProcessing); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE upload_notifications
SET status = ?, available_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadNotificationPending, UploadNotificationProcessing); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CountUploadTargetsByStatuses(ctx context.Context, statuses ...string) (int, error) {
	if len(statuses) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for index, status := range statuses {
		placeholders[index] = "?"
		args[index] = status
	}
	var count int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM upload_batch_targets WHERE status IN (%s)`, strings.Join(placeholders, ",")), args...).Scan(&count)
	return count, err
}

const uploadProviderSelect = `
SELECT id, name, type, enabled, user_agent,
       EXISTS(SELECT 1 FROM upload_provider_secrets s WHERE s.provider_id = upload_providers.id AND s.secret_key = 'cookie' AND s.secret_value <> ''),
       auth_device, created_at, updated_at
FROM upload_providers`

const uploadBatchSelect = `
SELECT b.id, b.watch_dir_id, b.upload_route_id, b.series_key, b.series_path, b.status, b.revision, b.ready_at,
       (SELECT COUNT(*) FROM upload_batch_files f WHERE f.batch_id = b.id),
       COALESCE((SELECT t.provider_name FROM upload_batch_targets t WHERE t.batch_id = b.id ORDER BY t.id LIMIT 1), ''),
       COALESCE((SELECT t.remote_root FROM upload_batch_targets t WHERE t.batch_id = b.id ORDER BY t.id LIMIT 1), ''),
       (SELECT COUNT(*) FROM upload_batch_targets t WHERE t.batch_id = b.id),
       (SELECT COUNT(*) FROM upload_batch_targets t WHERE t.batch_id = b.id AND t.status = 'completed'),
       (SELECT COUNT(*) FROM upload_batch_targets t WHERE t.batch_id = b.id AND t.status = 'failed'),
       (SELECT COUNT(*)
          FROM upload_transfers tr
          JOIN upload_batch_targets t ON t.id = tr.batch_target_id
         WHERE t.batch_id = b.id),
       (SELECT COUNT(*)
          FROM upload_transfers tr
          JOIN upload_batch_targets t ON t.id = tr.batch_target_id
         WHERE t.batch_id = b.id AND tr.status = 'completed'),
       (SELECT COUNT(*)
          FROM upload_transfers tr
          JOIN upload_batch_targets t ON t.id = tr.batch_target_id
         WHERE t.batch_id = b.id AND t.status = 'failed' AND tr.status = 'failed'),
       b.created_at, b.updated_at
FROM upload_batches b`

const uploadBatchTargetSelect = `
SELECT t.id, t.batch_id, t.provider_id, t.provider_name, t.provider_type, t.remote_root, t.user_agent, t.collision_policy, t.include_types,
       t.retryable, t.notification_template_id, t.notification_variables,
       t.status, t.attempts, t.error_summary, t.available_at, COALESCE(t.started_at, ''), COALESCE(t.finished_at, ''), t.created_at, t.updated_at
FROM upload_batch_targets t
JOIN upload_batches b ON b.id = t.batch_id`

const uploadTransferSelect = `
SELECT t.id, t.batch_target_id, t.batch_file_id, f.local_path, f.relative_path, f.file_type,
       f.modified_at, t.remote_path, t.status, t.attempts, t.bytes_total, t.bytes_transferred, t.remote_id,
       t.outcome, f.sha1, t.remote_sha1, t.error_summary,
       COALESCE(t.started_at, ''), COALESCE(t.finished_at, ''), t.created_at, t.updated_at
FROM upload_transfers t
JOIN upload_batch_files f ON f.id = t.batch_file_id`

type uploadProviderScanner interface {
	Scan(dest ...any) error
}

func scanUploadProvider(scanner uploadProviderScanner) (UploadProvider, error) {
	var item UploadProvider
	var enabled int
	var hasCookie int
	err := scanner.Scan(&item.ID, &item.Name, &item.Type, &enabled, &item.UserAgent, &hasCookie, &item.AuthDevice, &item.CreatedAt, &item.UpdatedAt)
	item.Enabled = enabled == 1
	item.HasCookie = hasCookie == 1
	return item, err
}

type uploadProviderRouteScanner interface {
	Scan(dest ...any) error
}

func scanUploadProviderRoute(scanner uploadProviderRouteScanner) (UploadProviderRoute, error) {
	var item UploadProviderRoute
	var watchDirID sql.NullInt64
	var notificationTemplateID sql.NullInt64
	var enabled int
	var encodedTypes string
	var encodedVariables string
	err := scanner.Scan(
		&item.ID, &item.ProviderID, &watchDirID, &enabled, &item.RemoteRoot, &item.CollisionPolicy, &encodedTypes,
		&notificationTemplateID, &encodedVariables, &item.CreatedAt, &item.UpdatedAt,
	)
	if watchDirID.Valid {
		item.WatchDirID = &watchDirID.Int64
	}
	item.Enabled = enabled == 1
	item.RemoteRoot = normalizeRemoteRoot(item.RemoteRoot)
	item.CollisionPolicy = normalizeCollisionPolicy(item.CollisionPolicy)
	item.IncludeTypes = decodeStoredUploadTypes(encodedTypes)
	if notificationTemplateID.Valid {
		item.NotificationTemplateID = &notificationTemplateID.Int64
	}
	item.NotificationVariables = decodeNotificationVariables(encodedVariables)
	return item, err
}

type uploadBatchScanner interface {
	Scan(dest ...any) error
}

func scanUploadBatch(scanner uploadBatchScanner) (UploadBatch, error) {
	var item UploadBatch
	var watchDirID sql.NullInt64
	var uploadRouteID sql.NullInt64
	err := scanner.Scan(
		&item.ID,
		&watchDirID,
		&uploadRouteID,
		&item.SeriesKey,
		&item.SeriesPath,
		&item.Status,
		&item.Revision,
		&item.ReadyAt,
		&item.FileCount,
		&item.ProviderName,
		&item.RemoteRoot,
		&item.TargetCount,
		&item.CompletedTargets,
		&item.FailedTargets,
		&item.TransferCount,
		&item.CompletedTransfers,
		&item.FailedTransfers,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if watchDirID.Valid {
		item.WatchDirID = &watchDirID.Int64
	}
	if uploadRouteID.Valid {
		item.UploadRouteID = &uploadRouteID.Int64
	}
	return item, err
}

type uploadTargetScanner interface {
	Scan(dest ...any) error
}

func scanUploadBatchTarget(scanner uploadTargetScanner) (UploadBatchTarget, error) {
	var item UploadBatchTarget
	var encodedTypes string
	var notificationTemplateID sql.NullInt64
	var encodedVariables string
	var retryable int
	err := scanner.Scan(
		&item.ID, &item.BatchID, &item.ProviderID, &item.ProviderName, &item.ProviderType, &item.RemoteRoot,
		&item.UserAgent, &item.CollisionPolicy, &encodedTypes, &retryable, &notificationTemplateID, &encodedVariables,
		&item.Status, &item.Attempts, &item.ErrorSummary, &item.AvailableAt, &item.StartedAt, &item.FinishedAt,
		&item.CreatedAt, &item.UpdatedAt,
	)
	item.IncludeTypes = decodeStoredUploadTypes(encodedTypes)
	item.Retryable = retryable == 1
	if notificationTemplateID.Valid {
		item.NotificationTemplateID = &notificationTemplateID.Int64
	}
	item.NotificationVariables = decodeNotificationVariables(encodedVariables)
	return item, err
}

type uploadTransferScanner interface {
	Scan(dest ...any) error
}

func scanUploadTransfer(scanner uploadTransferScanner) (UploadTransfer, error) {
	var item UploadTransfer
	err := scanner.Scan(
		&item.ID,
		&item.BatchTargetID,
		&item.BatchFileID,
		&item.LocalPath,
		&item.RelativePath,
		&item.FileType,
		&item.ModifiedAt,
		&item.RemotePath,
		&item.Status,
		&item.Attempts,
		&item.BytesTotal,
		&item.BytesTransferred,
		&item.RemoteID,
		&item.Outcome,
		&item.LocalSHA1,
		&item.RemoteSHA1,
		&item.ErrorSummary,
		&item.StartedAt,
		&item.FinishedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func normalizeUploadProvider(provider *UploadProvider) error {
	provider.Name = strings.TrimSpace(provider.Name)
	provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
	provider.UserAgent = strings.TrimSpace(provider.UserAgent)
	if provider.Name == "" || provider.Type == "" {
		return errors.New("provider name and type are required")
	}
	if provider.Type != UploadProviderType115Cookie {
		provider.AuthDevice = ""
		return nil
	}
	if strings.TrimSpace(provider.AuthDevice) == "" {
		provider.AuthDevice = ""
		return nil
	}
	authDevice, err := normalizeUploadProviderAuthDevice(provider.AuthDevice)
	if err != nil {
		return err
	}
	provider.AuthDevice = authDevice
	return nil
}

func normalizeUploadProviderAuthDevice(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := uploadProviderAuthDevices[value]; !ok {
		return "", fmt.Errorf("unsupported 115 Cookie auth device %q", value)
	}
	return value, nil
}

func normalizeStoredUploadTypes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if value == "all" {
			return append([]string{}, uploadFileTypes...)
		}
		if _, ok := uploadFileTypeSet[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateStoredUploadTypes(values []string) error {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "all" {
			continue
		}
		if _, ok := uploadFileTypeSet[value]; !ok {
			return fmt.Errorf("unsupported upload file type %q", value)
		}
	}
	return nil
}

func decodeStoredUploadTypes(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return normalizeStoredUploadTypes(result)
}

func normalizeCollisionPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "skip", "fail", "replace":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "fail"
	}
}

func normalizeUploadOutcome(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case UploadOutcomeCreated, UploadOutcomeReplaced, UploadOutcomeUnchanged, UploadOutcomeSkipped:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeRemoteRoot(value string) string {
	value = strings.TrimSpace(filepath.ToSlash(value))
	if value == "" {
		return "/"
	}
	return pathpkg.Clean("/" + strings.TrimPrefix(value, "/"))
}

func joinRemotePath(root string, relative string) string {
	relative = filepath.ToSlash(strings.TrimSpace(relative))
	return pathpkg.Clean(pathpkg.Join(normalizeRemoteRoot(root), "/"+strings.TrimPrefix(relative, "/")))
}

func validateUploadCandidate(candidate UploadCandidate) error {
	if strings.TrimSpace(candidate.LocalPath) == "" || strings.TrimSpace(candidate.RelativePath) == "" || strings.TrimSpace(candidate.FileType) == "" {
		return errors.New("upload candidate is incomplete")
	}
	if candidate.Size < 0 {
		return errors.New("upload candidate has invalid size")
	}
	if candidate.ModifiedAt.IsZero() {
		return errors.New("upload candidate has no modified time")
	}
	return nil
}

type configuredUploadTarget struct {
	provider UploadProvider
	route    UploadProviderRoute
}

func listEnabledWatchDirUploadTargetsTx(ctx context.Context, tx *sql.Tx, watchDirID int64) ([]configuredUploadTarget, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT p.id, p.name, p.type, p.enabled, p.user_agent,
       EXISTS(SELECT 1 FROM upload_provider_secrets s WHERE s.provider_id = p.id AND s.secret_key = 'cookie' AND s.secret_value <> ''),
       p.auth_device, p.created_at, p.updated_at,
       r.id, r.provider_id, r.watch_dir_id, r.enabled, r.remote_root, r.collision_policy, r.include_types,
       r.notification_template_id, r.notification_variables, r.created_at, r.updated_at
FROM upload_provider_routes r
JOIN upload_providers p ON p.id = r.provider_id
WHERE r.watch_dir_id = ? AND r.enabled = 1 AND p.enabled = 1
ORDER BY r.id
`, watchDirID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]configuredUploadTarget, 0)
	for rows.Next() {
		var item configuredUploadTarget
		var providerEnabled int
		var hasCookie int
		var routeWatchDirID sql.NullInt64
		var notificationTemplateID sql.NullInt64
		var routeEnabled int
		var encodedTypes string
		var encodedVariables string
		if err := rows.Scan(
			&item.provider.ID, &item.provider.Name, &item.provider.Type, &providerEnabled, &item.provider.UserAgent,
			&hasCookie, &item.provider.AuthDevice, &item.provider.CreatedAt, &item.provider.UpdatedAt,
			&item.route.ID, &item.route.ProviderID, &routeWatchDirID, &routeEnabled, &item.route.RemoteRoot,
			&item.route.CollisionPolicy, &encodedTypes, &notificationTemplateID, &encodedVariables,
			&item.route.CreatedAt, &item.route.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.provider.Enabled = providerEnabled == 1
		item.provider.HasCookie = hasCookie == 1
		item.route.Enabled = routeEnabled == 1
		if routeWatchDirID.Valid {
			item.route.WatchDirID = &routeWatchDirID.Int64
		}
		item.route.RemoteRoot = normalizeRemoteRoot(item.route.RemoteRoot)
		item.route.CollisionPolicy = normalizeCollisionPolicy(item.route.CollisionPolicy)
		item.route.IncludeTypes = decodeStoredUploadTypes(encodedTypes)
		if notificationTemplateID.Valid {
			item.route.NotificationTemplateID = &notificationTemplateID.Int64
		}
		item.route.NotificationVariables = decodeNotificationVariables(encodedVariables)
		items = append(items, item)
	}
	return items, rows.Err()
}

func uploadTypeAllowed(includeTypes []string, fileType string) bool {
	if len(includeTypes) == 0 {
		return true
	}
	fileType = strings.ToLower(strings.TrimSpace(fileType))
	for _, value := range includeTypes {
		if strings.EqualFold(strings.TrimSpace(value), fileType) {
			return true
		}
	}
	return false
}

func hasAllowedUploadCandidate(candidates []UploadCandidate, includeTypes []string) bool {
	for _, candidate := range candidates {
		if uploadTypeAllowed(includeTypes, candidate.FileType) {
			return true
		}
	}
	return false
}

func anyUploadTargetAllows(targets []UploadBatchTarget, fileType string) bool {
	for _, target := range targets {
		if uploadTypeAllowed(target.IncludeTypes, fileType) {
			return true
		}
	}
	return false
}

func listUploadBatchTargetsTx(ctx context.Context, tx *sql.Tx, batchID int64) ([]UploadBatchTarget, error) {
	rows, err := tx.QueryContext(ctx, uploadBatchTargetSelect+` WHERE t.batch_id = ? ORDER BY t.id ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadBatchTarget, 0)
	for rows.Next() {
		item, err := scanUploadBatchTarget(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listUploadBatchFiles(ctx context.Context, batchID int64) ([]UploadBatchFile, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, batch_id, local_path, relative_path, file_type, size, modified_at, sha1, created_at, updated_at
FROM upload_batch_files
WHERE batch_id = ?
ORDER BY id ASC
`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadBatchFile, 0)
	for rows.Next() {
		var item UploadBatchFile
		if err := rows.Scan(&item.ID, &item.BatchID, &item.LocalPath, &item.RelativePath, &item.FileType, &item.Size, &item.ModifiedAt, &item.SHA1, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listUploadBatchTargets(ctx context.Context, batchID int64) ([]UploadBatchTarget, error) {
	rows, err := s.db.QueryContext(ctx, uploadBatchTargetSelect+` WHERE t.batch_id = ? ORDER BY t.id ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadBatchTarget, 0)
	for rows.Next() {
		item, err := scanUploadBatchTarget(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listUploadBatchTransfers(ctx context.Context, batchID int64) ([]UploadTransfer, error) {
	rows, err := s.db.QueryContext(ctx, uploadTransferSelect+` WHERE t.batch_target_id IN (SELECT id FROM upload_batch_targets WHERE batch_id = ?) ORDER BY t.id ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadTransfer, 0)
	for rows.Next() {
		item, err := scanUploadTransfer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func hasActiveMediaTasksForSeriesTx(ctx context.Context, tx *sql.Tx, seriesPath string) (bool, error) {
	clean := filepath.Clean(seriesPath)
	prefix := escapeSQLiteLike(clean+string(os.PathSeparator)) + "%"
	var exists int
	err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM tasks t
  JOIN media_files m ON m.id = t.media_file_id
  WHERE t.type = 'media_process'
    AND t.status IN ('pending', 'running')
    AND (m.path = ? OR m.path LIKE ? ESCAPE '\')
)
`, clean, prefix).Scan(&exists)
	return exists == 1, err
}

func escapeSQLiteLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func refreshUploadBatchStatusTx(ctx context.Context, tx *sql.Tx, batchID int64) error {
	var total, waiting, pending, running, completed, failed, canceled int
	err := tx.QueryRowContext(ctx, `
	SELECT COUNT(*),
	       COALESCE(SUM(CASE WHEN status = 'waiting' THEN 1 ELSE 0 END), 0),
	       COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
	       COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0),
	       COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
	       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
	       COALESCE(SUM(CASE WHEN status = 'canceled' THEN 1 ELSE 0 END), 0)
FROM upload_batch_targets
WHERE batch_id = ?
`, batchID).Scan(&total, &waiting, &pending, &running, &completed, &failed, &canceled)
	if err != nil {
		return err
	}
	if waiting > 0 {
		return nil
	}
	status := UploadBatchPending
	switch {
	case total == 0:
		status = UploadBatchFailed
	case completed == total:
		status = UploadBatchCompleted
	case running > 0:
		status = UploadBatchRunning
	case pending > 0:
		status = UploadBatchPending
	case failed > 0 && completed > 0:
		status = UploadBatchPartial
	case failed > 0:
		status = UploadBatchFailed
	case canceled > 0 && completed > 0:
		status = UploadBatchPartial
	case canceled > 0:
		status = UploadBatchCanceled
	}
	_, err = tx.ExecContext(ctx, `UPDATE upload_batches SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, batchID)
	return err
}
