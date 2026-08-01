package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"NyaMediaMetadataTool/internal/appcore"
	"NyaMediaMetadataTool/internal/appdata"
	"NyaMediaMetadataTool/internal/appupdate"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/executil"
	"NyaMediaMetadataTool/internal/renamer"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type DesktopApp struct {
	paths            appdata.Paths
	version          string
	commit           string
	buildDate        string
	updateRepository string
	logger           *slog.Logger
	updateClient     appupdate.Client
	installed        func() bool
	launchInstaller  func(string) error

	runtimeMu             sync.RWMutex
	ctx                   context.Context
	secondInstancePending bool
	focusWindow           func(context.Context)
	startHidden           bool

	trayMu      sync.Mutex
	tray        desktopTray
	trayFactory func(onOpen func(), onQuit func()) (desktopTray, error)

	autostartStatus func() (bool, error)
	autostartSet    func(bool) error

	serviceOnce    sync.Once
	serviceMu      sync.RWMutex
	service        *appcore.Service
	serviceErr     error
	serviceClosing bool
	serviceClosed  bool
	serviceInitCtx context.Context
	serviceCancel  context.CancelFunc
	serviceStarter func() (*appcore.Service, error)
	serviceCloser  func(context.Context, *appcore.Service) error

	forceClose      atomic.Bool
	closeOnce       sync.Once
	closeDone       chan struct{}
	shutdownTimeout time.Duration
	previewMu       sync.Mutex
	previewWG       sync.WaitGroup
	previews        map[string]context.CancelFunc
	previewEnd      bool
}

var errDesktopServiceClosed = errors.New("desktop service is shutting down")

const defaultDesktopShutdownTimeout = 10 * time.Second

type DesktopRuntimeInfo struct {
	Desktop          bool   `json:"desktop"`
	Platform         string `json:"platform"`
	Arch             string `json:"arch"`
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	BuildDate        string `json:"buildDate"`
	UpdateRepository string `json:"updateRepository"`
	DataDir          string `json:"dataDir"`
	ConfigPath       string `json:"configPath"`
	Database         string `json:"database"`
}

type DesktopPreferences struct {
	AutostartEnabled   bool `json:"autostartEnabled"`
	AutostartSupported bool `json:"autostartSupported"`
}

type DesktopRenamePreviewEvent struct {
	RequestID string               `json:"requestId"`
	Type      string               `json:"type"`
	Item      *renamer.PreviewItem `json:"item,omitempty"`
	Count     int                  `json:"count"`
	Total     int                  `json:"total"`
	Error     string               `json:"error,omitempty"`
}

func NewDesktopApp(paths appdata.Paths, version string, logger *slog.Logger) *DesktopApp {
	initCtx, cancelInit := context.WithCancel(context.Background())
	app := &DesktopApp{
		paths:            paths,
		version:          version,
		commit:           commit,
		buildDate:        buildDate,
		updateRepository: updateRepository,
		logger:           logger,
		updateClient: appupdate.Client{
			Repository: updateRepository,
		},
		installed:       desktopInstalled,
		launchInstaller: launchDesktopInstaller,
		focusWindow:     focusDesktopWindow,
		trayFactory:     newDesktopTray,
		autostartStatus: desktopAutostartEnabled,
		autostartSet:    setDesktopAutostart,
		serviceInitCtx:  initCtx,
		serviceCancel:   cancelInit,
		closeDone:       make(chan struct{}),
		shutdownTimeout: defaultDesktopShutdownTimeout,
	}
	app.serviceStarter = app.startDesktopService
	app.serviceCloser = func(ctx context.Context, service *appcore.Service) error {
		return service.Close(ctx)
	}
	return app
}

func (a *DesktopApp) startup(ctx context.Context) {
	a.publishRuntimeContext(ctx)
	a.startTray()
	if err := wailsRuntime.InitializeNotifications(ctx); err != nil {
		a.logger.Debug("desktop notifications unavailable", "error", err)
	}
}

func (a *DesktopApp) domReady(ctx context.Context) {
	a.fitWindowToScreen(ctx)
	if !a.startHidden {
		wailsRuntime.WindowShow(ctx)
	}
}

func (a *DesktopApp) fitWindowToScreen(ctx context.Context) {
	screens, err := wailsRuntime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		if err != nil && a.logger != nil {
			a.logger.Debug("get screens for initial window size", "error", err)
		}
		return
	}
	selected := &screens[0]
	for index := range screens {
		if screens[index].IsCurrent {
			selected = &screens[index]
			break
		}
		if screens[index].IsPrimary {
			selected = &screens[index]
		}
	}
	screenWidth, screenHeight := selected.Size.Width, selected.Size.Height
	if screenWidth <= 0 {
		screenWidth = selected.Width
	}
	if screenHeight <= 0 {
		screenHeight = selected.Height
	}
	width, height, adjusted := fitDesktopWindowSize(screenWidth, screenHeight)
	if !adjusted {
		return
	}
	wailsRuntime.WindowSetSize(ctx, width, height)
	wailsRuntime.WindowCenter(ctx)
}

func fitDesktopWindowSize(screenWidth, screenHeight int) (int, int, bool) {
	width, height := desktopDefaultWidth, desktopDefaultHeight
	if availableWidth := screenWidth - desktopScreenWidthMargin; availableWidth > 0 && availableWidth < width {
		width = availableWidth
	}
	if availableHeight := screenHeight - desktopScreenHeightMargin; availableHeight > 0 && availableHeight < height {
		height = availableHeight
	}
	if width < desktopMinWidth {
		width = desktopMinWidth
	}
	if height < desktopMinHeight {
		height = desktopMinHeight
	}
	return width, height, width != desktopDefaultWidth || height != desktopDefaultHeight
}

func (a *DesktopApp) secondInstance(_ options.SecondInstanceData) {
	a.runtimeMu.Lock()
	ctx := a.ctx
	if ctx == nil {
		a.secondInstancePending = true
		a.runtimeMu.Unlock()
		return
	}
	a.runtimeMu.Unlock()
	a.focusWindow(ctx)
}

func (a *DesktopApp) beforeClose(ctx context.Context) bool {
	if a.forceClose.Load() {
		return false
	}
	service := a.currentService()
	if service == nil {
		return false
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	active, err := service.ActiveWork(checkCtx)
	if err != nil || active.Tasks+active.Uploads+active.Mutations+active.Background == 0 {
		return false
	}
	a.focusWindow(ctx)

	message := fmt.Sprintf(
		"仍有 %d 个媒体任务、%d 个上传任务、%d 个前台操作和 %d 个后台扫描等待或正在执行。退出会立即取消可中断操作，并最多等待 %d 秒完成收尾；未完成的队列任务会在下次启动时恢复。",
		active.Tasks,
		active.Uploads,
		active.Mutations,
		active.Background,
		int(a.desktopShutdownTimeout()/time.Second),
	)
	choice, dialogErr := wailsRuntime.MessageDialog(ctx, wailsRuntime.MessageDialogOptions{
		Type:          wailsRuntime.QuestionDialog,
		Title:         "退出 " + desktopProductName + "？",
		Message:       message,
		Buttons:       []string{"继续运行", "退出应用"},
		DefaultButton: "继续运行",
		CancelButton:  "继续运行",
	})
	if dialogErr != nil {
		a.logger.Warn("show exit confirmation", "error", dialogErr)
		return true
	}
	if choice != "退出应用" {
		return true
	}
	a.forceClose.Store(true)
	return false
}

func (a *DesktopApp) shutdown(ctx context.Context) {
	a.stopTray()
	a.clearRuntimeContext()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.desktopShutdownTimeout())
	defer cancel()
	closeDone := a.beginCloseService()
	a.cancelRenamePreviews(shutdownCtx)
	wailsRuntime.CleanupNotifications(ctx)
	select {
	case <-closeDone:
	case <-shutdownCtx.Done():
		a.logger.Warn("desktop shutdown exceeded time limit", "timeout", a.desktopShutdownTimeout())
	}
}

func (a *DesktopApp) startTray() {
	if !desktopTraySupported() {
		return
	}
	tray, err := a.trayFactory(a.showWindow, a.quitFromTray)
	if err != nil {
		a.logger.Warn("start desktop tray", "error", err)
		return
	}
	a.trayMu.Lock()
	a.tray = tray
	a.trayMu.Unlock()
}

func (a *DesktopApp) stopTray() {
	a.trayMu.Lock()
	tray := a.tray
	a.tray = nil
	a.trayMu.Unlock()
	if tray != nil {
		if err := tray.Close(); err != nil {
			a.logger.Warn("stop desktop tray", "error", err)
		}
	}
}

func (a *DesktopApp) showWindow() {
	if ctx := a.runtimeContext(); ctx != nil {
		a.focusWindow(ctx)
	}
}

func (a *DesktopApp) quitFromTray() {
	if ctx := a.runtimeContext(); ctx != nil {
		wailsRuntime.Quit(ctx)
	}
}

func (a *DesktopApp) closeService() {
	ctx, cancel := context.WithTimeout(context.Background(), a.desktopShutdownTimeout())
	defer cancel()
	closeDone := a.beginCloseService()
	select {
	case <-closeDone:
	case <-ctx.Done():
		a.logger.Warn("desktop service close exceeded time limit", "timeout", a.desktopShutdownTimeout())
	}
}

func (a *DesktopApp) beginCloseService() <-chan struct{} {
	a.closeOnce.Do(func() {
		a.serviceMu.Lock()
		a.serviceClosing = true
		cancelInit := a.serviceCancel
		a.serviceMu.Unlock()
		if cancelInit != nil {
			cancelInit()
		}
		go a.finishServiceClose()
	})
	return a.closeDone
}

func (a *DesktopApp) finishServiceClose() {
	ctx, cancel := context.WithTimeout(context.Background(), a.desktopShutdownTimeout())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		// If initialisation is in flight this waits for it. If it has not
		// started, the no-op consumes the Once and prevents a later start.
		a.serviceOnce.Do(func() {})

		a.serviceMu.Lock()
		service := a.service
		a.service = nil
		a.serviceMu.Unlock()
		if service == nil {
			result <- nil
			return
		}
		result <- a.serviceCloser(ctx, service)
	}()

	var closeErr error
	select {
	case closeErr = <-result:
	case <-ctx.Done():
		closeErr = ctx.Err()
	}
	if closeErr != nil {
		a.logger.Warn("shutdown desktop service", "error", closeErr)
	}
	a.serviceMu.Lock()
	a.serviceClosed = true
	a.serviceMu.Unlock()
	close(a.closeDone)
}

func (a *DesktopApp) desktopShutdownTimeout() time.Duration {
	if a.shutdownTimeout <= 0 {
		return defaultDesktopShutdownTimeout
	}
	return a.shutdownTimeout
}

func (a *DesktopApp) ensureService() (*appcore.Service, error) {
	a.serviceMu.RLock()
	closing := a.serviceClosing || a.serviceClosed
	a.serviceMu.RUnlock()
	if closing {
		return nil, errDesktopServiceClosed
	}

	a.serviceOnce.Do(func() {
		a.serviceMu.RLock()
		closing := a.serviceClosing || a.serviceClosed
		a.serviceMu.RUnlock()
		if closing {
			return
		}

		service, err := a.serviceStarter()
		if service == nil && err == nil {
			err = errors.New("desktop service starter returned no service")
		}
		a.serviceMu.Lock()
		if err != nil {
			a.serviceErr = err
		} else {
			a.service = service
		}
		a.serviceMu.Unlock()
	})
	a.serviceMu.RLock()
	defer a.serviceMu.RUnlock()
	if a.serviceClosing || a.serviceClosed {
		return nil, errDesktopServiceClosed
	}
	return a.service, a.serviceErr
}

func (a *DesktopApp) startDesktopService() (*appcore.Service, error) {
	if err := appdata.EnsureContext(a.serviceInitCtx, a.paths); err != nil {
		return nil, fmt.Errorf("prepare desktop data: %w", err)
	}
	if err := a.serviceInitCtx.Err(); err != nil {
		return nil, err
	}
	service, err := appcore.Start(context.Background(), a.paths.Config, a.logger)
	if err != nil {
		return nil, fmt.Errorf("start desktop service: %w", err)
	}
	return service, nil
}

func (a *DesktopApp) currentService() *appcore.Service {
	a.serviceMu.RLock()
	defer a.serviceMu.RUnlock()
	if a.serviceClosing || a.serviceClosed || a.serviceErr != nil {
		return nil
	}
	return a.service
}

func (a *DesktopApp) publishRuntimeContext(ctx context.Context) {
	a.runtimeMu.Lock()
	a.ctx = ctx
	pending := a.secondInstancePending
	a.secondInstancePending = false
	a.runtimeMu.Unlock()
	if pending {
		a.focusWindow(ctx)
	}
}

func (a *DesktopApp) runtimeContext() context.Context {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.ctx
}

func (a *DesktopApp) clearRuntimeContext() {
	a.runtimeMu.Lock()
	a.ctx = nil
	a.runtimeMu.Unlock()
}

func (a *DesktopApp) Handler(static http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			static.ServeHTTP(w, r)
			return
		}
		service, err := a.ensureService()
		if err != nil {
			a.logger.Error("desktop service unavailable", "error", err)
			http.Error(w, "desktop service unavailable", http.StatusServiceUnavailable)
			return
		}
		service.Handler().ServeHTTP(w, r)
	})
}

func (a *DesktopApp) GetRuntimeInfo() (DesktopRuntimeInfo, error) {
	service, err := a.ensureService()
	if err != nil {
		return DesktopRuntimeInfo{}, err
	}
	return DesktopRuntimeInfo{
		Desktop:          true,
		Platform:         runtime.GOOS,
		Arch:             runtime.GOARCH,
		Version:          a.version,
		Commit:           a.commit,
		BuildDate:        a.buildDate,
		UpdateRepository: a.updateRepository,
		DataDir:          a.paths.Root,
		ConfigPath:       a.paths.Config,
		Database:         service.Config.Database.Path,
	}, nil
}

func (a *DesktopApp) CheckForUpdates() (appupdate.CheckResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.updateClient.Check(ctx, a.version, a.installed())
}

func (a *DesktopApp) DownloadAndInstallUpdate(requestedVersion string) error {
	requestedVersion = strings.TrimSpace(requestedVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	installer, err := a.updateClient.Download(
		ctx,
		a.version,
		requestedVersion,
		filepath.Join(a.paths.Root, "updates", requestedVersion),
		a.installed(),
	)
	if err != nil {
		return err
	}
	if err := a.launchInstaller(installer); err != nil {
		return fmt.Errorf("launch update installer: %w", err)
	}
	if ctx := a.runtimeContext(); ctx != nil {
		wailsRuntime.Quit(ctx)
	}
	return nil
}

func launchDesktopInstaller(path string) error {
	return exec.Command(path).Start()
}

func (a *DesktopApp) GetDesktopPreferences() (DesktopPreferences, error) {
	preferences := DesktopPreferences{AutostartSupported: desktopAutostartSupported()}
	if !preferences.AutostartSupported {
		return preferences, nil
	}
	enabled, err := a.autostartStatus()
	if err != nil {
		return DesktopPreferences{}, err
	}
	preferences.AutostartEnabled = enabled
	return preferences, nil
}

func (a *DesktopApp) SetAutostartEnabled(enabled bool) (DesktopPreferences, error) {
	if !desktopAutostartSupported() {
		return DesktopPreferences{AutostartSupported: false}, errors.New("autostart is not supported on this platform")
	}
	if err := a.autostartSet(enabled); err != nil {
		return DesktopPreferences{}, err
	}
	return DesktopPreferences{AutostartEnabled: enabled, AutostartSupported: true}, nil
}

func (a *DesktopApp) PickDirectory(title string, initialPath string, allowedRoot string) (string, error) {
	ctx := a.runtimeContext()
	if ctx == nil {
		return "", errors.New("desktop runtime is not ready")
	}
	selected, err := wailsRuntime.OpenDirectoryDialog(ctx, wailsRuntime.OpenDialogOptions{
		Title:                title,
		DefaultDirectory:     existingDirectory(initialPath),
		CanCreateDirectories: true,
	})
	if err != nil || selected == "" {
		return selected, err
	}
	if allowedRoot != "" && !pathWithinRoot(selected, allowedRoot) {
		return "", errors.New("请选择媒体目录范围内的文件夹")
	}
	return selected, nil
}

func (a *DesktopApp) PickFile(title string, initialPath string, displayName string, pattern string) (string, error) {
	ctx := a.runtimeContext()
	if ctx == nil {
		return "", errors.New("desktop runtime is not ready")
	}
	options := wailsRuntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: existingDirectory(initialPath),
	}
	if strings.TrimSpace(pattern) != "" {
		options.Filters = []wailsRuntime.FileFilter{{DisplayName: displayName, Pattern: pattern}}
	}
	return wailsRuntime.OpenFileDialog(ctx, options)
}

func (a *DesktopApp) RevealPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return startOpenCommand(path, false)
	}
	return startOpenCommand(path, true)
}

func (a *DesktopApp) OpenPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return startDefaultCommand(path)
}

func (a *DesktopApp) Notify(title string, body string) error {
	ctx := a.runtimeContext()
	if ctx == nil || !wailsRuntime.IsNotificationAvailable(ctx) {
		return nil
	}
	return wailsRuntime.SendNotification(ctx, wailsRuntime.NotificationOptions{
		ID:    fmt.Sprintf("nyamedia-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	})
}

// PreviewRename uses Wails events because its AssetServer buffers HTTP
// responses and cannot expose the streaming progress used by the web UI.
func (a *DesktopApp) PreviewRename(requestID string, input renamer.PreviewRequest) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || len(requestID) > 128 {
		return errors.New("invalid rename preview request ID")
	}
	runtimeCtx := a.runtimeContext()
	if runtimeCtx == nil {
		return errors.New("desktop runtime is not ready")
	}
	if _, err := a.ensureService(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(runtimeCtx)
	a.previewMu.Lock()
	if a.previewEnd {
		a.previewMu.Unlock()
		cancel()
		return errDesktopServiceClosed
	}
	if a.previews == nil {
		a.previews = make(map[string]context.CancelFunc)
	}
	if _, exists := a.previews[requestID]; exists {
		a.previewMu.Unlock()
		cancel()
		return errors.New("rename preview request is already running")
	}
	a.previews[requestID] = cancel
	a.previewWG.Add(1)
	a.previewMu.Unlock()
	defer func() {
		cancel()
		a.previewMu.Lock()
		delete(a.previews, requestID)
		a.previewMu.Unlock()
		a.previewWG.Done()
	}()

	cfg, err := config.Load(a.paths.Config)
	if err != nil {
		return err
	}
	emit := func(event DesktopRenamePreviewEvent) {
		event.RequestID = requestID
		wailsRuntime.EventsEmit(runtimeCtx, "nyamedia:rename-preview", event)
	}
	return runRenamePreview(ctx, cfg, input, emit)
}

func (a *DesktopApp) CancelRenamePreview(requestID string) bool {
	a.previewMu.Lock()
	cancel := a.previews[strings.TrimSpace(requestID)]
	a.previewMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (a *DesktopApp) cancelRenamePreviews(ctx context.Context) {
	a.previewMu.Lock()
	a.previewEnd = true
	cancels := make([]context.CancelFunc, 0, len(a.previews))
	for _, cancel := range a.previews {
		cancels = append(cancels, cancel)
	}
	a.previewMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		a.previewWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		a.logger.Warn("rename previews did not stop before shutdown timeout")
	}
}

func runRenamePreview(ctx context.Context, cfg config.Config, input renamer.PreviewRequest, emit func(DesktopRenamePreviewEvent)) error {
	count := 0
	total := 0
	err := renamer.PreviewEachProgress(ctx, cfg, input, func(value int) error {
		total = value
		emit(DesktopRenamePreviewEvent{Type: "start", Total: total})
		return nil
	}, func(item renamer.PreviewItem) error {
		count++
		emit(DesktopRenamePreviewEvent{Type: "item", Item: &item, Count: count, Total: total})
		return nil
	})
	if err != nil {
		emit(DesktopRenamePreviewEvent{Type: "error", Count: count, Total: total, Error: err.Error()})
		return err
	}
	emit(DesktopRenamePreviewEvent{Type: "done", Count: count, Total: total})
	return nil
}

func existingDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func pathWithinRoot(path string, root string) bool {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	resolvedPath, pathErr := filepath.EvalSymlinks(absolutePath)
	resolvedRoot, rootErr := filepath.EvalSymlinks(absoluteRoot)
	if pathErr == nil && rootErr == nil {
		absolutePath = resolvedPath
		absoluteRoot = resolvedRoot
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func startOpenCommand(path string, selectFile bool) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if selectFile {
			command = executil.VisibleCommand("explorer.exe", "/select,", path)
		} else {
			command = executil.VisibleCommand("explorer.exe", path)
		}
	case "darwin":
		if selectFile {
			command = executil.VisibleCommand("open", "-R", path)
		} else {
			command = executil.VisibleCommand("open", path)
		}
	default:
		target := path
		if selectFile {
			target = filepath.Dir(path)
		}
		command = executil.VisibleCommand("xdg-open", target)
	}
	return startAndReap(command)
}

func startDefaultCommand(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = executil.VisibleCommand("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		command = executil.VisibleCommand("open", path)
	default:
		command = executil.VisibleCommand("xdg-open", path)
	}
	return startAndReap(command)
}

func startAndReap(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		_ = command.Wait()
	}()
	return nil
}

func focusDesktopWindow(ctx context.Context) {
	wailsRuntime.WindowShow(ctx)
	wailsRuntime.WindowUnminimise(ctx)
}
