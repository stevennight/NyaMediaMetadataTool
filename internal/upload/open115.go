package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk115 "github.com/xhofe/115-sdk-go"

	"NyaMediaMetadataTool/internal/store"
)

const (
	open115ListPageSize       = int64(200)
	open115RequestInterval    = 500 * time.Millisecond
	open115PathInfoRetryDelay = 250 * time.Millisecond
	open115ChildrenCacheTTL   = 10 * time.Minute
	open115PathNotFoundCode   = int64(20018)
	maxOpen115UploadAttempts  = 3
)

type open115PathInfo struct {
	Size         string `json:"size"`
	PTime        string `json:"ptime"`
	UTime        string `json:"utime"`
	FileName     string `json:"file_name"`
	PickCode     string `json:"pick_code"`
	SHA1         string `json:"sha1"`
	FileID       string `json:"file_id"`
	FileCategory string `json:"file_category"`
}

type open115CachedNode struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size,omitempty"`
	SHA1     string `json:"sha1,omitempty"`
}

type open115CachedFile struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name"`
	Category string `json:"category"`
	PickCode string `json:"pickCode,omitempty"`
	SHA1     string `json:"sha1,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type open115ChildrenCache struct {
	Files []open115CachedFile `json:"files"`
}

type open115CacheStore interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string) error
	SetWithTTL(context.Context, string, string, time.Duration) error
	Delete(context.Context, string) error
}

type open115SDKAPI interface {
	UserInfo(context.Context) (*sdk115.UserInfoResp, error)
	GetFiles(context.Context, *sdk115.GetFilesReq) (*sdk115.GetFilesResp, error)
	Mkdir(context.Context, string, string) (*sdk115.MkdirResp, error)
	DelFile(context.Context, *sdk115.DelFileReq) ([]string, error)
	UploadInit(context.Context, *sdk115.UploadInitReq) (*sdk115.UploadInitResp, error)
	UploadGetToken(context.Context) (*sdk115.UploadGetTokenResp, error)
}

type open115API interface {
	open115SDKAPI
	GetInfoByPath(context.Context, string) (*open115PathInfo, error)
}

type open115Session struct {
	client   open115SDKAPI
	refresh  func(context.Context) (*sdk115.RefreshTokenResp, error)
	pathInfo func(context.Context, string) (*open115PathInfo, error)

	apiMu sync.Mutex
	mu    sync.RWMutex

	accessToken       string
	refreshToken      string
	expiresAt         string
	refreshInProgress bool
	onTokenRefreshed  func(string, string, string)
}

func newOpen115Session(accessToken, refreshToken, expiresAt, userAgent string, onTokenRefreshed func(string, string, string)) *open115Session {
	session := &open115Session{
		accessToken:      strings.TrimSpace(accessToken),
		refreshToken:     strings.TrimSpace(refreshToken),
		expiresAt:        strings.TrimSpace(expiresAt),
		onTokenRefreshed: onTokenRefreshed,
	}
	client := sdk115.New(
		sdk115.WithAccessToken(session.accessToken),
		sdk115.WithRefreshToken(session.refreshToken),
		sdk115.WithOnRefreshToken(session.handleSDKRefresh),
	)
	if strings.TrimSpace(userAgent) != "" {
		client.SetUserAgent(strings.TrimSpace(userAgent))
	}
	session.client = client
	session.refresh = client.RefreshToken
	session.pathInfo = func(ctx context.Context, providerPath string) (*open115PathInfo, error) {
		var info open115PathInfo
		_, err := client.AuthRequest(ctx, sdk115.ApiFsGetFolderInfo, http.MethodPost, &info, sdk115.ReqWithForm(sdk115.Form{"path": providerPath}))
		if err != nil {
			return nil, err
		}
		return &info, nil
	}
	return session
}

func (s *open115Session) handleSDKRefresh(accessToken, refreshToken string) {
	s.mu.Lock()
	s.accessToken = strings.TrimSpace(accessToken)
	if strings.TrimSpace(refreshToken) != "" {
		s.refreshToken = strings.TrimSpace(refreshToken)
	}
	s.expiresAt = ""
	updatedAccessToken := s.accessToken
	updatedRefreshToken := s.refreshToken
	refreshInProgress := s.refreshInProgress
	callback := s.onTokenRefreshed
	s.mu.Unlock()
	if !refreshInProgress && callback != nil {
		callback(updatedAccessToken, updatedRefreshToken, "")
	}
}

func (s *open115Session) setTokens(accessToken, refreshToken, expiresAt string) {
	s.mu.Lock()
	s.accessToken = strings.TrimSpace(accessToken)
	if strings.TrimSpace(refreshToken) != "" {
		s.refreshToken = strings.TrimSpace(refreshToken)
	}
	s.expiresAt = strings.TrimSpace(expiresAt)
	updatedAccessToken := s.accessToken
	updatedRefreshToken := s.refreshToken
	updatedExpiresAt := s.expiresAt
	callback := s.onTokenRefreshed
	s.mu.Unlock()
	if callback != nil {
		callback(updatedAccessToken, updatedRefreshToken, updatedExpiresAt)
	}
}

func (s *open115Session) matches(accessToken, refreshToken, expiresAt string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accessToken == strings.TrimSpace(accessToken) &&
		s.refreshToken == strings.TrimSpace(refreshToken) &&
		s.expiresAt == strings.TrimSpace(expiresAt)
}

func (s *open115Session) ensureAccessToken(ctx context.Context) error {
	s.mu.RLock()
	accessToken := s.accessToken
	refreshToken := s.refreshToken
	expiresAt := s.expiresAt
	s.mu.RUnlock()
	if open115TokenUsable(accessToken, expiresAt, time.Now()) {
		return nil
	}
	if refreshToken == "" {
		if accessToken == "" {
			return errors.New("115 Open access token is unavailable")
		}
		return errors.New("115 Open access token is expired and refresh token is required")
	}
	if s.refresh == nil {
		return errors.New("115 Open token refresh is unavailable")
	}
	s.mu.Lock()
	s.refreshInProgress = true
	s.mu.Unlock()
	result, err := s.refresh(ctx)
	s.mu.Lock()
	s.refreshInProgress = false
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("refresh 115 Open access token: %w", err)
	}
	if result == nil || strings.TrimSpace(result.AccessToken) == "" {
		return errors.New("refresh 115 Open access token returned no access token")
	}
	s.setTokens(result.AccessToken, result.RefreshToken, open115ExpiresAt(result.ExpiresIn, time.Now()))
	return nil
}

func open115TokenUsable(accessToken, expiresAt string, now time.Time) bool {
	if strings.TrimSpace(accessToken) == "" {
		return false
	}
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return true
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return now.Add(time.Minute).Before(expiry)
}

func open115ExpiresAt(expiresIn int64, now time.Time) string {
	if expiresIn <= 0 {
		return ""
	}
	return now.UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
}

func (s *open115Session) UserInfo(ctx context.Context) (*sdk115.UserInfoResp, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.UserInfo(ctx)
}

func (s *open115Session) GetFiles(ctx context.Context, request *sdk115.GetFilesReq) (*sdk115.GetFilesResp, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.GetFiles(ctx, request)
}

func (s *open115Session) GetInfoByPath(ctx context.Context, providerPath string) (*open115PathInfo, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	if s.pathInfo == nil {
		return nil, errors.New("115 Open path info is unavailable")
	}
	return s.pathInfo(ctx, providerPath)
}

func (s *open115Session) Mkdir(ctx context.Context, parentID, name string) (*sdk115.MkdirResp, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.Mkdir(ctx, parentID, name)
}

func (s *open115Session) DelFile(ctx context.Context, request *sdk115.DelFileReq) ([]string, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.DelFile(ctx, request)
}

func (s *open115Session) UploadInit(ctx context.Context, request *sdk115.UploadInitReq) (*sdk115.UploadInitResp, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.UploadInit(ctx, request)
}

func (s *open115Session) UploadGetToken(ctx context.Context) (*sdk115.UploadGetTokenResp, error) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if err := s.ensureAccessToken(ctx); err != nil {
		return nil, err
	}
	return s.client.UploadGetToken(ctx)
}

type open115Provider struct {
	client open115API

	requestGuard     *cookie115RequestGuard
	requestInterval  time.Duration
	waitReporter     func(string, time.Time)
	progressReporter func(int64)

	directoryMu  sync.RWMutex
	directoryIDs map[string]string
	cacheStore   open115CacheStore

	pathInfoRetryDelay time.Duration

	uploadContent    func(context.Context, string, string, int64, *os.File, *open115Digest) error
	lookupChild      func(context.Context, string, string) (sdk115.GetFilesResp_File, bool, error)
	uploadRetryDelay func(int) time.Duration

	ossHTTPClient     *http.Client
	ossTokenMu        sync.Mutex
	ossToken          *sdk115.UploadGetTokenResp
	ossTokenExpiresAt time.Time
}

func newOpen115Provider(session *open115Session, cacheStores ...open115CacheStore) (*open115Provider, error) {
	if session == nil || session.client == nil {
		return nil, errors.New("115 Open session is unavailable")
	}
	provider := &open115Provider{
		client:             session,
		requestGuard:       newCookie115RequestGuard(),
		requestInterval:    open115RequestInterval,
		directoryIDs:       map[string]string{"/": "0"},
		pathInfoRetryDelay: open115PathInfoRetryDelay,
		ossHTTPClient:      new115OSSHTTPClient(),
	}
	if len(cacheStores) > 0 {
		provider.cacheStore = cacheStores[0]
	}
	return provider, nil
}

func (p *open115Provider) Check(ctx context.Context) error {
	if err := p.waitRequest(ctx); err != nil {
		return err
	}
	_, err := p.client.UserInfo(ctx)
	if err != nil {
		return fmt.Errorf("check 115 Open account: %w", err)
	}
	return nil
}

func (p *open115Provider) List(ctx context.Context, remotePath string) ([]RemoteEntry, error) {
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
			ID:    file.Fid,
			Name:  file.Fn,
			Path:  pathpkg.Join(base, file.Fn),
			IsDir: file.Fc == "0",
			Size:  file.FS,
		})
	}
	return entries, nil
}

func (p *open115Provider) Upload(ctx context.Context, localPath, remotePath string, size int64, localSHA1, collisionPolicy string) (RemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return RemoteFile{}, err
	}
	remotePath = normalize115Path(remotePath)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, fmt.Errorf("invalid 115 Open target path %q", remotePath)
	}
	parentID, err := p.ensureDirectory(ctx, pathpkg.Dir(remotePath))
	if err != nil {
		return RemoteFile{}, err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return RemoteFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return RemoteFile{}, err
	}
	if info.Size() != size {
		return RemoteFile{}, fmt.Errorf("local file changed after batch snapshot: %s", localPath)
	}

	existing, found, err := p.findPathEntry(ctx, remotePath, parentID, name, -1)
	if err != nil {
		return RemoteFile{}, err
	}
	localSHA1 = strings.ToUpper(strings.TrimSpace(localSHA1))
	var digest *open115Digest
	ensureDigest := func() (*open115Digest, error) {
		if digest != nil {
			return digest, nil
		}
		resolved, digestErr := calculateOpen115Digest(ctx, file)
		if digestErr != nil {
			return nil, digestErr
		}
		if resolved.Size != size {
			return nil, fmt.Errorf("local file changed after batch snapshot: %s", localPath)
		}
		digest = resolved
		return digest, nil
	}

	intendedOutcome := store.UploadOutcomeCreated
	if found {
		if existing.Fc == "0" {
			return RemoteFile{}, fmt.Errorf("115 Open target path is a directory: %s", remotePath)
		}
		if existing.FS == size && localSHA1 == "" {
			resolved, digestErr := ensureDigest()
			if digestErr != nil {
				return RemoteFile{}, fmt.Errorf("hash local file for collision check: %w", digestErr)
			}
			localSHA1 = resolved.SHA1
		}
		decision, decisionErr := decideOpen115Collision(existing, size, localSHA1, collisionPolicy)
		if decisionErr != nil {
			return RemoteFile{}, fmt.Errorf("%w: %s", decisionErr, remotePath)
		}
		if !decision.replace {
			return RemoteFile{ID: existing.Fid, Size: existing.FS, SHA1: existing.Sha1, LocalSHA1: localSHA1, Outcome: decision.outcome}, nil
		}
		intendedOutcome = store.UploadOutcomeReplaced
		if err := p.deleteFile(ctx, remotePath, parentID, existing.Fid); err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("replace existing 115 Open file %s: %w", remotePath, err)}
		}
	}

	resolved, err := ensureDigest()
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("hash local file for upload: %w", err)}
	}
	localSHA1 = resolved.SHA1
	remote, err := p.uploadAndVerify(ctx, remotePath, parentID, name, size, file, resolved)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("115 Open upload %s: %w", remotePath, err)}
	}
	p.invalidateChildren(pathpkg.Dir(remotePath))
	remote.Outcome = intendedOutcome
	remote.LocalSHA1 = localSHA1
	if strings.TrimSpace(remote.SHA1) == "" {
		remote.SHA1 = localSHA1
	}
	return remote, nil
}

func decideOpen115Collision(existing sdk115.GetFilesResp_File, localSize int64, localSHA1, collisionPolicy string) (collision115Decision, error) {
	if existing.FS == localSize && strings.TrimSpace(localSHA1) != "" && strings.TrimSpace(existing.Sha1) != "" && strings.EqualFold(localSHA1, existing.Sha1) {
		return collision115Decision{outcome: store.UploadOutcomeUnchanged}, nil
	}
	switch strings.ToLower(strings.TrimSpace(collisionPolicy)) {
	case "skip":
		return collision115Decision{outcome: store.UploadOutcomeSkipped}, nil
	case "fail":
		return collision115Decision{}, errors.New("115 Open target already exists with different content")
	default:
		return collision115Decision{outcome: store.UploadOutcomeReplaced, replace: true}, nil
	}
}

func (p *open115Provider) Verify(ctx context.Context, remotePath string, size int64, localSHA1 string) (RemoteFile, bool, error) {
	remotePath = normalize115Path(remotePath)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, false, fmt.Errorf("invalid 115 Open target path %q", remotePath)
	}
	parentID, err := p.ensureDirectory(ctx, pathpkg.Dir(remotePath))
	if err != nil {
		return RemoteFile{}, false, err
	}
	remote, found, err := p.findPathEntry(ctx, remotePath, parentID, name, size)
	if err != nil || !found || remote.Fc == "0" || remote.FS != size {
		return RemoteFile{}, false, err
	}
	localSHA1 = strings.TrimSpace(localSHA1)
	if localSHA1 != "" && (strings.TrimSpace(remote.Sha1) == "" || !strings.EqualFold(localSHA1, remote.Sha1)) {
		return RemoteFile{}, false, nil
	}
	return RemoteFile{ID: remote.Fid, Size: remote.FS, SHA1: remote.Sha1, LocalSHA1: localSHA1, Outcome: store.UploadOutcomeCreated}, true, nil
}

func (p *open115Provider) uploadAndVerify(ctx context.Context, remotePath, parentID, name string, size int64, file *os.File, digest *open115Digest) (RemoteFile, error) {
	upload := p.uploadOpen115Content
	if p.uploadContent != nil {
		upload = p.uploadContent
	}
	var lastErr error
	for attempt := 1; attempt <= maxOpen115UploadAttempts; attempt++ {
		if _, err := file.Seek(0, 0); err != nil {
			return RemoteFile{}, fmt.Errorf("rewind local file: %w", err)
		}
		uploadErr := upload(ctx, parentID, name, size, file, digest)
		if uploadErr == nil {
			remote, verifyErr := p.waitForFile(ctx, remotePath, parentID, name, size)
			if verifyErr == nil && strings.TrimSpace(remote.SHA1) != "" && !strings.EqualFold(remote.SHA1, digest.SHA1) {
				verifyErr = errors.New("verified 115 Open file has different content")
			}
			if verifyErr == nil {
				return remote, nil
			}
			return RemoteFile{}, fmt.Errorf("verify remote file after successful upload: %w", verifyErr)
		}
		if err := ctx.Err(); err != nil {
			return RemoteFile{}, err
		}
		lastErr = uploadErr
		var uncertain *uncertain115CommitError
		if errors.As(uploadErr, &uncertain) {
			remote, verifyErr := p.waitForFile(ctx, remotePath, parentID, name, size)
			if verifyErr == nil && (strings.TrimSpace(remote.SHA1) == "" || strings.EqualFold(remote.SHA1, digest.SHA1)) {
				return remote, nil
			}
			return RemoteFile{}, fmt.Errorf("%w; remote verification did not confirm completion: %v", uploadErr, verifyErr)
		}
		if !isRetryable115Error(uploadErr) {
			return RemoteFile{}, uploadErr
		}
		remote, found, lookupErr := p.findPathEntry(ctx, remotePath, parentID, name, size)
		if lookupErr != nil {
			return RemoteFile{}, fmt.Errorf("%w; remote check before retry also failed: %v", uploadErr, lookupErr)
		}
		if found {
			if remote.Fc == "0" || remote.FS != size || (strings.TrimSpace(remote.Sha1) != "" && !strings.EqualFold(remote.Sha1, digest.SHA1)) {
				return RemoteFile{}, fmt.Errorf("115 Open target appeared with different content while retrying: %s", name)
			}
			return RemoteFile{ID: remote.Fid, Size: remote.FS, SHA1: remote.Sha1}, nil
		}
		if attempt < maxOpen115UploadAttempts {
			if err := p.waitUploadRetry(ctx, attempt); err != nil {
				return RemoteFile{}, err
			}
		}
	}
	return RemoteFile{}, fmt.Errorf("failed after %d attempts: %w", maxOpen115UploadAttempts, lastErr)
}

func (p *open115Provider) getInfoByPath(ctx context.Context, remotePath string) (*open115PathInfo, error) {
	info, err := p.getInfoByPathOnce(ctx, remotePath)
	if err == nil || !isOpen115PathNotFound(err) {
		return info, err
	}
	if p.pathInfoRetryDelay > 0 {
		timer := time.NewTimer(p.pathInfoRetryDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return p.getInfoByPathOnce(ctx, remotePath)
}

func (p *open115Provider) getInfoByPathOnce(ctx context.Context, remotePath string) (*open115PathInfo, error) {
	if err := p.waitRequest(ctx); err != nil {
		return nil, err
	}
	return p.client.GetInfoByPath(ctx, normalize115Path(remotePath))
}

func isOpen115PathNotFound(err error) bool {
	var apiErr *sdk115.Error
	return errors.As(err, &apiErr) && apiErr.Code == open115PathNotFoundCode
}

func open115FileFromPathInfo(info *open115PathInfo, parentID, fallbackName string) sdk115.GetFilesResp_File {
	if info == nil {
		return sdk115.GetFilesResp_File{}
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(info.Size), 10, 64)
	name := strings.TrimSpace(info.FileName)
	if name == "" {
		name = fallbackName
	}
	return sdk115.GetFilesResp_File{
		Fid:  strings.TrimSpace(info.FileID),
		Pid:  strings.TrimSpace(parentID),
		Fc:   strings.TrimSpace(info.FileCategory),
		Fn:   name,
		Pc:   strings.TrimSpace(info.PickCode),
		Sha1: strings.TrimSpace(info.SHA1),
		FS:   size,
	}
}

func (p *open115Provider) ensureDirectory(ctx context.Context, remotePath string) (string, error) {
	remotePath = normalize115Path(remotePath)
	if id, ok := p.cachedDirectoryID(remotePath); ok {
		return id, nil
	}
	if info, err := p.getInfoByPath(ctx, remotePath); err == nil {
		if strings.TrimSpace(info.FileID) == "" {
			return "", fmt.Errorf("115 Open path info returned no file_id for %s", remotePath)
		}
		if info.FileCategory != "0" {
			return "", fmt.Errorf("115 Open path is not a directory: %s", remotePath)
		}
		p.cacheDirectoryID(remotePath, info.FileID)
		return info.FileID, nil
	} else if !isOpen115PathNotFound(err) {
		return "", fmt.Errorf("resolve 115 Open directory %s: %w", remotePath, err)
	}

	currentID, currentPath := "0", "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		currentPath = pathpkg.Join(currentPath, segment)
		if id, ok := p.cachedDirectoryID(currentPath); ok {
			currentID = id
			continue
		}
		child, found, err := p.findChild(ctx, currentID, segment)
		if err != nil {
			return "", err
		}
		if found {
			if child.Fc != "0" {
				return "", fmt.Errorf("115 Open path component is a file: %s", segment)
			}
			currentID = child.Fid
			p.cacheDirectoryID(currentPath, currentID)
			continue
		}
		if err := p.waitRequest(ctx); err != nil {
			return "", err
		}
		created, createErr := p.client.Mkdir(ctx, currentID, segment)
		if createErr == nil && created != nil && strings.TrimSpace(created.FileID) != "" {
			currentID = created.FileID
			p.cacheDirectoryID(currentPath, currentID)
			p.invalidateChildren(pathpkg.Dir(currentPath))
			continue
		}
		child, found, lookupErr := p.findChild(ctx, currentID, segment)
		if lookupErr == nil && found && child.Fc == "0" {
			currentID = child.Fid
			p.cacheDirectoryID(currentPath, currentID)
			continue
		}
		if createErr != nil {
			return "", fmt.Errorf("create 115 Open directory %s: %w", segment, createErr)
		}
		return "", fmt.Errorf("create 115 Open directory %s returned no id", segment)
	}
	return currentID, nil
}

func (p *open115Provider) resolveDirectory(ctx context.Context, remotePath string) (string, error) {
	remotePath = normalize115Path(remotePath)
	if id, ok := p.cachedDirectoryID(remotePath); ok {
		return id, nil
	}
	if info, err := p.getInfoByPath(ctx, remotePath); err == nil {
		if strings.TrimSpace(info.FileID) == "" || info.FileCategory != "0" {
			return "", fmt.Errorf("115 Open directory not found: %s", remotePath)
		}
		p.cacheDirectoryID(remotePath, info.FileID)
		return info.FileID, nil
	} else if !isOpen115PathNotFound(err) {
		return "", fmt.Errorf("resolve 115 Open directory %s: %w", remotePath, err)
	}

	currentID, currentPath := "0", "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		currentPath = pathpkg.Join(currentPath, segment)
		child, found, err := p.findChild(ctx, currentID, segment)
		if err != nil {
			return "", err
		}
		if !found || child.Fc != "0" {
			return "", fmt.Errorf("115 Open directory not found: %s", remotePath)
		}
		currentID = child.Fid
		p.cacheDirectoryID(currentPath, currentID)
	}
	return currentID, nil
}

func (p *open115Provider) deleteFile(ctx context.Context, remotePath, parentID, fileID string) error {
	if err := p.waitRequest(ctx); err != nil {
		return err
	}
	_, err := p.client.DelFile(ctx, &sdk115.DelFileReq{FileIDs: fileID, ParentID: parentID})
	if err == nil {
		p.invalidateNode(remotePath)
		p.invalidateChildren(pathpkg.Dir(remotePath))
	}
	return err
}

func (p *open115Provider) findPathEntry(ctx context.Context, remotePath, parentID, name string, preferredSize int64) (sdk115.GetFilesResp_File, bool, error) {
	info, err := p.getInfoByPath(ctx, remotePath)
	if err == nil {
		item := open115FileFromPathInfo(info, parentID, name)
		if strings.TrimSpace(item.Fid) == "" {
			return sdk115.GetFilesResp_File{}, false, fmt.Errorf("115 Open path info returned no file_id for %s", remotePath)
		}
		return item, true, nil
	}
	if !isOpen115PathNotFound(err) {
		return sdk115.GetFilesResp_File{}, false, err
	}
	return p.findChildWithPreferredSize(ctx, parentID, name, preferredSize)
}

func (p *open115Provider) findChild(ctx context.Context, parentID, name string) (sdk115.GetFilesResp_File, bool, error) {
	return p.findChildWithPreferredSize(ctx, parentID, name, -1)
}

func (p *open115Provider) findChildMatchingSize(ctx context.Context, parentID, name string, size int64) (sdk115.GetFilesResp_File, bool, error) {
	return p.findChildWithPreferredSize(ctx, parentID, name, size)
}

func (p *open115Provider) findChildWithPreferredSize(ctx context.Context, parentID, name string, preferredSize int64) (sdk115.GetFilesResp_File, bool, error) {
	if p.lookupChild != nil {
		return p.lookupChild(ctx, parentID, name)
	}
	var first sdk115.GetFilesResp_File
	found := false
	for offset := int64(0); ; offset += open115ListPageSize {
		page, count, err := p.listPage(ctx, parentID, offset)
		if err != nil {
			return sdk115.GetFilesResp_File{}, false, err
		}
		for _, item := range page {
			if item.Fn == name {
				if preferredSize < 0 || (item.Fc != "0" && item.FS == preferredSize) {
					return item, true, nil
				}
				if !found {
					first, found = item, true
				}
			}
		}
		if offset+int64(len(page)) >= count || len(page) == 0 {
			return first, found, nil
		}
	}
}

func (p *open115Provider) listFiles(ctx context.Context, parentID, remotePath string) ([]sdk115.GetFilesResp_File, error) {
	remotePath = normalize115Path(remotePath)
	if files, ok := p.cachedChildren(ctx, remotePath); ok {
		return files, nil
	}
	var files []sdk115.GetFilesResp_File
	for offset := int64(0); ; offset += open115ListPageSize {
		page, count, err := p.listPage(ctx, parentID, offset)
		if err != nil {
			return nil, fmt.Errorf("list 115 Open directory %s: %w", remotePath, err)
		}
		files = append(files, page...)
		if int64(len(files)) >= count || len(page) == 0 {
			break
		}
	}
	for _, file := range files {
		if file.Fc == "0" && strings.TrimSpace(file.Fid) != "" {
			p.cacheDirectoryID(pathpkg.Join(remotePath, file.Fn), file.Fid)
		}
	}
	p.cacheChildren(ctx, remotePath, files)
	return files, nil
}

func (p *open115Provider) listPage(ctx context.Context, parentID string, offset int64) ([]sdk115.GetFilesResp_File, int64, error) {
	if err := p.waitRequest(ctx); err != nil {
		return nil, 0, err
	}
	result, err := p.client.GetFiles(ctx, &sdk115.GetFilesReq{CID: parentID, Limit: open115ListPageSize, Offset: offset, ASC: true, O: "file_name", ShowDir: true})
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, errors.New("115 Open file list returned no response")
	}
	return result.Data, result.Count, nil
}

func (p *open115Provider) waitForFile(ctx context.Context, remotePath, parentID, name string, size int64) (RemoteFile, error) {
	for attempt := 0; attempt < 4; attempt++ {
		file, found, err := p.findPathEntry(ctx, remotePath, parentID, name, size)
		if err != nil {
			return RemoteFile{}, err
		}
		if found && file.Fc != "0" && file.FS == size {
			return RemoteFile{ID: file.Fid, Size: file.FS, SHA1: file.Sha1}, nil
		}
	}
	return RemoteFile{}, fmt.Errorf("%w: %s", err115RemoteFileNotVisible, name)
}

func (p *open115Provider) cachedDirectoryID(remotePath string) (string, bool) {
	remotePath = normalize115Path(remotePath)
	p.directoryMu.RLock()
	id, ok := p.directoryIDs[remotePath]
	p.directoryMu.RUnlock()
	if ok && strings.TrimSpace(id) != "" {
		return id, true
	}
	if p.cacheStore == nil {
		return "", false
	}
	value, ok, err := p.cacheStore.Get(context.Background(), "node:"+remotePath)
	if err != nil || !ok {
		return "", false
	}
	var node open115CachedNode
	if err := json.Unmarshal([]byte(value), &node); err != nil || !node.IsDir || strings.TrimSpace(node.ID) == "" {
		return "", false
	}
	p.directoryMu.Lock()
	p.directoryIDs[remotePath] = node.ID
	p.directoryMu.Unlock()
	return node.ID, true
}

func (p *open115Provider) cacheDirectoryID(remotePath, id string) {
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
	if p.cacheStore == nil || remotePath == "/" {
		return
	}
	encoded, err := json.Marshal(open115CachedNode{ID: id, Path: remotePath, Name: pathpkg.Base(remotePath), IsDir: true})
	if err == nil {
		_ = p.cacheStore.Set(context.Background(), "node:"+remotePath, string(encoded))
	}
}

func (p *open115Provider) cachedChildren(ctx context.Context, remotePath string) ([]sdk115.GetFilesResp_File, bool) {
	if p.cacheStore == nil {
		return nil, false
	}
	value, ok, err := p.cacheStore.Get(ctx, "children:"+normalize115Path(remotePath))
	if err != nil || !ok {
		return nil, false
	}
	var cached open115ChildrenCache
	if err := json.Unmarshal([]byte(value), &cached); err != nil {
		return nil, false
	}
	files := make([]sdk115.GetFilesResp_File, 0, len(cached.Files))
	for _, file := range cached.Files {
		files = append(files, sdk115.GetFilesResp_File{
			Fid: file.ID, Pid: file.ParentID, Fn: file.Name, Fc: file.Category,
			Pc: file.PickCode, Sha1: file.SHA1, FS: file.Size,
		})
	}
	return files, true
}

func (p *open115Provider) cacheChildren(ctx context.Context, remotePath string, files []sdk115.GetFilesResp_File) {
	if p.cacheStore == nil {
		return
	}
	cachedFiles := make([]open115CachedFile, 0, len(files))
	for _, file := range files {
		cachedFiles = append(cachedFiles, open115CachedFile{
			ID: file.Fid, ParentID: file.Pid, Name: file.Fn, Category: file.Fc,
			PickCode: file.Pc, SHA1: file.Sha1, Size: file.FS,
		})
	}
	encoded, err := json.Marshal(open115ChildrenCache{Files: cachedFiles})
	if err == nil {
		_ = p.cacheStore.SetWithTTL(ctx, "children:"+normalize115Path(remotePath), string(encoded), open115ChildrenCacheTTL)
	}
}

func (p *open115Provider) invalidateNode(remotePath string) {
	remotePath = normalize115Path(remotePath)
	p.directoryMu.Lock()
	delete(p.directoryIDs, remotePath)
	p.directoryMu.Unlock()
	if p.cacheStore != nil {
		_ = p.cacheStore.Delete(context.Background(), "node:"+remotePath)
	}
}

func (p *open115Provider) invalidateChildren(remotePath string) {
	if p.cacheStore != nil {
		_ = p.cacheStore.Delete(context.Background(), "children:"+normalize115Path(remotePath))
	}
}

func (p *open115Provider) waitRequest(ctx context.Context) error {
	guard := p.requestGuard
	if guard == nil {
		guard = newCookie115RequestGuard()
		p.requestGuard = guard
	}
	return guard.wait(ctx, p.requestInterval, p.waitReporter)
}

func (p *open115Provider) waitUploadRetry(ctx context.Context, attempt int) error {
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

func (p *open115Provider) setWaitReporter(reporter func(string, time.Time)) {
	p.waitReporter = reporter
}

func (p *open115Provider) setProgressReporter(reporter func(int64)) {
	p.progressReporter = reporter
}
