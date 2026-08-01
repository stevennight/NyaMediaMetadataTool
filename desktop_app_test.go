package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"NyaMediaMetadataTool/internal/appcore"
	"NyaMediaMetadataTool/internal/appdata"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/renamer"

	"github.com/wailsapp/wails/v2/pkg/options"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer unavailable")
}

func TestPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "series", "season")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if !pathWithinRoot(child, root) {
		t.Fatalf("expected %q to be inside %q", child, root)
	}
	if pathWithinRoot(outside, root) {
		t.Fatalf("expected %q to be outside %q", outside, root)
	}
}

func TestFitDesktopWindowSize(t *testing.T) {
	tests := []struct {
		name         string
		screenWidth  int
		screenHeight int
		wantWidth    int
		wantHeight   int
		wantAdjusted bool
	}{
		{name: "regular desktop", screenWidth: 1920, screenHeight: 1080, wantWidth: 1180, wantHeight: 720},
		{name: "Surface Go high scaling", screenWidth: 960, screenHeight: 640, wantWidth: 912, wantHeight: 560, wantAdjusted: true},
		{name: "minimum usable window", screenWidth: 780, screenHeight: 540, wantWidth: 760, wantHeight: 480, wantAdjusted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height, adjusted := fitDesktopWindowSize(test.screenWidth, test.screenHeight)
			if width != test.wantWidth || height != test.wantHeight || adjusted != test.wantAdjusted {
				t.Fatalf("fitDesktopWindowSize(%d, %d) = (%d, %d, %t), want (%d, %d, %t)", test.screenWidth, test.screenHeight, width, height, adjusted, test.wantWidth, test.wantHeight, test.wantAdjusted)
			}
		})
	}
}

func TestLaunchedInBackground(t *testing.T) {
	if !launchedInBackground([]string{"--background"}) {
		t.Fatal("--background did not enable background launch")
	}
	if !launchedInBackground([]string{"--some-option", "--background"}) {
		t.Fatal("background launch argument was not detected after another option")
	}
	if launchedInBackground([]string{"--background=false"}) {
		t.Fatal("an unrelated argument enabled background launch")
	}
}

func TestDesktopPreferencesReadAndUpdateAutostart(t *testing.T) {
	if !desktopAutostartSupported() {
		t.Skip("autostart integration is platform-specific")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	app.autostartStatus = func() (bool, error) { return true, nil }

	preferences, err := app.GetDesktopPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if !preferences.AutostartSupported || !preferences.AutostartEnabled {
		t.Fatalf("GetDesktopPreferences() = %+v", preferences)
	}

	var updated bool
	app.autostartSet = func(enabled bool) error {
		updated = enabled
		return nil
	}
	preferences, err = app.SetAutostartEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !updated || !preferences.AutostartEnabled || !preferences.AutostartSupported {
		t.Fatalf("SetAutostartEnabled(true) = %+v, updated=%v", preferences, updated)
	}
}

func TestDesktopHandlerStartsServiceOnlyForAPI(t *testing.T) {
	root := t.TempDir()
	paths := appdata.Paths{
		Root:     root,
		Config:   filepath.Join(root, "config.yaml"),
		Database: filepath.Join(root, "nyamedia.db"),
		Logs:     filepath.Join(root, "logs"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(paths, "test", logger)
	t.Cleanup(app.closeService)

	handler := app.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	staticResponse := httptest.NewRecorder()
	handler.ServeHTTP(staticResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if staticResponse.Code != http.StatusNoContent {
		t.Fatalf("unexpected static response status: %d", staticResponse.Code)
	}
	if app.currentService() != nil {
		t.Fatal("static asset request unexpectedly started the service")
	}

	cfg := config.Default()
	cfg.Database.Path = paths.Database
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("unexpected API response status: %d; body=%s", apiResponse.Code, apiResponse.Body.String())
	}
	if app.currentService() == nil {
		t.Fatal("API request did not start the service")
	}
}

func TestCloseServicePreventsLazyStart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	var starts atomic.Int32
	var closes atomic.Int32
	app.serviceStarter = func() (*appcore.Service, error) {
		starts.Add(1)
		return &appcore.Service{}, nil
	}
	app.serviceCloser = func(context.Context, *appcore.Service) error {
		closes.Add(1)
		return nil
	}

	app.closeService()
	service, err := app.ensureService()
	if service != nil {
		t.Fatal("service started after the desktop app began closing")
	}
	if !errors.Is(err, errDesktopServiceClosed) {
		t.Fatalf("ensureService error = %v, want %v", err, errDesktopServiceClosed)
	}
	if starts.Load() != 0 {
		t.Fatalf("starter called %d times after close", starts.Load())
	}
	if closes.Load() != 0 {
		t.Fatalf("closer called %d times without a service", closes.Load())
	}
}

func TestCloseServiceWaitsForInFlightStartAndClosesResult(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	startedService := &appcore.Service{}
	starterEntered := make(chan struct{})
	releaseStarter := make(chan struct{})
	closedServices := make(chan *appcore.Service, 1)
	app.serviceStarter = func() (*appcore.Service, error) {
		close(starterEntered)
		<-releaseStarter
		return startedService, nil
	}
	app.serviceCloser = func(_ context.Context, service *appcore.Service) error {
		closedServices <- service
		return nil
	}

	type serviceResult struct {
		service *appcore.Service
		err     error
	}
	ensureDone := make(chan serviceResult, 1)
	go func() {
		service, err := app.ensureService()
		ensureDone <- serviceResult{service: service, err: err}
	}()
	<-starterEntered

	closeDone := make(chan struct{})
	go func() {
		app.closeService()
		close(closeDone)
	}()

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		app.serviceMu.RLock()
		closing := app.serviceClosing
		app.serviceMu.RUnlock()
		if closing {
			break
		}
		select {
		case <-deadline.C:
			close(releaseStarter)
			<-closeDone
			t.Fatal("closeService did not publish its closing state")
		default:
			runtime.Gosched()
		}
	}

	select {
	case <-closeDone:
		close(releaseStarter)
		t.Fatal("closeService returned before in-flight initialisation finished")
	default:
	}
	close(releaseStarter)

	select {
	case service := <-closedServices:
		if service != startedService {
			t.Fatalf("closed service = %p, want %p", service, startedService)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("started service was not closed")
	}
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("closeService did not finish")
	}
	select {
	case result := <-ensureDone:
		if result.service != nil {
			t.Fatal("ensureService exposed a service after close began")
		}
		if !errors.Is(result.err, errDesktopServiceClosed) {
			t.Fatalf("ensureService error = %v, want %v", result.err, errDesktopServiceClosed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ensureService did not finish")
	}

	app.closeService()
	select {
	case <-closedServices:
		t.Fatal("service was closed more than once")
	default:
	}
	if app.currentService() != nil {
		t.Fatal("closed service remained published")
	}
}

func TestCloseServiceReturnsWhenCloserIgnoresCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	app.shutdownTimeout = 40 * time.Millisecond
	startedService := &appcore.Service{}
	app.serviceStarter = func() (*appcore.Service, error) {
		return startedService, nil
	}
	releaseCloser := make(chan struct{})
	app.serviceCloser = func(context.Context, *appcore.Service) error {
		<-releaseCloser
		return nil
	}
	if service, err := app.ensureService(); err != nil || service != startedService {
		t.Fatalf("ensureService() = %p, %v", service, err)
	}

	startedAt := time.Now()
	app.closeService()
	elapsed := time.Since(startedAt)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("closeService blocked for %s after its timeout", elapsed)
	}
	close(releaseCloser)
}

func TestCancelRenamePreviewsHonorsShutdownDeadline(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	app.previewWG.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	app.cancelRenamePreviews(ctx)
	elapsed := time.Since(startedAt)
	app.previewWG.Done()
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cancelRenamePreviews blocked for %s after its deadline", elapsed)
	}
}

func TestSecondInstanceBeforeStartupIsFocusedAfterContextPublish(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := NewDesktopApp(appdata.Paths{}, "test", logger)
	focused := make(chan context.Context, 1)
	app.focusWindow = func(ctx context.Context) {
		focused <- ctx
	}

	app.secondInstance(options.SecondInstanceData{})
	if app.runtimeContext() != nil {
		t.Fatal("second-instance callback unexpectedly published a runtime context")
	}
	runtimeCtx := context.WithValue(context.Background(), struct{}{}, "runtime")
	app.publishRuntimeContext(runtimeCtx)

	select {
	case got := <-focused:
		if got != runtimeCtx {
			t.Fatal("pending second-instance callback used the wrong runtime context")
		}
	case <-time.After(time.Second):
		t.Fatal("pending second-instance callback did not focus the window")
	}
}

func TestDesktopLogWriterWritesFileBeforeConsoleFailure(t *testing.T) {
	var file bytes.Buffer
	writer := desktopLogWriter(&file, failingWriter{})
	if _, err := writer.Write([]byte("persistent log")); err == nil {
		t.Fatal("expected the simulated console failure")
	}
	if got := file.String(); got != "persistent log" {
		t.Fatalf("file log = %q, want persistent log", got)
	}
}

func TestDesktopOutputAvailableRejectsClosedFile(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "closed-output")
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if desktopOutputAvailable(output) {
		t.Fatal("closed output was considered available")
	}
}

func TestRunRenamePreviewEmitsProgress(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Example.Show.S01E02.mkv")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []DesktopRenamePreviewEvent
	err := runRenamePreview(context.Background(), config.Default(), renamer.PreviewRequest{
		Path:     root,
		Template: renamer.DefaultTemplate,
	}, func(event DesktopRenamePreviewEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("expected start, item, and done events, got %#v", events)
	}
	if events[0].Type != "start" || events[0].Total != 1 {
		t.Fatalf("unexpected start event: %#v", events[0])
	}
	if events[1].Type != "item" || events[1].Item == nil || events[1].Count != 1 {
		t.Fatalf("unexpected item event: %#v", events[1])
	}
	last := events[len(events)-1]
	if last.Type != "done" || last.Count != 1 || last.Total != 1 {
		t.Fatalf("unexpected done event: %#v", last)
	}
}
