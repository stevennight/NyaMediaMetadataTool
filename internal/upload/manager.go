package upload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/pipeline"
	"NyaMediaMetadataTool/internal/store"
)

const schedulerInterval = 2 * time.Second

type Options struct {
	Concurrency int
	QuietPeriod time.Duration
	MaxAttempts int
}

func DefaultOptions() Options {
	return Options{Concurrency: 1, QuietPeriod: 2 * time.Minute, MaxAttempts: 3}
}

// RemoteEntry is a provider-neutral view used for validation and future
// destination directory pickers.
type RemoteEntry struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// RemoteFile is returned after a file is verified at a destination.
type RemoteFile struct {
	ID        string
	Size      int64
	SHA1      string
	LocalSHA1 string
	Outcome   string
}

// Provider is intentionally small. New provider types such as 115 Open,
// 123pan, and Baidu Pan only need to implement this contract.
type Provider interface {
	Check(ctx context.Context) error
	List(ctx context.Context, remotePath string) ([]RemoteEntry, error)
	Upload(ctx context.Context, localPath string, remotePath string, size int64, localSHA1 string, collisionPolicy string) (RemoteFile, error)
}

type ProviderVerifier interface {
	Verify(ctx context.Context, remotePath string, size int64, localSHA1 string) (RemoteFile, bool, error)
}

type UploadAttemptError struct {
	Outcome   string
	LocalSHA1 string
	Err       error
}

func (err *UploadAttemptError) Error() string {
	if err == nil || err.Err == nil {
		return "upload attempt failed"
	}
	return err.Err.Error()
}

func (err *UploadAttemptError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// TransferRuntimeState is ephemeral progress information for an active
// transfer. Persistent transfer status remains owned by the store.
type TransferRuntimeState struct {
	Phase            string `json:"phase,omitempty"`
	StatusMessage    string `json:"statusMessage,omitempty"`
	WaitingUntil     string `json:"waitingUntil,omitempty"`
	BytesTransferred int64  `json:"bytesTransferred,omitempty"`
}

type providerWaitReporter interface {
	setWaitReporter(func(message string, until time.Time))
}

type providerProgressReporter interface {
	setProgressReporter(func(bytesTransferred int64))
}

type ProviderFactory func(ctx context.Context, target store.UploadBatchTarget) (Provider, error)

type Manager struct {
	options    Options
	store      *store.Store
	logger     *slog.Logger
	factory    ProviderFactory
	builders   map[string]ProviderBuilder
	providers  map[string]ProviderDescriptor
	providerMu sync.RWMutex

	mu               sync.Mutex
	active           map[int64]context.CancelFunc
	authMu           sync.Mutex
	authFlows        map[string]*cookie115AuthFlow
	open115AuthMu    sync.Mutex
	open115AuthFlows map[string]*open115AuthFlow

	cookie115GuardMu sync.Mutex
	cookie115Guards  map[int64]*cookie115RequestGuard
	open115Mu        sync.Mutex
	open115Sessions  map[int64]*open115Session

	runtimeMu        sync.RWMutex
	transferRuntime  map[int64]TransferRuntimeState
	notificationHTTP *http.Client
}

func New(st *store.Store, logger *slog.Logger) *Manager {
	return newManager(DefaultOptions(), st, logger, nil)
}

func NewWithOptions(options Options, st *store.Store, logger *slog.Logger) *Manager {
	return newManager(options, st, logger, nil)
}

func NewWithFactory(options Options, st *store.Store, logger *slog.Logger, factory ProviderFactory) *Manager {
	return newManager(options, st, logger, factory)
}

func newManager(options Options, st *store.Store, logger *slog.Logger, factory ProviderFactory) *Manager {
	options = normalizeOptions(options)
	manager := &Manager{
		options:          options,
		store:            st,
		logger:           logger,
		active:           make(map[int64]context.CancelFunc),
		authFlows:        make(map[string]*cookie115AuthFlow),
		open115AuthFlows: make(map[string]*open115AuthFlow),
		builders:         make(map[string]ProviderBuilder),
		providers:        providerDescriptorMap(),
		cookie115Guards:  make(map[int64]*cookie115RequestGuard),
		open115Sessions:  make(map[int64]*open115Session),
		transferRuntime:  make(map[int64]TransferRuntimeState),
		notificationHTTP: &http.Client{Timeout: 10 * time.Second},
	}
	manager.registerBuiltInProviders()
	if factory == nil {
		factory = manager.defaultProviderFactory
	}
	manager.factory = factory
	return manager
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.Concurrency <= 0 {
		options.Concurrency = defaults.Concurrency
	}
	if options.QuietPeriod <= 0 {
		options.QuietPeriod = defaults.QuietPeriod
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = defaults.MaxAttempts
	}
	return options
}

func (m *Manager) RegisterProvider(providerType string, builder ProviderBuilder) {
	m.RegisterProviderDescriptor(ProviderDescriptor{Type: providerType}, builder)
}

// RegisterProviderDescriptor registers an upload implementation together with
// the metadata exposed to configuration clients. Future providers can declare
// their credential keys here and use the generic secret API without adding a
// provider-specific settings endpoint.
func (m *Manager) RegisterProviderDescriptor(descriptor ProviderDescriptor, builder ProviderBuilder) {
	providerType := normalizeProviderType(descriptor.Type)
	if providerType == "" || builder == nil {
		return
	}
	m.providerMu.Lock()
	m.builders[providerType] = builder
	existing, exists := m.providers[providerType]
	if !exists {
		existing = ProviderDescriptor{Type: providerType, Name: providerType}
	}
	if strings.TrimSpace(descriptor.Name) == "" {
		descriptor.Name = existing.Name
	}
	if len(descriptor.SecretKeys) == 0 {
		descriptor.SecretKeys = append([]string{}, existing.SecretKeys...)
	}
	if len(descriptor.AuthDevices) == 0 {
		descriptor.AuthDevices = append([]AuthDeviceDescriptor{}, existing.AuthDevices...)
	}
	descriptor.Type = providerType
	descriptor.Implemented = true
	descriptor.SecretKeys = append([]string{}, descriptor.SecretKeys...)
	descriptor.AuthDevices = append([]AuthDeviceDescriptor{}, descriptor.AuthDevices...)
	m.providers[providerType] = descriptor
	m.providerMu.Unlock()
}

func (m *Manager) registerBuiltInProviders() {
	m.RegisterProvider(store.UploadProviderType115Cookie, func(ctx context.Context, target store.UploadBatchTarget, lookup SecretLookup) (Provider, error) {
		cookieValue, err := lookup(ctx, "cookie")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(cookieValue) == "" {
			return nil, fmt.Errorf("115 Cookie is not configured for destination %q", target.ProviderName)
		}
		provider, err := newCookie115Provider(cookieValue, target.UserAgent)
		if err != nil {
			return nil, err
		}
		provider.requestGuard = m.cookie115RequestGuard(target.ProviderID)
		return provider, nil
	})
	m.RegisterProvider(store.UploadProviderType115Open, func(ctx context.Context, target store.UploadBatchTarget, lookup SecretLookup) (Provider, error) {
		accessToken, err := lookup(ctx, "access_token")
		if err != nil {
			return nil, err
		}
		refreshToken, err := lookup(ctx, "refresh_token")
		if err != nil {
			return nil, err
		}
		expiresAt, err := lookup(ctx, "access_token_expires_at")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(accessToken) == "" && strings.TrimSpace(refreshToken) == "" {
			return nil, fmt.Errorf("115 Open tokens are not configured for destination %q", target.ProviderName)
		}
		session := m.open115Session(target.ProviderID, accessToken, refreshToken, expiresAt, target.UserAgent)
		provider, err := newOpen115Provider(session)
		if err != nil {
			return nil, err
		}
		provider.requestGuard = m.cookie115RequestGuard(target.ProviderID)
		provider.requestInterval = time.Duration(store.NormalizeUploadRequestIntervalMS(target.RequestIntervalMS)) * time.Millisecond
		return provider, nil
	})
}

func (m *Manager) open115Session(providerID int64, accessToken, refreshToken, expiresAt, userAgent string) *open115Session {
	m.open115Mu.Lock()
	defer m.open115Mu.Unlock()
	if session := m.open115Sessions[providerID]; session != nil && session.matches(accessToken, refreshToken, expiresAt) {
		return session
	}
	session := newOpen115Session(accessToken, refreshToken, expiresAt, userAgent, func(updatedAccessToken, updatedRefreshToken, updatedExpiresAt string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.store.SetUploadProvider115OpenRefreshedTokens(ctx, providerID, updatedAccessToken, updatedRefreshToken, updatedExpiresAt); err != nil && m.logger != nil {
			m.logger.Error("persist refreshed 115 Open tokens", "provider_id", providerID, "error", err)
		}
	})
	m.open115Sessions[providerID] = session
	return session
}
func (m *Manager) cookie115RequestGuard(providerID int64) *cookie115RequestGuard {
	if providerID <= 0 {
		return newCookie115RequestGuard()
	}
	m.cookie115GuardMu.Lock()
	defer m.cookie115GuardMu.Unlock()
	guard := m.cookie115Guards[providerID]
	if guard == nil {
		guard = newCookie115RequestGuard()
		m.cookie115Guards[providerID] = guard
	}
	return guard
}

func (m *Manager) TransferRuntimeStates() map[int64]TransferRuntimeState {
	m.runtimeMu.RLock()
	defer m.runtimeMu.RUnlock()
	result := make(map[int64]TransferRuntimeState, len(m.transferRuntime))
	for transferID, state := range m.transferRuntime {
		result[transferID] = state
	}
	return result
}

func (m *Manager) setTransferRuntime(transferID int64, phase string, message string, waitingUntil time.Time) {
	if transferID <= 0 {
		return
	}
	m.runtimeMu.Lock()
	state := m.transferRuntime[transferID]
	state.Phase = strings.TrimSpace(phase)
	state.StatusMessage = strings.TrimSpace(message)
	state.WaitingUntil = ""
	if !waitingUntil.IsZero() {
		state.WaitingUntil = waitingUntil.UTC().Format(time.RFC3339Nano)
	}
	m.transferRuntime[transferID] = state
	m.runtimeMu.Unlock()
}

func (m *Manager) setTransferProgress(transferID int64, bytesTransferred int64) {
	if transferID <= 0 {
		return
	}
	if bytesTransferred < 0 {
		bytesTransferred = 0
	}
	m.runtimeMu.Lock()
	state, ok := m.transferRuntime[transferID]
	if ok && bytesTransferred != state.BytesTransferred {
		state.BytesTransferred = bytesTransferred
		m.transferRuntime[transferID] = state
	}
	m.runtimeMu.Unlock()
}

func (m *Manager) clearTransferRuntime(transferID int64) {
	if transferID <= 0 {
		return
	}
	m.runtimeMu.Lock()
	delete(m.transferRuntime, transferID)
	m.runtimeMu.Unlock()
}

// ProviderDescriptors reports the actual runtime registry, so an installed
// extension becomes selectable without a second, separate capability list.
func (m *Manager) ProviderDescriptors() []ProviderDescriptor {
	m.providerMu.RLock()
	defer m.providerMu.RUnlock()
	return sortedProviderDescriptors(m.providers)
}

func (m *Manager) ProviderDescriptor(providerType string) (ProviderDescriptor, bool) {
	m.providerMu.RLock()
	defer m.providerMu.RUnlock()
	descriptor, ok := m.providers[normalizeProviderType(providerType)]
	descriptor.SecretKeys = append([]string{}, descriptor.SecretKeys...)
	descriptor.AuthDevices = append([]AuthDeviceDescriptor{}, descriptor.AuthDevices...)
	return descriptor, ok
}

// RecordMediaProcessed is the handoff from the metadata pipeline. It only
// collects stable source and generated artifacts; network work happens later.
func (m *Manager) RecordMediaProcessed(ctx context.Context, task store.Task, media store.MediaFile) error {
	dir, err := m.store.FindWatchDirForPath(ctx, media.Path)
	if errors.Is(err, store.ErrWatchDirNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find media watch directory: %w", err)
	}
	hasEnabledConfig := false
	for _, uploadConfig := range dir.UploadConfigs {
		if uploadConfig.Enabled {
			hasEnabledConfig = true
			break
		}
	}
	if !hasEnabledConfig {
		return nil
	}
	artifacts, err := m.store.ListArtifactsByTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("list task artifacts: %w", err)
	}
	seriesPath := pipeline.SeriesDirectory(media.Path)
	files, err := collectCandidates(media.Path, seriesPath, dir.Path, artifacts)
	if err != nil {
		return err
	}
	seriesKey := normalizeSeriesKey(&dir.ID, seriesPath)
	batches, created, err := m.store.CollectUploadBatches(ctx, store.UploadCollectionInput{
		WatchDirID:  &dir.ID,
		SeriesKey:   seriesKey,
		SeriesPath:  seriesPath,
		QuietPeriod: m.options.QuietPeriod,
		Files:       files,
	})
	if err != nil {
		return fmt.Errorf("collect upload batch: %w", err)
	}
	if len(batches) > 0 {
		batchIDs := make([]int64, 0, len(batches))
		for _, batch := range batches {
			batchIDs = append(batchIDs, batch.ID)
		}
		m.logger.Info("upload batches collected", "batchIDs", batchIDs, "created", created, "series", seriesPath, "files", len(files))
	}
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	workers := m.options.Concurrency
	if workers <= 0 {
		workers = 1
	}
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.worker(ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.sealer(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.notificationWorker(ctx)
	}()
	<-ctx.Done()
	m.CancelRunningTargets()
	wg.Wait()
	return nil
}

func (m *Manager) CancelRunningTargets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.active {
		cancel()
	}
	return len(m.active)
}

func (m *Manager) CheckProvider(ctx context.Context, providerID int64) error {
	provider, err := m.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return err
	}
	client, err := m.factory(ctx, targetFromProvider(provider))
	if err != nil {
		return err
	}
	return client.Check(ctx)
}

func (m *Manager) ListProviderDirectory(ctx context.Context, providerID int64, providerPath string) ([]RemoteEntry, error) {
	provider, err := m.store.GetUploadProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	client, err := m.factory(ctx, targetFromProvider(provider))
	if err != nil {
		return nil, err
	}
	return client.List(ctx, providerPath)
}

func (m *Manager) sealer(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()
	for {
		if _, err := m.store.SealDueUploadBatches(ctx, time.Now(), m.options.QuietPeriod); err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Warn("seal upload batches failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		target, err := m.store.ClaimNextUploadTarget(ctx)
		if errors.Is(err, store.ErrNoPendingUploadTarget) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			m.logger.Warn("claim upload target failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		targetCtx, cancel := context.WithCancel(ctx)
		m.trackTarget(target.ID, cancel)
		err = m.processTarget(targetCtx, target)
		wasCanceled := errors.Is(targetCtx.Err(), context.Canceled)
		cancel()
		m.untrackTarget(target.ID)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || wasCanceled {
			continue
		}
		m.handleTargetFailure(ctx, target, err)
	}
}

func (m *Manager) handleTargetFailure(ctx context.Context, target store.UploadBatchTarget, uploadErr error) {
	m.logger.Warn("upload target failed", "targetID", target.ID, "provider", target.ProviderName, "error", uploadErr)
	if target.Attempts < m.options.MaxAttempts {
		delay := retryDelay(target.Attempts)
		if retryErr := m.store.RescheduleUploadTarget(ctx, target.ID, uploadErr.Error(), time.Now().Add(delay)); retryErr == nil {
			return
		} else {
			m.logger.Warn("reschedule upload target failed", "targetID", target.ID, "error", retryErr)
			uploadErr = fmt.Errorf("%w; reschedule upload target: %v", uploadErr, retryErr)
		}
	}
	// A failed reschedule must not leave a target looking active forever. If
	// this write also fails, startup recovery still resets running work.
	if failErr := m.store.FailUploadTarget(ctx, target.ID, uploadErr.Error()); failErr != nil {
		m.logger.Warn("mark upload target failed", "targetID", target.ID, "error", failErr)
	}
}

func (m *Manager) processTarget(ctx context.Context, target store.UploadBatchTarget) error {
	client, err := m.factory(ctx, target)
	if err != nil {
		return err
	}
	transfers, err := m.store.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil {
		return err
	}

	var activeMu sync.RWMutex
	var activeTransferID int64
	var activeTransferBytesTotal int64
	var activePhase string
	var activeMessage string
	setActiveTransfer := func(transferID int64, bytesTotal int64, phase string, message string) {
		activeMu.Lock()
		previousTransferID := activeTransferID
		activeTransferID = transferID
		activeTransferBytesTotal = bytesTotal
		activePhase = phase
		activeMessage = message
		activeMu.Unlock()
		if previousTransferID != 0 && previousTransferID != transferID {
			m.clearTransferRuntime(previousTransferID)
		}
		m.setTransferRuntime(transferID, phase, message, time.Time{})
	}
	clearActiveTransfer := func() {
		activeMu.Lock()
		transferID := activeTransferID
		activeTransferID = 0
		activeTransferBytesTotal = 0
		activePhase = ""
		activeMessage = ""
		activeMu.Unlock()
		m.clearTransferRuntime(transferID)
	}
	defer clearActiveTransfer()

	for _, transfer := range transfers {
		if transfer.Status != store.UploadTransferCompleted {
			setActiveTransfer(transfer.ID, transfer.BytesTotal, "checking", "正在检查 115 上传服务")
			break
		}
	}
	if reporter, ok := client.(providerWaitReporter); ok {
		reporter.setWaitReporter(func(message string, until time.Time) {
			activeMu.RLock()
			defer activeMu.RUnlock()
			transferID := activeTransferID
			phase := activePhase
			activeStatusMessage := activeMessage
			if transferID == 0 {
				return
			}
			if until.IsZero() {
				m.setTransferRuntime(transferID, phase, activeStatusMessage, time.Time{})
				return
			}
			m.setTransferRuntime(transferID, "waiting", message, until)
		})
		defer reporter.setWaitReporter(nil)
	}
	if reporter, ok := client.(providerProgressReporter); ok {
		reporter.setProgressReporter(func(bytesTransferred int64) {
			activeMu.RLock()
			defer activeMu.RUnlock()
			transferID := activeTransferID
			bytesTotal := activeTransferBytesTotal
			if transferID == 0 {
				return
			}
			if bytesTotal > 0 && bytesTransferred > bytesTotal {
				bytesTransferred = bytesTotal
			}
			m.setTransferProgress(transferID, bytesTransferred)
		})
		defer reporter.setProgressReporter(nil)
	}
	if err := client.Check(ctx); err != nil {
		return fmt.Errorf("check provider %s: %w", target.ProviderName, err)
	}

	failedTransfers := 0
	var firstFailure error
	recordFailure := func(transferID int64, transferErr error) error {
		var attemptErr *UploadAttemptError
		outcome := ""
		localSHA1 := ""
		if errors.As(transferErr, &attemptErr) {
			outcome = attemptErr.Outcome
			localSHA1 = attemptErr.LocalSHA1
		}
		if err := m.store.FailUploadTransferWithResult(ctx, transferID, transferErr.Error(), outcome, localSHA1); err != nil {
			return fmt.Errorf("mark transfer %d failed: %w", transferID, err)
		}
		failedTransfers++
		if firstFailure == nil {
			firstFailure = transferErr
		}
		return nil
	}
	for _, transfer := range transfers {
		if transfer.Status == store.UploadTransferCompleted {
			continue
		}
		if transfer.Status == store.UploadTransferFailed && strings.Contains(transfer.ErrorSummary, uncertain115CommitMarker) {
			setActiveTransfer(transfer.ID, transfer.BytesTotal, "verifying", "正在确认 115 远端文件")
			verificationErr := fmt.Errorf("%s: remote file is still not confirmed", uncertain115CommitMarker)
			if verifier, ok := client.(ProviderVerifier); ok {
				remote, found, err := verifier.Verify(ctx, transfer.RemotePath, transfer.BytesTotal, transfer.LocalSHA1)
				if err == nil && found {
					if err := m.completeTransfer(ctx, transfer, remote); err != nil {
						return err
					}
					continue
				}
				if err != nil {
					verificationErr = fmt.Errorf("%s: verify remote file: %w", uncertain115CommitMarker, err)
				}
			}
			if target.Attempts < m.options.MaxAttempts {
				if err := recordFailure(transfer.ID, verificationErr); err != nil {
					return err
				}
				continue
			}
		}
		setActiveTransfer(transfer.ID, transfer.BytesTotal, "preparing", "正在准备上传")
		if err := m.store.StartUploadTransfer(ctx, transfer.ID); err != nil {
			return fmt.Errorf("start transfer %d: %w", transfer.ID, err)
		}
		info, err := os.Stat(transfer.LocalPath)
		if err != nil {
			transferErr := fmt.Errorf("stat local file %s: %w", transfer.LocalPath, err)
			if err := recordFailure(transfer.ID, transferErr); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() || info.Size() != transfer.BytesTotal || !uploadSnapshotTimeMatches(transfer.ModifiedAt, info.ModTime()) {
			transferErr := fmt.Errorf("local file changed after batch snapshot: %s", transfer.LocalPath)
			if err := recordFailure(transfer.ID, transferErr); err != nil {
				return err
			}
			continue
		}
		activeMu.Lock()
		activePhase = "uploading"
		activeMessage = "正在上传到 115"
		activeMu.Unlock()
		m.setTransferRuntime(transfer.ID, "uploading", "正在上传到 115", time.Time{})
		remote, err := client.Upload(ctx, transfer.LocalPath, transfer.RemotePath, transfer.BytesTotal, transfer.LocalSHA1, target.CollisionPolicy)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			transferErr := fmt.Errorf("upload %s: %w", transfer.LocalPath, err)
			if err := recordFailure(transfer.ID, transferErr); err != nil {
				return err
			}
			continue
		}
		if err := m.completeTransfer(ctx, transfer, remote); err != nil {
			return err
		}
		clearActiveTransfer()
	}
	if failedTransfers > 0 {
		return fmt.Errorf("%d file(s) failed; first failure: %w", failedTransfers, firstFailure)
	}
	return m.store.CompleteUploadTarget(ctx, target.ID)
}

func (m *Manager) completeTransfer(ctx context.Context, transfer store.UploadTransfer, remote RemoteFile) error {
	outcome := strings.ToLower(strings.TrimSpace(remote.Outcome))
	if store.UploadOutcomeChangesRemote(transfer.Outcome) &&
		(outcome == "" || outcome == store.UploadOutcomeUnchanged) {
		outcome = transfer.Outcome
	}
	if outcome == "" {
		// Unknown successful providers are treated conservatively as a remote
		// change so future notification consumers cannot miss an update.
		outcome = store.UploadOutcomeCreated
	}
	localSHA1 := strings.TrimSpace(remote.LocalSHA1)
	if localSHA1 == "" {
		localSHA1 = transfer.LocalSHA1
	}
	return m.store.CompleteUploadTransferWithResult(ctx, transfer.ID, store.UploadTransferCompletion{
		RemoteID:   remote.ID,
		Outcome:    outcome,
		LocalSHA1:  localSHA1,
		RemoteSHA1: remote.SHA1,
	})
}

func uploadSnapshotTimeMatches(snapshot string, actual time.Time) bool {
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(snapshot), time.UTC)
	if err != nil {
		return false
	}
	return parsed.Equal(actual.UTC().Truncate(time.Second))
}

func (m *Manager) trackTarget(targetID int64, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[targetID] = cancel
}

func (m *Manager) untrackTarget(targetID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, targetID)
}

func targetFromProvider(provider store.UploadProvider) store.UploadBatchTarget {
	return store.UploadBatchTarget{
		ProviderID:        provider.ID,
		ProviderName:      provider.Name,
		ProviderType:      provider.Type,
		UserAgent:         provider.UserAgent,
		RequestIntervalMS: provider.RequestIntervalMS,
		RemoteRoot:        "/",
		CollisionPolicy:   "fail",
	}
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 5 * time.Second
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Duration(attempt*attempt) * 5 * time.Second
}

func collectCandidates(mediaPath string, seriesPath string, watchRoot string, artifacts []store.Artifact) ([]store.UploadCandidate, error) {
	candidates := make(map[string]store.UploadCandidate)
	add := func(localPath string, fileType string) error {
		localPath = filepath.Clean(localPath)
		if !isWithinPath(localPath, seriesPath) {
			return nil
		}
		if !isWithinPath(localPath, watchRoot) {
			return nil
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(watchRoot, localPath)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || relative == ".." {
			return fmt.Errorf("cannot create upload relative path for %s", localPath)
		}
		candidates[localPath] = store.UploadCandidate{
			LocalPath:    localPath,
			RelativePath: filepath.ToSlash(relative),
			FileType:     fileType,
			Size:         info.Size(),
			ModifiedAt:   info.ModTime(),
		}
		return nil
	}
	if err := add(mediaPath, "video"); err != nil {
		return nil, fmt.Errorf("collect media upload candidate: %w", err)
	}
	for _, artifact := range artifacts {
		if err := add(artifact.Path, artifact.Type); err != nil {
			return nil, fmt.Errorf("collect artifact upload candidate: %w", err)
		}
	}
	items := make([]store.UploadCandidate, 0, len(candidates))
	for _, item := range candidates {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		leftPath := strings.ToLower(items[left].RelativePath)
		rightPath := strings.ToLower(items[right].RelativePath)
		if leftPath == rightPath {
			return items[left].RelativePath < items[right].RelativePath
		}
		return leftPath < rightPath
	})
	return items, nil
}

func normalizeSeriesKey(watchDirID *int64, seriesPath string) string {
	prefix := "unmanaged"
	if watchDirID != nil {
		prefix = fmt.Sprintf("watch-%d", *watchDirID)
	}
	return prefix + ":" + strings.ToLower(filepath.Clean(seriesPath))
}

func isWithinPath(candidate string, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	return candidate == root || strings.HasPrefix(candidate, root+string(os.PathSeparator))
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
