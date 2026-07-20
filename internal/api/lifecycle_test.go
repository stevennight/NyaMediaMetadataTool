package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

func TestServerCloseDrainsInFlightRequestsAndRejectsNewAPIRequests(t *testing.T) {
	server := newLifecycleTestServer(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	server.mux.HandleFunc("POST /api/test/block", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/test/block", nil))
		close(requestDone)
	}()
	waitForSignal(t, started, "request handler did not start")

	activity := server.Activity()
	if activity.InFlight != 1 || activity.ActiveMutations != 1 {
		t.Fatalf("unexpected activity while request is blocked: %+v", activity)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close(closeCtx) }()
	waitForClosing(t, server)

	rejected := httptest.NewRecorder()
	server.ServeHTTP(rejected, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatalf("new API request status = %d, want %d", rejected.Code, http.StatusServiceUnavailable)
	}
	if rejected.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", rejected.Header().Get("Retry-After"))
	}

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before the accepted request completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	waitForSignal(t, requestDone, "request handler did not finish")
	if response.Code != http.StatusNoContent {
		t.Fatalf("accepted request status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}

	activity = server.Activity()
	if activity.InFlight != 0 || activity.ActiveMutations != 0 || !activity.Closing {
		t.Fatalf("unexpected activity after close: %+v", activity)
	}
}

func TestServerCloseDrainsBackgroundAndPreventsNewBackgroundWork(t *testing.T) {
	server := newLifecycleTestServer(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	if !server.runBackground(func(context.Context) {
		close(started)
		<-release
	}) {
		t.Fatal("initial background work was rejected")
	}
	waitForSignal(t, started, "background work did not start")

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close(closeCtx) }()
	waitForClosing(t, server)

	if server.runBackground(func(context.Context) {}) {
		t.Fatal("background work was accepted after closing began")
	}
	if activity := server.Activity(); activity.Background != 1 {
		t.Fatalf("background count = %d, want 1", activity.Background)
	}

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before background work completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
	if activity := server.Activity(); activity.Background != 0 {
		t.Fatalf("background count after close = %d, want 0", activity.Background)
	}
}

func TestServerRequestsInheritServiceCancellation(t *testing.T) {
	serviceCtx, cancelService := context.WithCancel(context.Background())
	server := newLifecycleTestServer(serviceCtx)
	started := make(chan struct{})
	requestErr := make(chan error, 1)
	server.mux.HandleFunc("DELETE /api/test/cancel", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		requestErr <- r.Context().Err()
		w.WriteHeader(http.StatusNoContent)
	})

	requestDone := make(chan struct{})
	go func() {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/api/test/cancel", nil))
		close(requestDone)
	}()
	waitForSignal(t, started, "request handler did not start")

	server.BeginClose()
	cancelService()
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := server.Close(closeCtx); err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
	waitForSignal(t, requestDone, "canceled request did not finish")
	if err := <-requestErr; err != context.Canceled {
		t.Fatalf("request context error = %v, want %v", err, context.Canceled)
	}
}

func TestServerCloseCanRaceWithConcurrentRequests(t *testing.T) {
	server := newLifecycleTestServer(context.Background())
	release := make(chan struct{})
	server.mux.HandleFunc("PUT /api/test/race", func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	})

	const requestCount = 64
	start := make(chan struct{})
	responses := make([]*httptest.ResponseRecorder, requestCount)
	var requests sync.WaitGroup
	requests.Add(requestCount)
	for i := range requestCount {
		responses[i] = httptest.NewRecorder()
		go func(index int) {
			defer requests.Done()
			<-start
			server.ServeHTTP(responses[index], httptest.NewRequest(http.MethodPut, "/api/test/race", nil))
		}(i)
	}

	close(start)
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- server.Close(closeCtx) }()
	waitForClosing(t, server)
	close(release)
	requests.Wait()
	if err := <-closeDone; err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}

	for i, response := range responses {
		if response.Code != http.StatusNoContent && response.Code != http.StatusServiceUnavailable {
			t.Fatalf("request %d status = %d, want %d or %d", i, response.Code, http.StatusNoContent, http.StatusServiceUnavailable)
		}
	}
	if activity := server.Activity(); activity.InFlight != 0 || activity.ActiveMutations != 0 {
		t.Fatalf("activity was not drained: %+v", activity)
	}
}

func newLifecycleTestServer(serviceCtx context.Context) *Server {
	return newServer(serviceCtx, config.Default(), "", nil, nil, nil, slog.Default())
}

func waitForClosing(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !server.Activity().Closing {
		if time.Now().After(deadline) {
			t.Fatal("server did not enter closing state")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
