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
	"strconv"
	"strings"
	"sync"
	"time"

	pan115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"NyaMediaMetadataTool/internal/store"
)

const (
	fallback115AppVersion = "35.6.0.3"
	default115UserAgent   = "Mozilla/5.0 115Browser/" + fallback115AppVersion
)

const (
	min115RequestInterval = 250 * time.Millisecond
	max115RequestInterval = 750 * time.Millisecond
	max115ListRetries     = 3
	max115UploadAttempts  = 3
	list115PageSize       = 1150
)

var err115RemoteFileNotVisible = errors.New("115 uploaded file is not visible yet")

type cookie115Provider struct {
	client                  *pan115.Pan115Client
	configuredUserAgent     string
	appVersionEndpoint      string
	appVersionMu            sync.Mutex
	appVersion              string
	appVersionResolved      bool
	appVersionResolutionErr error
	newUploadCipher         func() (upload115Cipher, error)
	nowMilli                func() int64
	requestMu               sync.Mutex
	lastRequest             time.Time
	requestInterval         func() time.Duration
	directoryMu             sync.RWMutex
	directoryIDs            map[string]string
	uploadContent           func(context.Context, string, string, int64, *os.File) error
	lookupChild             func(context.Context, string, string) (pan115.File, bool, error)
	uploadRetryDelay        func(int) time.Duration
	ossHTTPClient           *http.Client
	ossTokenMu              sync.Mutex
	ossToken                *pan115.UploadOSSTokenResp
	ossTokenExpiresAt       time.Time
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
	userAgent = strings.TrimSpace(userAgent)
	configuredUserAgent := userAgent
	if userAgent == "" {
		userAgent = default115UserAgent
	}
	client := pan115.New().SetHttpClient(new115APIHTTPClient()).SetUserAgent(userAgent)
	client.ImportCredential(credential)
	return &cookie115Provider{
		client:              client,
		configuredUserAgent: configuredUserAgent,
		appVersionEndpoint:  pan115.ApiGetVersion,
		appVersion:          fallback115AppVersion,
		newUploadCipher:     newECDH115UploadCipher,
		nowMilli:            func() int64 { return pan115.NowMilli().ToInt64() },
		directoryIDs:        map[string]string{"/": "0"},
		ossHTTPClient:       new115OSSHTTPClient(),
	}, nil
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
		if attempt < max115ListRetries-1 {
			if waitErr := p.waitUploadRetry(ctx, attempt+1); waitErr != nil {
				return waitErr
			}
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
	existing, found, err := p.findChildMatchingSize(ctx, parentID, name, size)
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
			if err := p.delete115(ctx, existing.FileID); err != nil {
				return RemoteFile{}, fmt.Errorf("replace existing 115 file %s: %w", remotePath, err)
			}
		}
	}

	file, err := os.Open(localPath)
	if err != nil {
		return RemoteFile{}, err
	}
	defer file.Close()
	remote, err := p.uploadAndVerify(ctx, parentID, name, size, file)
	if err != nil {
		return RemoteFile{}, fmt.Errorf("115 upload %s: %w", remotePath, err)
	}
	return remote, nil
}

func (p *cookie115Provider) Verify(ctx context.Context, remotePath string, size int64) (RemoteFile, bool, error) {
	remotePath = normalize115Path(remotePath)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, false, fmt.Errorf("invalid 115 target path %q", remotePath)
	}
	parentID, err := p.ensureDirectory(ctx, pathpkg.Dir(remotePath))
	if err != nil {
		return RemoteFile{}, false, err
	}
	remoteFile, found, err := p.findChildMatchingSize(ctx, parentID, name, size)
	if err != nil {
		return RemoteFile{}, false, err
	}
	if !found || remoteFile.IsDirectory || remoteFile.Size != size {
		return RemoteFile{}, false, nil
	}
	return RemoteFile{ID: remoteFile.FileID, Size: remoteFile.Size}, true, nil
}

func (p *cookie115Provider) uploadAndVerify(ctx context.Context, parentID string, name string, size int64, file *os.File) (RemoteFile, error) {
	upload := p.rapidUploadOrByMultipart
	if p.uploadContent != nil {
		upload = p.uploadContent
	}
	var lastErr error
	for attempt := 1; attempt <= max115UploadAttempts; attempt++ {
		if _, err := file.Seek(0, 0); err != nil {
			return RemoteFile{}, fmt.Errorf("rewind local file: %w", err)
		}
		uploadErr := upload(ctx, parentID, name, size, file)
		if uploadErr == nil {
			remote, verifyErr := p.waitForFile(ctx, parentID, name, size)
			if verifyErr == nil {
				return remote, nil
			}
			if err := ctx.Err(); err != nil {
				return RemoteFile{}, err
			}
			// The upload endpoint already reported success. Do not replay a
			// successful PUT/init merely because the independent metadata lookup
			// is temporarily unavailable; the next target retry starts with a
			// collision lookup and will recognize the completed remote file.
			return RemoteFile{}, fmt.Errorf("verify remote file after successful upload: %w", verifyErr)
		} else {
			if err := ctx.Err(); err != nil {
				return RemoteFile{}, err
			}
			lastErr = uploadErr
			var uncertainCommit *uncertain115CommitError
			if errors.As(uploadErr, &uncertainCommit) {
				remote, verifyErr := p.waitForFile(ctx, parentID, name, size)
				if verifyErr == nil {
					return remote, nil
				}
				return RemoteFile{}, fmt.Errorf("%w; remote verification did not confirm completion: %v", uploadErr, verifyErr)
			}
			if !isRetryable115Error(uploadErr) {
				return RemoteFile{}, uploadErr
			}

			// A timed-out init, PUT, or callback may have completed remotely even
			// though its response never reached us. Check before retrying so the
			// retry remains idempotent from the user's point of view.
			remoteFile, found, lookupErr := p.findChildMatchingSize(ctx, parentID, name, size)
			if lookupErr != nil {
				return RemoteFile{}, fmt.Errorf("%w; remote check before retry also failed: %v", uploadErr, lookupErr)
			}
			if lookupErr == nil && found {
				if remoteFile.IsDirectory {
					return RemoteFile{}, fmt.Errorf("115 target became a directory: %s", name)
				}
				if remoteFile.Size == size {
					return RemoteFile{ID: remoteFile.FileID, Size: remoteFile.Size}, nil
				}
				return RemoteFile{}, fmt.Errorf("115 target appeared with a different size while retrying: %s", name)
			}
		}
		if attempt == max115UploadAttempts {
			break
		}
		if err := p.waitUploadRetry(ctx, attempt); err != nil {
			return RemoteFile{}, err
		}
	}
	return RemoteFile{}, fmt.Errorf("failed after %d attempts: %w", max115UploadAttempts, lastErr)
}

func (p *cookie115Provider) waitUploadRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt) * time.Second
	if p.uploadRetryDelay != nil {
		delay = p.uploadRetryDelay(attempt)
	}
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *cookie115Provider) ensureDirectory(ctx context.Context, remotePath string) (string, error) {
	remotePath = normalize115Path(remotePath)
	if cachedID, ok := p.cachedDirectoryID(remotePath); ok {
		return cachedID, nil
	}
	currentID := "0"
	currentPath := "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		currentPath = pathpkg.Join(currentPath, segment)
		if cachedID, ok := p.cachedDirectoryID(currentPath); ok {
			currentID = cachedID
			continue
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
			p.cacheDirectoryID(currentPath, currentID)
			continue
		}
		if err := p.waitRequest(ctx); err != nil {
			return "", err
		}
		createdID, err := p.mkdir115(ctx, currentID, segment)
		if err == nil && strings.TrimSpace(createdID) != "" {
			currentID = createdID
			p.cacheDirectoryID(currentPath, currentID)
			continue
		}
		// A concurrent uploader may have created this directory. Re-read before
		// treating Mkdir's error as a real failure.
		child, found, lookupErr := p.findChild(ctx, currentID, segment)
		if lookupErr == nil && found && child.IsDirectory {
			currentID = child.FileID
			p.cacheDirectoryID(currentPath, currentID)
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
	if cachedID, ok := p.cachedDirectoryID(remotePath); ok {
		return cachedID, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := p.waitRequest(ctx); err != nil {
		return "", err
	}
	result := pan115.APIGetDirIDResp{}
	response, err := p.client.NewRequest().
		SetContext(ctx).
		SetQueryParam("path", strings.TrimPrefix(remotePath, "/")).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8").
		Get(pan115.ApiDirName2CID)
	if err := pan115.CheckErr(err, &result, response); err != nil {
		return "", fmt.Errorf("resolve 115 directory %s: %w", remotePath, err)
	}
	id := fmt.Sprintf("%v", result.CategoryID)
	if id == "" || id == "0" {
		return "", fmt.Errorf("115 directory not found: %s", remotePath)
	}
	p.cacheDirectoryID(remotePath, id)
	return id, nil
}

func (p *cookie115Provider) mkdir115(ctx context.Context, parentID string, name string) (string, error) {
	result := pan115.MkdirResp{}
	response, err := p.client.NewRequest().
		SetContext(ctx).
		SetFormData(map[string]string{"pid": parentID, "cname": name}).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8").
		Post(pan115.ApiDirAdd)
	if err := pan115.CheckErr(err, &result, response); err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", result.CategoryID), nil
}

func (p *cookie115Provider) delete115(ctx context.Context, fileID string) error {
	result := pan115.BasicResp{}
	response, err := p.client.NewRequest().
		SetContext(ctx).
		SetFormData(map[string]string{"fid[0]": fileID}).
		SetResult(&result).
		ForceContentType("application/json;charset=UTF-8").
		Post(pan115.ApiFileDelete)
	return pan115.CheckErr(err, &result, response)
}

func (p *cookie115Provider) cachedDirectoryID(remotePath string) (string, bool) {
	remotePath = normalize115Path(remotePath)
	p.directoryMu.RLock()
	id, ok := p.directoryIDs[remotePath]
	p.directoryMu.RUnlock()
	return id, ok && strings.TrimSpace(id) != ""
}

func (p *cookie115Provider) cacheDirectoryID(remotePath string, id string) {
	remotePath = normalize115Path(remotePath)
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	p.directoryMu.Lock()
	if p.directoryIDs == nil {
		p.directoryIDs = map[string]string{"/": "0"}
	}
	p.directoryIDs[remotePath] = id
	p.directoryMu.Unlock()
}

func (p *cookie115Provider) findChild(ctx context.Context, parentID string, name string) (pan115.File, bool, error) {
	return p.findChildWithPreferredSize(ctx, parentID, name, -1)
}

func (p *cookie115Provider) findChildMatchingSize(ctx context.Context, parentID string, name string, size int64) (pan115.File, bool, error) {
	return p.findChildWithPreferredSize(ctx, parentID, name, size)
}

func (p *cookie115Provider) findChildWithPreferredSize(ctx context.Context, parentID string, name string, preferredSize int64) (pan115.File, bool, error) {
	if p.lookupChild != nil {
		return p.lookupChild(ctx, parentID, name)
	}
	var firstMatch pan115.File
	foundMatch := false
	for offset := int64(0); ; {
		page, err := p.listPage(ctx, parentID, "parent "+parentID, offset)
		if err != nil {
			return pan115.File{}, false, err
		}
		for _, item := range page {
			if item.Name == name {
				if preferredSize < 0 || (!item.IsDirectory && item.Size == preferredSize) {
					return item, true, nil
				}
				if !foundMatch {
					firstMatch = item
					foundMatch = true
				}
			}
		}
		if len(page) < list115PageSize {
			return firstMatch, foundMatch, nil
		}
		offset += int64(len(page))
	}
}

func (p *cookie115Provider) listFiles(ctx context.Context, parentID string, providerPath string) ([]pan115.File, error) {
	collected := make([]pan115.File, 0, list115PageSize)
	for offset := int64(0); ; {
		page, err := p.listPage(ctx, parentID, providerPath, offset)
		if err != nil {
			return nil, err
		}
		collected = append(collected, page...)
		if len(page) < list115PageSize {
			return collected, nil
		}
		offset += int64(len(page))
	}
}

func (p *cookie115Provider) listPage(ctx context.Context, parentID string, providerPath string, offset int64) ([]pan115.File, error) {
	var page *[]pan115.File
	var err error
	for attempt := 0; attempt < max115ListRetries; attempt++ {
		if err := p.waitRequest(ctx); err != nil {
			return nil, err
		}
		result := pan115.FileListResp{}
		response, requestErr := p.client.NewRequest().
			SetContext(ctx).
			SetQueryParams(map[string]string{
				"aid":              "1",
				"cid":              parentID,
				"o":                pan115.FileOrderByTime,
				"asc":              "1",
				"offset":           strconv.FormatInt(offset, 10),
				"show_dir":         "1",
				"limit":            strconv.Itoa(list115PageSize),
				"snap":             "0",
				"natsort":          "0",
				"record_open_time": "1",
				"format":           "json",
				"fc_mix":           "0",
			}).
			SetResult(&result).
			ForceContentType("application/json;charset=UTF-8").
			Get(pan115.ApiFileList)
		err = pan115.CheckErr(requestErr, &result, response)
		if err == nil && string(result.CategoryID) != parentID {
			err = fmt.Errorf("115 directory response CID %q does not match requested CID %q", result.CategoryID, parentID)
		}
		if err == nil {
			converted := make([]pan115.File, 0, len(result.Files))
			for index := range result.Files {
				converted = append(converted, *(&pan115.File{}).From(&result.Files[index]))
			}
			page = &converted
		}
		if err == nil {
			break
		}
		if !isRetryable115Error(err) || attempt == max115ListRetries-1 {
			return nil, fmt.Errorf("list 115 directory %s (offset=%d): %w", providerPath, offset, err)
		}
		if waitErr := p.waitUploadRetry(ctx, attempt+1); waitErr != nil {
			return nil, waitErr
		}
	}
	if page == nil {
		return nil, fmt.Errorf("list 115 directory %s returned no page", providerPath)
	}
	return *page, nil
}

func (p *cookie115Provider) waitForFile(ctx context.Context, parentID string, name string, size int64) (RemoteFile, error) {
	for attempt := 0; attempt < 4; attempt++ {
		file, found, err := p.findChildMatchingSize(ctx, parentID, name, size)
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
	return RemoteFile{}, fmt.Errorf("%w: %s", err115RemoteFileNotVisible, name)
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
	window := max115RequestInterval - min115RequestInterval
	if window <= 0 {
		return min115RequestInterval
	}
	return min115RequestInterval + time.Duration(rand.Int64N(int64(window)+1))
}

func isRetryable115Error(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var statusErr *http115StatusError
	if errors.As(err, &statusErr) {
		return isRetryable115Status(statusErr.statusCode)
	}
	var ossErr oss.ServiceError
	if errors.As(err, &ossErr) {
		return isRetryable115Status(ossErr.StatusCode)
	}
	var statusCodeErr interface{ Got() int }
	if errors.As(err, &statusCodeErr) {
		return isRetryable115Status(statusCodeErr.Got())
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
	return status == http.StatusRequestTimeout ||
		status == http.StatusMethodNotAllowed ||
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
