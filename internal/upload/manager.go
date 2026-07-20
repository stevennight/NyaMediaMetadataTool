package upload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/pipeline"
	"NyaMediaMetadataTool/internal/store"
)

const schedulerInterval = 2 * time.Second

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
	ID   string
	Size int64
}

// Provider is intentionally small. New provider types such as 115 Open,
// 123pan, and Baidu Pan only need to implement this contract.
type Provider interface {
	Check(ctx context.Context) error
	List(ctx context.Context, remotePath string) ([]RemoteEntry, error)
	Upload(ctx context.Context, localPath string, remotePath string, size int64, collisionPolicy string) (RemoteFile, error)
}

type ProviderFactory func(ctx context.Context, target store.UploadBatchTarget) (Provider, error)

type Manager struct {
	cfg        config.UploadConfig
	store      *store.Store
	logger     *slog.Logger
	factory    ProviderFactory
	builders   map[string]ProviderBuilder
	providers  map[string]ProviderDescriptor
	providerMu sync.RWMutex

	mu        sync.Mutex
	active    map[int64]context.CancelFunc
	authMu    sync.Mutex
	authFlows map[string]*cookie115AuthFlow
}

func New(cfg config.UploadConfig, st *store.Store, logger *slog.Logger) *Manager {
	manager := &Manager{
		cfg:       cfg,
		store:     st,
		logger:    logger,
		active:    make(map[int64]context.CancelFunc),
		authFlows: make(map[string]*cookie115AuthFlow),
		builders:  make(map[string]ProviderBuilder),
		providers: providerDescriptorMap(),
	}
	manager.registerBuiltInProviders()
	manager.factory = manager.defaultProviderFactory
	return manager
}

func NewWithFactory(cfg config.UploadConfig, st *store.Store, logger *slog.Logger, factory ProviderFactory) *Manager {
	manager := New(cfg, st, logger)
	if factory != nil {
		manager.factory = factory
	}
	return manager
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
	descriptor.Type = providerType
	descriptor.Implemented = true
	descriptor.SecretKeys = append([]string{}, descriptor.SecretKeys...)
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
		return newCookie115Provider(cookieValue, target.UserAgent)
	})
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
	return descriptor, ok
}

func (m *Manager) Enabled() bool {
	return m.cfg.Enabled
}

// RecordMediaProcessed is the handoff from the metadata pipeline. It only
// collects stable source and generated artifacts; network work happens later.
func (m *Manager) RecordMediaProcessed(ctx context.Context, task store.Task, media store.MediaFile) error {
	if !m.cfg.Enabled {
		return nil
	}
	artifacts, err := m.store.ListArtifactsByTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("list task artifacts: %w", err)
	}
	var watchDirID *int64
	if dir, findErr := m.store.FindWatchDirForPath(ctx, media.Path); findErr == nil {
		watchDirID = &dir.ID
	}
	seriesPath := pipeline.SeriesDirectory(media.Path)
	// Collect all known candidates first. The store applies the global profile
	// or a target-specific route profile when it creates transfers, so a route
	// can intentionally widen or narrow the global selection.
	files, err := collectCandidates(media.Path, seriesPath, artifacts, nil)
	if err != nil {
		return err
	}
	seriesKey := normalizeSeriesKey(watchDirID, seriesPath)
	batch, created, err := m.store.CollectUploadBatch(ctx, store.UploadCollectionInput{
		WatchDirID:          watchDirID,
		SeriesKey:           seriesKey,
		SeriesPath:          seriesPath,
		QuietPeriod:         m.cfg.QuietPeriod,
		DefaultIncludeTypes: m.cfg.IncludeTypes,
		Files:               files,
	})
	if err != nil {
		return fmt.Errorf("collect upload batch: %w", err)
	}
	if batch.ID != 0 {
		message := "upload batch updated"
		if created {
			message = "upload batch created"
		}
		m.logger.Info(message, "batchID", batch.ID, "series", batch.SeriesPath, "files", len(files))
	}
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	if !m.cfg.Enabled {
		<-ctx.Done()
		return nil
	}
	workers := m.cfg.Concurrency
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
		if _, err := m.store.SealDueUploadBatches(ctx, time.Now(), m.cfg.QuietPeriod); err != nil && !errors.Is(err, context.Canceled) {
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
		cancel()
		m.untrackTarget(target.ID)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(targetCtx.Err(), context.Canceled) {
			continue
		}
		m.logger.Warn("upload target failed", "targetID", target.ID, "provider", target.ProviderName, "error", err)
		if target.Attempts < m.cfg.MaxAttempts {
			delay := retryDelay(target.Attempts)
			if retryErr := m.store.RescheduleUploadTarget(ctx, target.ID, err.Error(), time.Now().Add(delay)); retryErr != nil {
				m.logger.Warn("reschedule upload target failed", "targetID", target.ID, "error", retryErr)
			}
			continue
		}
		if failErr := m.store.FailUploadTarget(ctx, target.ID, err.Error()); failErr != nil {
			m.logger.Warn("mark upload target failed", "targetID", target.ID, "error", failErr)
		}
	}
}

func (m *Manager) processTarget(ctx context.Context, target store.UploadBatchTarget) error {
	client, err := m.factory(ctx, target)
	if err != nil {
		return err
	}
	if err := client.Check(ctx); err != nil {
		return fmt.Errorf("check provider %s: %w", target.ProviderName, err)
	}
	transfers, err := m.store.ListUploadTransfersByTarget(ctx, target.ID)
	if err != nil {
		return err
	}
	for _, transfer := range transfers {
		if transfer.Status == store.UploadTransferCompleted {
			continue
		}
		if err := m.store.StartUploadTransfer(ctx, transfer.ID); err != nil {
			return fmt.Errorf("start transfer %d: %w", transfer.ID, err)
		}
		info, err := os.Stat(transfer.LocalPath)
		if err != nil {
			_ = m.store.FailUploadTransfer(ctx, transfer.ID, err.Error())
			return fmt.Errorf("stat local file %s: %w", transfer.LocalPath, err)
		}
		if info.IsDir() || info.Size() != transfer.BytesTotal {
			err := fmt.Errorf("local file changed after batch snapshot: %s", transfer.LocalPath)
			_ = m.store.FailUploadTransfer(ctx, transfer.ID, err.Error())
			return err
		}
		remote, err := client.Upload(ctx, transfer.LocalPath, transfer.RemotePath, transfer.BytesTotal, target.CollisionPolicy)
		if err != nil {
			_ = m.store.FailUploadTransfer(ctx, transfer.ID, err.Error())
			return fmt.Errorf("upload %s: %w", transfer.LocalPath, err)
		}
		if err := m.store.CompleteUploadTransfer(ctx, transfer.ID, remote.ID); err != nil {
			return err
		}
	}
	return m.store.CompleteUploadTarget(ctx, target.ID)
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
		ProviderID:      provider.ID,
		ProviderName:    provider.Name,
		ProviderType:    provider.Type,
		RemoteRoot:      provider.RemoteRoot,
		UserAgent:       provider.UserAgent,
		CollisionPolicy: provider.CollisionPolicy,
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

func collectCandidates(mediaPath string, seriesPath string, artifacts []store.Artifact, includeTypes []string) ([]store.UploadCandidate, error) {
	candidates := make(map[string]store.UploadCandidate)
	add := func(localPath string, fileType string) error {
		if !uploadTypeAllowed(includeTypes, fileType) {
			return nil
		}
		localPath = filepath.Clean(localPath)
		if !isWithinPath(localPath, seriesPath) {
			return nil
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(seriesPath, localPath)
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
