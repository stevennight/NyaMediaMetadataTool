package upload

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	pathpkg "path"
	"strings"
	"sync"
	"time"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"

	"NyaMediaMetadataTool/internal/store"
)

const default115UserAgent = "Mozilla/5.0"

const (
	min115RequestInterval = 2 * time.Second
	max115RequestInterval = 5 * time.Second
	max115ListRetries     = 3
	list115PageSize       = 100
)

type cookie115Provider struct {
	client          *pan115.Pan115Client
	requestMu       sync.Mutex
	lastRequest     time.Time
	requestInterval func() time.Duration
}

func (m *Manager) defaultProviderFactory(ctx context.Context, target store.UploadBatchTarget) (Provider, error) {
	providerType := normalizeProviderType(target.ProviderType)
	m.providerMu.RLock()
	builder := m.builders[providerType]
	m.providerMu.RUnlock()
	if builder == nil {
		return nil, unsupportedProviderError(providerType)
	}
	return builder(ctx, target, func(ctx context.Context, key string) (string, error) {
		return m.store.GetUploadProviderSecret(ctx, target.ProviderID, key)
	})
}

func newCookie115Provider(cookieValue string, userAgent string) (*cookie115Provider, error) {
	credential := &pan115.Credential{}
	if err := credential.FromCookie(strings.TrimSpace(cookieValue)); err != nil {
		return nil, fmt.Errorf("parse 115 Cookie: %w", err)
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = default115UserAgent
	}
	client := pan115.New().SetUserAgent(userAgent)
	client.ImportCredential(credential)
	return &cookie115Provider{client: client}, nil
}

func (p *cookie115Provider) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < max115ListRetries; attempt++ {
		if err = p.waitRequest(ctx); err != nil {
			return err
		}
		err = check115FileAPI(ctx, p.client)
		if err == nil {
			return nil
		}
		if !isRetryable115Error(err) {
			break
		}
	}
	return fmt.Errorf("115 file API check failed: %w", err)
}

type http115StatusError struct {
	statusCode int
}

func (err *http115StatusError) Error() string {
	return fmt.Sprintf("115 file API returned HTTP %d", err.statusCode)
}

func check115FileAPI(ctx context.Context, client *pan115.Pan115Client) error {
	result := pan115.FileListResp{}
	response, err := client.NewRequest().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"aid":              "1",
			"cid":              "0",
			"o":                pan115.FileOrderByTime,
			"asc":              "1",
			"offset":           "0",
			"show_dir":         "1",
			"limit":            "1",
			"snap":             "0",
			"natsort":          "0",
			"record_open_time": "1",
			"format":           "json",
			"fc_mix":           "0",
		}).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8").
		Get(pan115.ApiFileList)
	if err != nil {
		return err
	}
	if response == nil {
		return errors.New("115 file API returned no response")
	}
	if response.IsError() && isRetryable115Status(response.StatusCode()) {
		return &http115StatusError{statusCode: response.StatusCode()}
	}
	if err := pan115.CheckErr(nil, &result, response); err != nil {
		return err
	}
	if !result.State {
		return errors.New("115 file API returned an invalid response")
	}
	return nil
}

func (p *cookie115Provider) List(ctx context.Context, remotePath string) ([]RemoteEntry, error) {
	dirID, err := p.resolveDirectory(ctx, remotePath)
	if err != nil {
		return nil, err
	}
	files, err := p.listFiles(ctx, dirID, remotePath)
	if err != nil {
		return nil, err
	}
	base := normalize115Path(remotePath)
	entries := make([]RemoteEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, RemoteEntry{
			ID:    file.FileID,
			Name:  file.Name,
			Path:  pathpkg.Join(base, file.Name),
			IsDir: file.IsDirectory,
			Size:  file.Size,
		})
	}
	return entries, nil
}

func (p *cookie115Provider) Upload(ctx context.Context, localPath string, remotePath string, size int64, collisionPolicy string) (RemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return RemoteFile{}, err
	}
	remotePath = normalize115Path(remotePath)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, fmt.Errorf("invalid 115 target path %q", remotePath)
	}
	parentID, err := p.ensureDirectory(ctx, pathpkg.Dir(remotePath))
	if err != nil {
		return RemoteFile{}, err
	}
	existing, found, err := p.findChild(ctx, parentID, name)
	if err != nil {
		return RemoteFile{}, err
	}
	if found {
		if existing.IsDirectory {
			return RemoteFile{}, fmt.Errorf("115 target path is a directory: %s", remotePath)
		}
		if existing.Size == size {
			return RemoteFile{ID: existing.FileID, Size: existing.Size}, nil
		}
		switch strings.ToLower(strings.TrimSpace(collisionPolicy)) {
		case "skip":
			return RemoteFile{ID: existing.FileID, Size: existing.Size}, nil
		case "fail":
			return RemoteFile{}, fmt.Errorf("115 target already exists with a different size: %s", remotePath)
		default:
			if err := p.waitRequest(ctx); err != nil {
				return RemoteFile{}, err
			}
			if err := p.client.Delete(existing.FileID); err != nil {
				return RemoteFile{}, fmt.Errorf("replace existing 115 file %s: %w", remotePath, err)
			}
		}
	}

	file, err := os.Open(localPath)
	if err != nil {
		return RemoteFile{}, err
	}
	defer file.Close()
	if err := p.waitRequest(ctx); err != nil {
		return RemoteFile{}, err
	}
	if err := p.client.RapidUploadOrByMultipart(parentID, name, size, file); err != nil {
		return RemoteFile{}, fmt.Errorf("115 upload %s: %w", remotePath, err)
	}
	return p.waitForFile(ctx, parentID, name, size)
}

func (p *cookie115Provider) ensureDirectory(ctx context.Context, remotePath string) (string, error) {
	remotePath = normalize115Path(remotePath)
	if remotePath == "/" {
		return "0", nil
	}
	currentID := "0"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		child, found, err := p.findChild(ctx, currentID, segment)
		if err != nil {
			return "", err
		}
		if found {
			if !child.IsDirectory {
				return "", fmt.Errorf("115 path component is a file: %s", segment)
			}
			currentID = child.FileID
			continue
		}
		if err := p.waitRequest(ctx); err != nil {
			return "", err
		}
		createdID, err := p.client.Mkdir(currentID, segment)
		if err == nil && strings.TrimSpace(createdID) != "" {
			currentID = createdID
			continue
		}
		// A concurrent uploader may have created this directory. Re-read before
		// treating Mkdir's error as a real failure.
		child, found, lookupErr := p.findChild(ctx, currentID, segment)
		if lookupErr == nil && found && child.IsDirectory {
			currentID = child.FileID
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create 115 directory %s: %w", segment, err)
		}
		return "", fmt.Errorf("create 115 directory %s returned no id", segment)
	}
	return currentID, nil
}

func (p *cookie115Provider) resolveDirectory(ctx context.Context, remotePath string) (string, error) {
	remotePath = normalize115Path(remotePath)
	if remotePath == "/" {
		return "0", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := p.waitRequest(ctx); err != nil {
		return "", err
	}
	result, err := p.client.DirName2CID(remotePath)
	if err != nil {
		return "", fmt.Errorf("resolve 115 directory %s: %w", remotePath, err)
	}
	id := fmt.Sprintf("%v", result.CategoryID)
	if id == "" || id == "0" {
		return "", fmt.Errorf("115 directory not found: %s", remotePath)
	}
	return id, nil
}

func (p *cookie115Provider) findChild(ctx context.Context, parentID string, name string) (pan115.File, bool, error) {
	files, err := p.listFiles(ctx, parentID, "parent "+parentID)
	if err != nil {
		return pan115.File{}, false, err
	}
	for _, item := range files {
		if item.Name == name {
			return item, true, nil
		}
	}
	return pan115.File{}, false, nil
}

func (p *cookie115Provider) listFiles(ctx context.Context, parentID string, providerPath string) ([]pan115.File, error) {
	collected := make([]pan115.File, 0, list115PageSize)
	for offset := int64(0); ; {
		var page *[]pan115.File
		var err error
		for attempt := 0; attempt < max115ListRetries; attempt++ {
			if err := p.waitRequest(ctx); err != nil {
				return nil, err
			}
			page, err = p.client.ListPage(parentID, offset, int64(list115PageSize))
			if err == nil {
				break
			}
			if !isRetryable115Error(err) || attempt == max115ListRetries-1 {
				return nil, fmt.Errorf("list 115 directory %s (offset=%d): %w", providerPath, offset, err)
			}
		}
		if page == nil {
			return nil, fmt.Errorf("list 115 directory %s returned no page", providerPath)
		}
		collected = append(collected, (*page)...)
		if len(*page) < list115PageSize {
			return collected, nil
		}
		offset += int64(len(*page))
	}
}

func (p *cookie115Provider) waitForFile(ctx context.Context, parentID string, name string, size int64) (RemoteFile, error) {
	for attempt := 0; attempt < 4; attempt++ {
		file, found, err := p.findChild(ctx, parentID, name)
		if err != nil {
			return RemoteFile{}, err
		}
		if found && !file.IsDirectory && file.Size == size {
			return RemoteFile{ID: file.FileID, Size: file.Size}, nil
		}
		if attempt == 3 {
			break
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RemoteFile{}, ctx.Err()
		case <-timer.C:
		}
	}
	return RemoteFile{}, errors.New("115 upload completed but remote file verification failed")
}

func (p *cookie115Provider) waitRequest(ctx context.Context) error {
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	if !p.lastRequest.IsZero() {
		interval := random115RequestInterval()
		if p.requestInterval != nil {
			interval = p.requestInterval()
		}
		waitFor := p.lastRequest.Add(interval).Sub(time.Now())
		if waitFor > 0 {
			timer := time.NewTimer(waitFor)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	p.lastRequest = time.Now()
	return nil
}

func random115RequestInterval() time.Duration {
	steps := int((max115RequestInterval-min115RequestInterval)/time.Second) + 1
	return min115RequestInterval + time.Duration(rand.IntN(steps))*time.Second
}

func isRetryable115Error(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var statusErr *http115StatusError
	if errors.As(err, &statusErr) {
		return isRetryable115Status(statusErr.statusCode)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"method not allowed", "too many", "rate limit", "waf", "gateway timeout", "service unavailable", "bad gateway", "timeout", "temporary", "connection reset", "connection refused", "unexpected eof", "eof"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isRetryable115Status(status int) bool {
	return status == http.StatusMethodNotAllowed ||
		status == http.StatusTooManyRequests ||
		(status >= http.StatusInternalServerError && status <= 599)
}

func normalize115Path(value string) string {
	clean := pathpkg.Clean("/" + strings.TrimSpace(value))
	if clean == "." {
		return "/"
	}
	return clean
}
