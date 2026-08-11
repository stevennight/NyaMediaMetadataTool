package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

const notificationMaxAttempts = 5

func (m *Manager) notificationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		notification, err := m.store.ClaimNextUploadNotification(ctx)
		if errors.Is(err, store.ErrNoPendingUploadNotification) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				m.logger.Warn("claim upload notification failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		responseStatus, err := m.deliverNotification(ctx, notification)
		if err == nil {
			if completeErr := m.store.CompleteUploadNotification(ctx, notification.ID, responseStatus); completeErr != nil {
				m.logger.Warn("complete upload notification failed", "notificationID", notification.ID, "error", completeErr)
			} else {
				m.logger.Info("upload completion notification delivered",
					"notificationID", notification.ID,
					"batchTargetID", notification.BatchTargetID,
					"template", notification.TemplateName,
					"attempt", notification.Attempts,
					"responseStatus", responseStatus,
				)
			}
			continue
		}
		retryAt := time.Time{}
		if notification.Attempts < notificationMaxAttempts {
			retryAt = time.Now().Add(retryDelay(notification.Attempts))
		}
		if failErr := m.store.FailUploadNotification(ctx, notification.ID, responseStatus, err.Error(), retryAt); failErr != nil {
			m.logger.Warn("record upload notification failure failed", "notificationID", notification.ID, "error", failErr)
		}
		m.logger.Warn("upload completion notification failed",
			"notificationID", notification.ID,
			"batchTargetID", notification.BatchTargetID,
			"template", notification.TemplateName,
			"attempt", notification.Attempts,
			"responseStatus", responseStatus,
			"willRetry", !retryAt.IsZero(),
			"error", err,
		)
	}
}

func (m *Manager) deliverNotification(ctx context.Context, notification store.UploadNotification) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, notification.URL, bytes.NewBufferString(notification.Payload))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	headers := map[string]string{}
	if strings.TrimSpace(notification.Headers) != "" {
		if err := json.Unmarshal([]byte(notification.Headers), &headers); err != nil {
			return 0, fmt.Errorf("decode notification headers: %w", err)
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := m.notificationHTTP.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		return response.StatusCode, nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = response.Status
	}
	return response.StatusCode, fmt.Errorf("notification endpoint returned HTTP %d: %s", response.StatusCode, detail)
}
