package upload

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"NyaMediaMetadataTool/internal/store"
)

func TestDeliverUploadNotificationPostsJSON(t *testing.T) {
	var method, contentType, payload string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		payload = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer endpoint.Close()

	manager := NewWithFactory(Options{}, nil, slog.Default(), nil)
	manager.notificationHTTP = endpoint.Client()
	status, err := manager.deliverNotification(context.Background(), store.UploadNotification{
		URL:     endpoint.URL,
		Payload: `{"source_path":"/影视/番剧/示例番剧"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusAccepted || method != http.MethodPost || contentType != "application/json" ||
		!strings.Contains(payload, "source_path") {
		t.Fatalf("status=%d method=%s contentType=%s payload=%s", status, method, contentType, payload)
	}
}

func TestDeliverUploadNotificationRejectsNonSuccessResponse(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer endpoint.Close()
	manager := NewWithFactory(Options{}, nil, slog.Default(), nil)
	manager.notificationHTTP = endpoint.Client()
	status, err := manager.deliverNotification(context.Background(), store.UploadNotification{URL: endpoint.URL, Payload: `{}`})
	if status != http.StatusServiceUnavailable || err == nil || !strings.Contains(err.Error(), "temporary failure") {
		t.Fatalf("status=%d err=%v", status, err)
	}
}
