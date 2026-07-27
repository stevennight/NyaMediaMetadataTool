package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	UploadNotificationPending    = "pending"
	UploadNotificationProcessing = "processing"
	UploadNotificationDelivered  = "delivered"
	UploadNotificationFailed     = "failed"
)

var (
	ErrUploadNotificationTemplateNotFound = errors.New("upload notification template not found")
	ErrUploadNotificationTemplateInUse    = errors.New("upload notification template is in use")
	ErrNoPendingUploadNotification        = errors.New("no pending upload notification")
	notificationVariablePattern           = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	notificationVariableNamePattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type UploadNotificationTemplate struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	HeadersTemplate string `json:"headersTemplate"`
	PayloadTemplate string `json:"payloadTemplate"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type UploadNotification struct {
	ID             int64  `json:"id"`
	BatchTargetID  int64  `json:"batchTargetId"`
	TemplateID     int64  `json:"templateId"`
	TemplateName   string `json:"templateName"`
	URL            string `json:"url"`
	Headers        string `json:"headers"`
	Payload        string `json:"payload"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	AvailableAt    string `json:"availableAt"`
	ResponseStatus int    `json:"responseStatus"`
	ErrorSummary   string `json:"errorSummary"`
	CreatedAt      string `json:"createdAt"`
	DeliveredAt    string `json:"deliveredAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func (s *Store) ListUploadNotificationTemplates(ctx context.Context) ([]UploadNotificationTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, url, headers_template, payload_template, created_at, updated_at
FROM upload_notification_templates
ORDER BY name, id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UploadNotificationTemplate, 0)
	for rows.Next() {
		item, err := scanUploadNotificationTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetUploadNotificationTemplate(ctx context.Context, id int64) (UploadNotificationTemplate, error) {
	item, err := scanUploadNotificationTemplate(s.db.QueryRowContext(ctx, `
SELECT id, name, url, headers_template, payload_template, created_at, updated_at
FROM upload_notification_templates
WHERE id = ?
`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadNotificationTemplate{}, ErrUploadNotificationTemplateNotFound
	}
	return item, err
}

func (s *Store) CreateUploadNotificationTemplate(ctx context.Context, item UploadNotificationTemplate) (UploadNotificationTemplate, error) {
	if err := validateUploadNotificationTemplate(item); err != nil {
		return UploadNotificationTemplate{}, err
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO upload_notification_templates (name, url, headers_template, payload_template, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
`, strings.TrimSpace(item.Name), strings.TrimSpace(item.URL), normalizeHeadersJSON(item.HeadersTemplate), normalizeJSON(item.PayloadTemplate))
	if err != nil {
		return UploadNotificationTemplate{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UploadNotificationTemplate{}, err
	}
	return s.GetUploadNotificationTemplate(ctx, id)
}

func (s *Store) UpdateUploadNotificationTemplate(ctx context.Context, item UploadNotificationTemplate) (UploadNotificationTemplate, error) {
	if item.ID <= 0 {
		return UploadNotificationTemplate{}, ErrUploadNotificationTemplateNotFound
	}
	if err := validateUploadNotificationTemplate(item); err != nil {
		return UploadNotificationTemplate{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadNotificationTemplate{}, err
	}
	defer tx.Rollback()
	if err := syncTemplateRouteVariablesTx(ctx, tx, item.ID, item.HeadersTemplate, item.PayloadTemplate); err != nil {
		return UploadNotificationTemplate{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE upload_notification_templates
SET name = ?, url = ?, headers_template = ?, payload_template = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, strings.TrimSpace(item.Name), strings.TrimSpace(item.URL), normalizeHeadersJSON(item.HeadersTemplate), normalizeJSON(item.PayloadTemplate), item.ID)
	if err != nil {
		return UploadNotificationTemplate{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UploadNotificationTemplate{}, err
	}
	if affected == 0 {
		return UploadNotificationTemplate{}, ErrUploadNotificationTemplateNotFound
	}
	if err := tx.Commit(); err != nil {
		return UploadNotificationTemplate{}, err
	}
	return s.GetUploadNotificationTemplate(ctx, item.ID)
}

func (s *Store) DeleteUploadNotificationTemplate(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var references int
	if err := tx.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM upload_provider_routes WHERE notification_template_id = ?) +
  (SELECT COUNT(*) FROM upload_batch_targets WHERE notification_template_id = ? AND status IN (?, ?, ?))
`, id, id, UploadTargetWaiting, UploadTargetPending, UploadTargetRunning).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrUploadNotificationTemplateInUse
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM upload_notification_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUploadNotificationTemplateNotFound
	}
	return tx.Commit()
}

func validateUploadNotificationTemplate(item UploadNotificationTemplate) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("notification template name is required")
	}
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(item.URL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return errors.New("notification template URL must be an absolute HTTP or HTTPS URL")
	}
	var payload any
	if err := json.Unmarshal([]byte(item.PayloadTemplate), &payload); err != nil {
		return fmt.Errorf("notification payload must be valid JSON: %w", err)
	}
	if _, ok := payload.(map[string]any); !ok {
		return errors.New("notification payload must be a JSON object")
	}
	if err := validateNotificationHeadersTemplate(item.HeadersTemplate); err != nil {
		return err
	}
	return nil
}

func validateNotificationHeadersTemplate(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var headers map[string]any
	if err := json.Unmarshal([]byte(value), &headers); err != nil {
		return fmt.Errorf("notification headers must be a valid JSON object: %w", err)
	}
	if headers == nil {
		return errors.New("notification headers must be a JSON object")
	}
	headerNamePattern := regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")
	for name, value := range headers {
		if !headerNamePattern.MatchString(name) {
			return fmt.Errorf("invalid notification header name %q", name)
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("notification header %q must have a string value", name)
		}
		if strings.ContainsAny(text, "\r\n") {
			return fmt.Errorf("notification header %q contains a line break", name)
		}
	}
	return nil
}

func normalizeJSON(value string) string {
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return strings.TrimSpace(value)
	}
	encoded, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return strings.TrimSpace(value)
	}
	return string(encoded)
}

func normalizeHeadersJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return normalizeJSON(value)
}

func encodeNotificationVariables(variables map[string]string) (string, error) {
	if variables == nil {
		variables = map[string]string{}
	}
	normalized := make(map[string]string, len(variables))
	for key, value := range variables {
		key = strings.TrimSpace(key)
		if !notificationVariableNamePattern.MatchString(key) || key == "path" {
			if key == "path" {
				return "", errors.New("notification variable path is built in and cannot be overridden")
			}
			return "", fmt.Errorf("invalid notification variable name %q", key)
		}
		normalized[key] = value
	}
	encoded, err := json.Marshal(normalized)
	return string(encoded), err
}

func decodeNotificationVariables(value string) map[string]string {
	result := map[string]string{}
	if strings.TrimSpace(value) != "" {
		_ = json.Unmarshal([]byte(value), &result)
	}
	return result
}

func validateNotificationTemplateVariables(headersTemplate string, payloadTemplate string, variables map[string]string) error {
	available := make(map[string]struct{}, len(variables)+1)
	available["path"] = struct{}{}
	for key := range variables {
		available[key] = struct{}{}
	}
	missing := make(map[string]struct{})
	for _, template := range []string{headersTemplate, payloadTemplate} {
		for _, match := range notificationVariablePattern.FindAllStringSubmatch(template, -1) {
			if _, ok := available[match[1]]; !ok {
				missing[match[1]] = struct{}{}
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("notification variables are missing: %s", strings.Join(names, ", "))
}

func notificationTemplateVariableNames(headersTemplate string, payloadTemplate string) []string {
	names := map[string]struct{}{}
	for _, template := range []string{headersTemplate, payloadTemplate} {
		for _, match := range notificationVariablePattern.FindAllStringSubmatch(template, -1) {
			if match[1] != "path" {
				names[match[1]] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func syncTemplateRouteVariablesTx(ctx context.Context, tx *sql.Tx, templateID int64, headersTemplate string, payloadTemplate string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, notification_variables
FROM upload_provider_routes
WHERE notification_template_id = ?
`, templateID)
	if err != nil {
		return err
	}
	type routeUpdate struct {
		id        int64
		variables string
	}
	required := notificationTemplateVariableNames(headersTemplate, payloadTemplate)
	var updates []routeUpdate
	for rows.Next() {
		var id int64
		var encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			rows.Close()
			return err
		}
		variables := decodeNotificationVariables(encoded)
		changed := false
		for _, name := range required {
			if _, ok := variables[name]; !ok {
				variables[name] = ""
				changed = true
			}
		}
		if !changed {
			continue
		}
		encoded, err = encodeNotificationVariables(variables)
		if err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, routeUpdate{id: id, variables: encoded})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `
UPDATE upload_provider_routes
SET notification_variables = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, update.variables, update.id); err != nil {
			return err
		}
	}
	return nil
}

func renderNotificationJSON(template string, variables map[string]string) (string, error) {
	var value any
	if err := json.Unmarshal([]byte(template), &value); err != nil {
		return "", err
	}
	var replace func(any) any
	replace = func(value any) any {
		switch typed := value.(type) {
		case string:
			return notificationVariablePattern.ReplaceAllStringFunc(typed, func(token string) string {
				match := notificationVariablePattern.FindStringSubmatch(token)
				return variables[match[1]]
			})
		case []any:
			for index := range typed {
				typed[index] = replace(typed[index])
			}
			return typed
		case map[string]any:
			for key, child := range typed {
				typed[key] = replace(child)
			}
			return typed
		default:
			return typed
		}
	}
	encoded, err := json.Marshal(replace(value))
	return string(encoded), err
}

func renderNotificationTemplates(headersTemplate string, payloadTemplate string, variables map[string]string) (string, string, error) {
	if err := validateNotificationTemplateVariables(headersTemplate, payloadTemplate, variables); err != nil {
		return "", "", err
	}
	headers, err := renderNotificationJSON(normalizeHeadersJSON(headersTemplate), variables)
	if err != nil {
		return "", "", err
	}
	payload, err := renderNotificationJSON(payloadTemplate, variables)
	if err != nil {
		return "", "", err
	}
	return headers, payload, nil
}

func enqueueUploadNotificationTx(ctx context.Context, tx *sql.Tx, target UploadBatchTarget) error {
	if target.NotificationTemplateID == nil || *target.NotificationTemplateID <= 0 {
		return nil
	}
	template, err := scanUploadNotificationTemplate(tx.QueryRowContext(ctx, `
SELECT id, name, url, headers_template, payload_template, created_at, updated_at
FROM upload_notification_templates
WHERE id = ?
`, *target.NotificationTemplateID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	remoteSeriesPath, err := uploadNotificationSeriesPathTx(ctx, tx, target)
	if err != nil {
		return err
	}
	variables := make(map[string]string, len(target.NotificationVariables)+1)
	for key, value := range target.NotificationVariables {
		variables[key] = value
	}
	variables["path"] = remoteSeriesPath
	headers, payload, renderErr := renderNotificationTemplates(template.HeadersTemplate, template.PayloadTemplate, variables)
	status := UploadNotificationPending
	errorSummary := ""
	if renderErr != nil {
		status = UploadNotificationFailed
		errorSummary = renderErr.Error()
		headers = normalizeHeadersJSON(template.HeadersTemplate)
		payload = template.PayloadTemplate
	}
	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO upload_notifications
  (batch_target_id, template_id, template_name, url, headers, payload, status, available_at, error_summary, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP)
`, target.ID, template.ID, template.Name, template.URL, headers, payload, status, errorSummary)
	return err
}

func uploadNotificationSeriesPathTx(ctx context.Context, tx *sql.Tx, target UploadBatchTarget) (string, error) {
	var seriesPath, watchRoot string
	if err := tx.QueryRowContext(ctx, `
SELECT b.series_path, COALESCE(w.path, '')
FROM upload_batches b
LEFT JOIN watch_dirs w ON w.id = b.watch_dir_id
WHERE b.id = ?
`, target.BatchID).Scan(&seriesPath, &watchRoot); err != nil {
		return "", err
	}
	relative := ""
	if watchRoot != "" {
		value, err := filepath.Rel(watchRoot, seriesPath)
		if err != nil {
			return "", err
		}
		if value != "." && value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator)) {
			relative = filepath.ToSlash(value)
		}
	}
	return pathpkg.Clean("/" + strings.TrimPrefix(pathpkg.Join(target.RemoteRoot, relative), "/")), nil
}

func (s *Store) ClaimNextUploadNotification(ctx context.Context) (UploadNotification, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UploadNotification{}, err
	}
	defer tx.Rollback()
	item, err := scanUploadNotification(tx.QueryRowContext(ctx, uploadNotificationSelect+`
WHERE status = ? AND available_at <= ?
ORDER BY id
LIMIT 1
`, UploadNotificationPending, formatStoreTime(time.Now().UTC())))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadNotification{}, ErrNoPendingUploadNotification
	}
	if err != nil {
		return UploadNotification{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE upload_notifications
SET status = ?, attempts = attempts + 1, error_summary = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, UploadNotificationProcessing, item.ID, UploadNotificationPending)
	if err != nil {
		return UploadNotification{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UploadNotification{}, err
	}
	if affected == 0 {
		return UploadNotification{}, ErrNoPendingUploadNotification
	}
	if err := tx.Commit(); err != nil {
		return UploadNotification{}, err
	}
	item.Status = UploadNotificationProcessing
	item.Attempts++
	return item, nil
}

func (s *Store) CompleteUploadNotification(ctx context.Context, id int64, responseStatus int) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_notifications
SET status = ?, response_status = ?, error_summary = '', delivered_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, UploadNotificationDelivered, responseStatus, id, UploadNotificationProcessing)
	return err
}

func (s *Store) FailUploadNotification(ctx context.Context, id int64, responseStatus int, summary string, retryAt time.Time) error {
	status := UploadNotificationFailed
	availableAt := formatStoreTime(time.Now().UTC())
	if !retryAt.IsZero() {
		status = UploadNotificationPending
		availableAt = formatStoreTime(retryAt.UTC())
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_notifications
SET status = ?, response_status = ?, error_summary = ?, available_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?
`, status, responseStatus, strings.TrimSpace(summary), availableAt, id, UploadNotificationProcessing)
	return err
}

func (s *Store) ResetProcessingUploadNotifications(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE upload_notifications
SET status = ?, available_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE status = ?
`, UploadNotificationPending, UploadNotificationProcessing)
	return err
}

const uploadNotificationSelect = `
SELECT id, batch_target_id, template_id, template_name, url, headers, payload, status, attempts,
       available_at, response_status, error_summary, created_at, COALESCE(delivered_at, ''), updated_at
FROM upload_notifications
`

type notificationTemplateScanner interface {
	Scan(dest ...any) error
}

func scanUploadNotificationTemplate(scanner notificationTemplateScanner) (UploadNotificationTemplate, error) {
	var item UploadNotificationTemplate
	err := scanner.Scan(&item.ID, &item.Name, &item.URL, &item.HeadersTemplate, &item.PayloadTemplate, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

type notificationScanner interface {
	Scan(dest ...any) error
}

func scanUploadNotification(scanner notificationScanner) (UploadNotification, error) {
	var item UploadNotification
	err := scanner.Scan(
		&item.ID, &item.BatchTargetID, &item.TemplateID, &item.TemplateName, &item.URL, &item.Headers, &item.Payload,
		&item.Status, &item.Attempts, &item.AvailableAt, &item.ResponseStatus, &item.ErrorSummary,
		&item.CreatedAt, &item.DeliveredAt, &item.UpdatedAt,
	)
	return item, err
}
