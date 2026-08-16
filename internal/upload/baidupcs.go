package upload

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

const (
	baiduPCSBaseURL                 = "https://pan.baidu.com"
	baiduPCSAPIBaseURL              = "https://pcs.baidu.com"
	baiduPCSUploadBaseURL           = "https://c2.pcs.baidu.com"
	baiduPCSAppID                   = "250528"
	baiduPCSDefaultUserAgent        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/152.0.0.0 Safari/537.36"
	baiduPCSRequestTimeout          = 5 * time.Minute
	baiduPCSSliceSize               = 256 * 1024
	baiduPCSDataContentSize         = baiduPCSSliceSize
	baiduPCSMinBlockSize            = 4 * 1024 * 1024
	baiduPCSChunkConcurrency        = 3
	baiduPCSChunkRetryLimit         = 3
	baiduPCSWorkerStagger           = time.Second
	baiduPCSVerifyAttempts          = 8
	baiduPCSFileManagerTaskAttempts = 60
	baiduPCSVerifyRetryDelay        = time.Second
)

type baiduPCSProvider struct {
	cookie    string
	bdstoken  string
	userAgent string
	logger    *slog.Logger

	httpClient           *http.Client
	requestInterval      time.Duration
	requestMu            sync.Mutex
	lastRequest          time.Time
	requestSequence      atomic.Uint64
	sessionMu            sync.Mutex
	uk                   int64
	vipMu                sync.Mutex
	vipLoaded            bool
	vipIdentity          int64
	maxConcurrentFiles   int
	chunkConcurrency     int
	rapidUploadEnabled   bool
	preuploadBeforeRapid bool
	directoryMu          sync.Mutex
	confirmedDirectories map[string]struct{}
	progressReporter     func(int64)
}

type baiduPCSAPIResponse struct {
	Errno     int64  `json:"errno"`
	ErrMsg    string `json:"errmsg"`
	ErrorCode int64  `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (response baiduPCSAPIResponse) code() int64 {
	if response.Errno != 0 {
		return response.Errno
	}
	return response.ErrorCode
}

func (response baiduPCSAPIResponse) message() string {
	if strings.TrimSpace(response.ErrMsg) != "" {
		return strings.TrimSpace(response.ErrMsg)
	}
	return strings.TrimSpace(response.ErrorMsg)
}

type baiduPCSAPIError struct {
	StatusCode int
	Code       int64
	Message    string
}

func (err *baiduPCSAPIError) Error() string {
	return fmt.Sprintf("baidu pcs api error status=%d code=%d message=%s", err.StatusCode, err.Code, err.Message)
}

type baiduPCSRemotePathError struct {
	Expected string
	Actual   string
	FSID     string
}

func (err *baiduPCSRemotePathError) Error() string {
	return fmt.Sprintf("BaiduPCS rapid upload created unexpected path %q, want %q (fs_id=%s)", err.Actual, err.Expected, err.FSID)
}

type baiduPCSDirectoryPathError struct {
	Expected string
	Actual   string
	FSID     string
}

func (err *baiduPCSDirectoryPathError) Error() string {
	return fmt.Sprintf("BaiduPCS directory create returned unexpected path %q, want %q (fs_id=%s)", err.Actual, err.Expected, err.FSID)
}

func baiduPCSOnDup(collisionPolicy string) string {
	switch normalizeBaiduOpenCollisionPolicy(collisionPolicy) {
	case "replace":
		return "overwrite"
	case "skip", "fail":
		return "fail"
	default:
		return "fail"
	}
}

func baiduPCSTargetPath(remotePath string) string {
	parent := pathpkg.Dir(normalizeBaiduOpenPath(remotePath))
	if parent == "." || parent == "/" {
		return "/"
	}
	return strings.TrimRight(parent, "/") + "/"
}

type baiduPCSFileItem struct {
	FSID           json.Number `json:"fs_id"`
	Path           string      `json:"path"`
	ServerFilename string      `json:"server_filename"`
	Size           json.Number `json:"size"`
	MD5            string      `json:"md5"`
	IsDir          int         `json:"isdir"`
}

type baiduPCSListResponse struct {
	baiduPCSAPIResponse
	List    []baiduPCSFileItem `json:"list"`
	HasMore int                `json:"has_more"`
}

type baiduPCSPrecreateResponse struct {
	baiduPCSAPIResponse
	UploadID   string            `json:"uploadid"`
	ReturnType int               `json:"return_type"`
	BlockList  []int             `json:"block_list"`
	FSID       json.Number       `json:"fs_id"`
	Info       *baiduPCSFileItem `json:"info"`
}

type baiduPCSCreateResponse struct {
	baiduPCSAPIResponse
	FSID json.Number       `json:"fs_id"`
	Path string            `json:"path"`
	Info *baiduPCSFileItem `json:"info"`
}

type baiduPCSUploadResponse struct {
	baiduPCSAPIResponse
	MD5 string `json:"md5"`
}

type baiduPCSRapidResponse struct {
	baiduPCSAPIResponse
	ReturnType int               `json:"return_type"`
	FSID       json.Number       `json:"fs_id"`
	Path       string            `json:"path"`
	Info       *baiduPCSFileItem `json:"info"`
}

type baiduPCSFileManagerResponse struct {
	baiduPCSAPIResponse
	TaskID json.Number `json:"taskid"`
}

type baiduPCSFileManagerItem struct {
	ID      json.Number `json:"id"`
	Path    string      `json:"path"`
	NewName string      `json:"newname,omitempty"`
}

type baiduPCSShareTaskQueryResponse struct {
	baiduPCSAPIResponse
	TaskErrno json.Number `json:"task_errno"`
	Status    string      `json:"status"`
	ShowMsg   string      `json:"show_msg"`
}

type baiduPCSUserInfoResponse struct {
	baiduPCSAPIResponse
	Records []struct {
		UK json.Number `json:"uk"`
	} `json:"records"`
}

type baiduPCSWebTemplateResponse struct {
	baiduPCSAPIResponse
	Result map[string]json.RawMessage `json:"result"`
}

type baiduPCSLoginStatusResponse struct {
	baiduPCSAPIResponse
	LoginInfo struct {
		VIPIdentity json.RawMessage `json:"vip_identity"`
	} `json:"login_info"`
}

type baiduPCSUploadServer struct {
	Server string `json:"server"`
}

type baiduPCSLocateUploadResponse struct {
	baiduPCSAPIResponse
	Server  []string               `json:"server"`
	Servers []baiduPCSUploadServer `json:"servers"`
}

type baiduPCSProgressReader struct {
	reader io.Reader
	offset int64
	read   int64
	report func(int64)
}

type baiduPCSUploadProgress struct {
	mu       sync.Mutex
	parts    map[int]int64
	total    int64
	reporter func(int64)
}

func newBaiduPCSUploadProgress(reporter func(int64)) *baiduPCSUploadProgress {
	return &baiduPCSUploadProgress{parts: make(map[int]int64), reporter: reporter}
}

func (progress *baiduPCSUploadProgress) update(part int, bytesRead int64) {
	if progress == nil || bytesRead < 0 {
		return
	}
	progress.mu.Lock()
	previous := progress.parts[part]
	if bytesRead <= previous {
		progress.mu.Unlock()
		return
	}
	progress.parts[part] = bytesRead
	progress.total += bytesRead - previous
	total := progress.total
	reporter := progress.reporter
	progress.mu.Unlock()
	if reporter != nil {
		reporter(total)
	}
}

func (reader *baiduPCSProgressReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	if n > 0 {
		reader.read += int64(n)
		if reader.report != nil {
			reader.report(reader.offset + reader.read)
		}
	}
	return n, err
}

type baiduPCSDigest struct {
	Size      int64
	SHA1      string
	MD5       string
	SliceMD5  string
	ChunkMD5s []string
	BlockSize int64
}

func newBaiduPCSProvider(cookie, bdstoken, userAgent string, requestInterval time.Duration, loggers ...*slog.Logger) (*baiduPCSProvider, error) {
	return newBaiduPCSProviderWithOptions(cookie, bdstoken, userAgent, requestInterval, false, loggers...)
}

func newBaiduPCSProviderWithOptions(cookie, bdstoken, userAgent string, requestInterval time.Duration, preuploadBeforeRapid bool, loggers ...*slog.Logger) (*baiduPCSProvider, error) {
	if strings.TrimSpace(cookie) == "" {
		return nil, errors.New("BaiduPCS cookie is required")
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = baiduPCSDefaultUserAgent
	}
	var logger *slog.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	return &baiduPCSProvider{
		cookie:               strings.TrimSpace(cookie),
		bdstoken:             strings.TrimSpace(bdstoken),
		userAgent:            strings.TrimSpace(userAgent),
		logger:               logger,
		httpClient:           &http.Client{Timeout: baiduPCSRequestTimeout, Transport: transport},
		requestInterval:      requestInterval,
		chunkConcurrency:     baiduPCSChunkConcurrency,
		rapidUploadEnabled:   true,
		preuploadBeforeRapid: preuploadBeforeRapid,
		confirmedDirectories: map[string]struct{}{"/": {}},
	}, nil
}

func (p *baiduPCSProvider) setProgressReporter(reporter func(int64)) {
	p.progressReporter = reporter
}

func (p *baiduPCSProvider) usesContextProgress() {}

func (p *baiduPCSProvider) reportProgress(ctx context.Context, bytesTransferred int64) {
	if reporter := transferProgressReporter(ctx); reporter != nil {
		reporter(bytesTransferred)
		return
	}
	if p.progressReporter != nil {
		p.progressReporter(bytesTransferred)
	}
}

func (p *baiduPCSProvider) log(ctx context.Context, level slog.Level, message string, args ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Log(ctx, level, message, args...)
}

func (p *baiduPCSProvider) logError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, secret := range []string{p.cookie, p.bdstoken} {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		for _, encoded := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			message = strings.ReplaceAll(message, encoded, "<redacted>")
		}
	}
	return message
}

func baiduPCSOperation(parsed *url.URL) string {
	if parsed == nil {
		return "unknown"
	}
	if parsed.Path == "/rest/2.0/pcs/file" {
		if method := strings.TrimSpace(parsed.Query().Get("method")); method != "" {
			return method
		}
	}
	if parsed.Path == "/rest/2.0/pcs/superfile2" {
		return "superfile2"
	}
	if strings.HasPrefix(parsed.Path, "/api/") {
		return strings.TrimPrefix(parsed.Path, "/api/")
	}
	return parsed.Path
}

func baiduPCSRequestFields(requestID uint64, method string, parsed *url.URL, query url.Values, contentLength int64) []any {
	fields := []any{
		"client_request_id", requestID,
		"operation", baiduPCSOperation(parsed),
		"http_method", method,
		"host", parsed.Host,
		"api_path", parsed.Path,
	}
	if remotePath := strings.TrimSpace(query.Get("path")); remotePath != "" {
		fields = append(fields, "remote_path", remotePath)
	} else if directoryPath := strings.TrimSpace(query.Get("dir")); directoryPath != "" {
		fields = append(fields, "remote_path", directoryPath)
	}
	if part := strings.TrimSpace(query.Get("partseq")); part != "" {
		fields = append(fields, "part", part)
	}
	if uploadID := strings.TrimSpace(query.Get("uploadid")); uploadID != "" {
		fields = append(fields, "uploadid_present", true)
	}
	if contentLength > 0 {
		fields = append(fields, "request_bytes", contentLength)
	}
	return fields
}

func summarizeBaiduPCSBlocks(blocks []int) string {
	if len(blocks) == 0 {
		return "[]"
	}
	if len(blocks) <= 16 {
		values := make([]string, len(blocks))
		for index, block := range blocks {
			values[index] = strconv.Itoa(block)
		}
		return "[" + strings.Join(values, ",") + "]"
	}
	values := make([]string, 0, 9)
	for _, block := range blocks[:8] {
		values = append(values, strconv.Itoa(block))
	}
	values = append(values, "...")
	values = append(values, strconv.Itoa(blocks[len(blocks)-1]))
	return "[" + strings.Join(values, ",") + "]"
}

func (p *baiduPCSProvider) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.listFiles(ctx, "/"); err != nil {
		return fmt.Errorf("check BaiduPCS account: %w", err)
	}
	return nil
}

func (p *baiduPCSProvider) List(ctx context.Context, remotePath string) ([]RemoteEntry, error) {
	items, err := p.listFiles(ctx, remotePath)
	if err != nil {
		return nil, fmt.Errorf("list BaiduPCS directory %s: %w", normalizeBaiduOpenPath(remotePath), err)
	}
	entries := make([]RemoteEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, RemoteEntry{ID: item.ID, Name: item.Name, Path: item.Path, IsDir: item.IsDir, Size: item.Size})
	}
	return entries, nil
}

func (p *baiduPCSProvider) Upload(ctx context.Context, localPath, remotePath string, size int64, localSHA1, collisionPolicy string) (RemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return RemoteFile{}, err
	}
	remotePath = normalizeBaiduOpenPath(remotePath)
	p.log(ctx, slog.LevelInfo, "baidu pcs upload started",
		"local_path", localPath,
		"remote_path", remotePath,
		"size", size,
		"collision_policy", collisionPolicy,
		"preupload_before_rapid", p.preuploadBeforeRapid,
	)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, fmt.Errorf("invalid BaiduPCS target path %q", remotePath)
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
	if info.IsDir() || info.Size() != size {
		return RemoteFile{}, fmt.Errorf("local file changed after batch snapshot: %s", localPath)
	}
	p.reportProgress(ctx, 0)

	parentPath := pathpkg.Dir(remotePath)
	if err := p.ensureDirectory(ctx, parentPath); err != nil {
		return RemoteFile{}, err
	}
	existing, found, err := p.findEntry(ctx, parentPath, name)
	if err != nil {
		return RemoteFile{}, err
	}
	collisionPolicy = normalizeBaiduOpenCollisionPolicy(collisionPolicy)
	intendedOutcome := store.UploadOutcomeCreated
	if found {
		p.log(ctx, slog.LevelInfo, "baidu pcs collision check found remote entry",
			"remote_path", remotePath,
			"remote_id", existing.ID,
			"remote_size", existing.Size,
			"remote_md5_present", baiduOpenMD5IsValid(existing.MD5),
			"is_directory", existing.IsDir,
		)
		if existing.IsDir {
			return RemoteFile{}, fmt.Errorf("BaiduPCS target path is a directory: %s", remotePath)
		}
		switch collisionPolicy {
		case "skip":
			return RemoteFile{ID: existing.ID, Size: existing.Size, LocalSHA1: strings.TrimSpace(localSHA1), Outcome: store.UploadOutcomeSkipped}, nil
		case "fail":
			return RemoteFile{}, fmt.Errorf("BaiduPCS target already exists: %s", remotePath)
		case "replace":
			intendedOutcome = store.UploadOutcomeReplaced
		}
	}

	// rapidupload may create a timestamp-suffixed sibling when the target
	// already exists. The path-repair flow below removes that collision before
	// renaming the rapid-upload result to the requested name.
	rapidUploadAllowed := p.rapidUploadEnabled
	var digest *baiduPCSDigest
	var precreated *baiduPCSPrecreateResponse
	preuploadMode := p.preuploadBeforeRapid && rapidUploadAllowed && size > baiduPCSDataContentSize
	if preuploadMode {
		initialDigest, initialErr := calculateBaiduPCSInitialDigest(ctx, file)
		if initialErr != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("prepare local file for BaiduPCS preupload: %w", initialErr)}
		}
		if initialDigest.Size != size {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("local file changed before hash: %s", localPath)}
		}
		precreated, err = p.precreate(ctx, remotePath, info.ModTime(), initialDigest, collisionPolicy)
		if err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS precreate %s: %w", remotePath, err)}
		}
		if err := validateBaiduPCSPrecreateResponse(precreated); err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: err}
		}
		p.log(ctx, slog.LevelInfo, "baidu pcs precreate result",
			"remote_path", remotePath,
			"return_type", precreated.ReturnType,
			"block_count", len(precreated.BlockList),
			"block_list", summarizeBaiduPCSBlocks(precreated.BlockList),
			"uploadid_present", strings.TrimSpace(precreated.UploadID) != "",
			"fs_id", strings.TrimSpace(string(precreated.FSID)),
			"info_fs_id", baiduPCSInfoFSID(precreated.Info),
		)

		fullDigest, rapid, rapidErr, uploadErr := p.preuploadAndRapid(ctx, remotePath, precreated.UploadID, file, info.ModTime(), initialDigest)
		if fullDigest == nil {
			if rapidErr == nil {
				rapidErr = errors.New("BaiduPCS full digest was not produced")
			}
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("preupload BaiduPCS file %s: %w", remotePath, rapidErr)}
		}
		digest = fullDigest
		localSHA1 = digest.SHA1
		if rapidErr == nil {
			verified, verifyErr := p.verifyRemoteFile(ctx, rapid.ID, remotePath, size)
			if verifyErr != nil {
				return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("verify BaiduPCS rapid upload %s: %w", remotePath, verifyErr)}
			}
			verified.LocalSHA1 = localSHA1
			verified.SHA1 = localSHA1
			verified.Outcome = intendedOutcome
			p.reportProgress(ctx, size)
			return verified, nil
		}
		var pathErr *baiduPCSRemotePathError
		if errors.As(rapidErr, &pathErr) {
			if repaired, repairErr := p.repairUnexpectedRapidUploadPath(ctx, pathErr, remotePath, size); repairErr == nil {
				repaired.LocalSHA1 = localSHA1
				repaired.SHA1 = localSHA1
				repaired.Outcome = intendedOutcome
				p.reportProgress(ctx, size)
				return repaired, nil
			} else {
				return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("%w (automatic rename repair failed: %v)", rapidErr, repairErr)}
			}
		}
		if uploadErr != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS upload chunks for %s: %w", remotePath, uploadErr)}
		}
		p.log(ctx, slog.LevelWarn, "baidu pcs rapid upload missed; continuing preuploaded chunks",
			"remote_path", remotePath,
			"error", p.logError(rapidErr),
		)
	} else {
		digest, err = calculateBaiduPCSDigest(ctx, file)
		if err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("hash local file for BaiduPCS upload: %w", err)}
		}
		if digest.Size != size {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("local file changed after hash: %s", localPath)}
		}
		localSHA1 = digest.SHA1
		p.log(ctx, slog.LevelInfo, "baidu pcs digest calculated",
			"remote_path", remotePath,
			"size", digest.Size,
			"block_size", digest.BlockSize,
			"block_count", len(digest.ChunkMD5s),
			"sha1", digest.SHA1,
			"content_md5", digest.MD5,
			"slice_md5", digest.SliceMD5,
			"encoded_content_md5", encodeBaiduOpenMD5(digest.MD5),
			"encoded_slice_md5", encodeBaiduOpenMD5(digest.SliceMD5),
		)
		if found && existing.Size == size && baiduOpenMD5IsValid(existing.MD5) && strings.EqualFold(existing.MD5, digest.MD5) {
			p.log(ctx, slog.LevelInfo, "baidu pcs upload skipped because remote file is unchanged",
				"remote_path", remotePath,
				"remote_id", existing.ID,
			)
			return RemoteFile{ID: existing.ID, Size: existing.Size, SHA1: localSHA1, LocalSHA1: localSHA1, Outcome: store.UploadOutcomeUnchanged}, nil
		}

		precreated, err = p.precreate(ctx, remotePath, info.ModTime(), digest, collisionPolicy)
		if err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS precreate %s: %w", remotePath, err)}
		}
		if err := validateBaiduPCSPrecreateResponse(precreated); err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: err}
		}
		p.log(ctx, slog.LevelInfo, "baidu pcs precreate result",
			"remote_path", remotePath,
			"return_type", precreated.ReturnType,
			"block_count", len(precreated.BlockList),
			"block_list", summarizeBaiduPCSBlocks(precreated.BlockList),
			"uploadid_present", strings.TrimSpace(precreated.UploadID) != "",
			"fs_id", strings.TrimSpace(string(precreated.FSID)),
			"info_fs_id", baiduPCSInfoFSID(precreated.Info),
		)

		if rapidUploadAllowed && digest.Size > baiduPCSDataContentSize {
			rapid, rapidErr := p.rapidUploadFromFile(ctx, remotePath, precreated.UploadID, file, info.ModTime(), digest, 1)
			if rapidErr == nil {
				verified, verifyErr := p.verifyRemoteFile(ctx, rapid.ID, remotePath, size)
				if verifyErr != nil {
					return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("verify BaiduPCS rapid upload %s: %w", remotePath, verifyErr)}
				}
				verified.LocalSHA1 = localSHA1
				verified.SHA1 = localSHA1
				verified.Outcome = intendedOutcome
				p.reportProgress(ctx, size)
				return verified, nil
			}
			var pathErr *baiduPCSRemotePathError
			if errors.As(rapidErr, &pathErr) {
				if repaired, repairErr := p.repairUnexpectedRapidUploadPath(ctx, pathErr, remotePath, size); repairErr == nil {
					repaired.LocalSHA1 = localSHA1
					repaired.SHA1 = localSHA1
					repaired.Outcome = intendedOutcome
					p.reportProgress(ctx, size)
					return repaired, nil
				} else {
					return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("%w (automatic rename repair failed: %v)", rapidErr, repairErr)}
				}
			}
			p.log(ctx, slog.LevelWarn, "baidu pcs rapid upload missed; falling back to chunk upload",
				"remote_path", remotePath,
				"error", p.logError(rapidErr),
			)
		} else {
			reason := "small file or disabled"
			if found {
				reason = "target exists; using overwrite create"
			}
			p.log(ctx, slog.LevelInfo, "baidu pcs rapid upload skipped",
				"remote_path", remotePath,
				"size", digest.Size,
				"rapid_upload_enabled", rapidUploadAllowed,
				"reason", reason,
			)
		}

		parts := baiduPCSParts(digest.Size, digest.BlockSize)
		if len(parts) > 0 {
			serverURL, locateErr := p.locateUpload(ctx, remotePath, precreated.UploadID)
			if locateErr != nil {
				return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("locate BaiduPCS upload server: %w", locateErr)}
			}
			p.log(ctx, slog.LevelInfo, "baidu pcs upload server selected",
				"remote_path", remotePath,
				"server_host", baiduPCSURLHost(serverURL),
			)
			if err := p.uploadChunks(ctx, serverURL, remotePath, precreated.UploadID, parts, file, digest); err != nil {
				return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS upload chunks for %s: %w", remotePath, err)}
			}
		}
	}

	created, err := p.createFile(ctx, remotePath, size, precreated.UploadID, digest, 3, collisionPolicy)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS create %s: %w", remotePath, err)}
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs create result",
		"remote_path", remotePath,
		"rtype", 3,
		"fs_id", baiduPCSResponseFSID(created),
		"path", created.Path,
		"info_fs_id", baiduPCSInfoFSID(created.Info),
	)
	createdID := baiduPCSResponseFSID(created)
	if createdID == "" {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: errors.New("BaiduPCS create returned no fs_id")}
	}
	verified, err := p.verifyRemoteFile(ctx, createdID, remotePath, size)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("verify BaiduPCS upload %s: %w", remotePath, err)}
	}
	verified.LocalSHA1 = localSHA1
	verified.SHA1 = localSHA1
	verified.Outcome = intendedOutcome
	p.reportProgress(ctx, size)
	return verified, nil
}

func validateBaiduPCSPrecreateResponse(response *baiduPCSPrecreateResponse) error {
	if response == nil {
		return errors.New("BaiduPCS precreate returned an empty response")
	}
	if strings.TrimSpace(response.UploadID) == "" {
		return errors.New("BaiduPCS precreate returned no uploadid")
	}
	return nil
}

func baiduPCSParts(size, blockSize int64) []int {
	if size <= 0 || blockSize <= 0 {
		return nil
	}
	count := (size + blockSize - 1) / blockSize
	parts := make([]int, count)
	for index := range parts {
		parts[index] = index
	}
	return parts
}

type baiduPCSPreuploadDigestResult struct {
	digest *baiduPCSDigest
	err    error
}

// preuploadAndRapid follows the web uploader's race: ordinary chunks start
// while the complete digest is calculated, then rapidupload is attempted as
// soon as that digest is ready. A rapid miss leaves the chunk workers running
// so the normal create path can reuse the data already sent.
func (p *baiduPCSProvider) preuploadAndRapid(ctx context.Context, remotePath, uploadID string, file *os.File, modTime time.Time, initialDigest *baiduPCSDigest) (*baiduPCSDigest, RemoteFile, error, error) {
	parts := baiduPCSParts(initialDigest.Size, initialDigest.BlockSize)
	serverURL, err := p.locateUpload(ctx, remotePath, uploadID)
	if err != nil {
		return nil, RemoteFile{}, nil, fmt.Errorf("locate BaiduPCS upload server: %w", err)
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs upload server selected",
		"remote_path", remotePath,
		"server_host", baiduPCSURLHost(serverURL),
		"preupload", true,
	)

	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	digestResults := make(chan baiduPCSPreuploadDigestResult, 1)
	go func() {
		digest, digestErr := calculateBaiduPCSDigest(ctx, file)
		digestResults <- baiduPCSPreuploadDigestResult{digest: digest, err: digestErr}
	}()
	uploadResults := make(chan error, 1)
	go func() {
		uploadResults <- p.uploadChunks(uploadCtx, serverURL, remotePath, uploadID, parts, file, initialDigest)
	}()

	var fullDigest *baiduPCSDigest
	var rapid RemoteFile
	var rapidErr error
	var uploadErr error
	digestDone := false
	uploadDone := false
	for !digestDone || !uploadDone {
		select {
		case result := <-digestResults:
			digestDone = true
			if result.err != nil {
				cancel()
				uploadErr = <-uploadResults
				return nil, RemoteFile{}, result.err, uploadErr
			}
			fullDigest = result.digest
			if fullDigest == nil {
				cancel()
				uploadErr = <-uploadResults
				return nil, RemoteFile{}, errors.New("BaiduPCS full digest was empty"), uploadErr
			}
			if fullDigest.Size != initialDigest.Size {
				cancel()
				uploadErr = <-uploadResults
				return nil, RemoteFile{}, fmt.Errorf("local file changed while hashing: size=%d want=%d", fullDigest.Size, initialDigest.Size), uploadErr
			}
			p.log(ctx, slog.LevelInfo, "baidu pcs digest calculated",
				"remote_path", remotePath,
				"size", fullDigest.Size,
				"block_size", fullDigest.BlockSize,
				"block_count", len(fullDigest.ChunkMD5s),
				"sha1", fullDigest.SHA1,
				"content_md5", fullDigest.MD5,
				"slice_md5", fullDigest.SliceMD5,
				"encoded_content_md5", encodeBaiduOpenMD5(fullDigest.MD5),
				"encoded_slice_md5", encodeBaiduOpenMD5(fullDigest.SliceMD5),
				"preupload_complete", true,
			)
			rapid, rapidErr = p.rapidUploadFromFile(ctx, remotePath, uploadID, file, modTime, fullDigest, 1)
			if rapidErr == nil {
				cancel()
				uploadErr = <-uploadResults
				return fullDigest, rapid, nil, nil
			}
			var pathErr *baiduPCSRemotePathError
			if errors.As(rapidErr, &pathErr) {
				cancel()
				uploadErr = <-uploadResults
				return fullDigest, RemoteFile{}, rapidErr, uploadErr
			}
			p.log(ctx, slog.LevelWarn, "baidu pcs rapid upload missed; waiting for preuploaded chunks",
				"remote_path", remotePath,
				"error", p.logError(rapidErr),
			)
		case uploadErr = <-uploadResults:
			uploadDone = true
		case <-ctx.Done():
			cancel()
			if !uploadDone {
				uploadErr = <-uploadResults
				uploadDone = true
			}
			if !digestDone {
				result := <-digestResults
				fullDigest = result.digest
				digestDone = true
			}
			return fullDigest, RemoteFile{}, ctx.Err(), uploadErr
		}
	}
	return fullDigest, rapid, rapidErr, uploadErr
}

func calculateBaiduPCSInitialDigest(ctx context.Context, file *os.File) (*baiduPCSDigest, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	blockSize := int64(baiduPCSMinBlockSize)
	blockCount := (info.Size() + blockSize - 1) / blockSize
	initialBlockCount := blockCount
	if initialBlockCount > 2 {
		initialBlockCount = 2
	}
	chunkMD5s := make([]string, 0, initialBlockCount)
	sliceMD5 := md5.New()
	chunkBuffer := make([]byte, blockSize)
	var sliceRemaining int64 = baiduPCSSliceSize
	for index := int64(0); index < initialBlockCount; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, readErr := io.ReadFull(file, chunkBuffer)
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			readErr = nil
		}
		if readErr != nil {
			return nil, readErr
		}
		if read == 0 {
			break
		}
		chunk := chunkBuffer[:read]
		chunkHash := md5.Sum(chunk)
		chunkMD5s = append(chunkMD5s, fmt.Sprintf("%x", chunkHash))
		if sliceRemaining > 0 {
			sliceRead := int64(read)
			if sliceRead > sliceRemaining {
				sliceRead = sliceRemaining
			}
			if _, err := sliceMD5.Write(chunk[:sliceRead]); err != nil {
				return nil, err
			}
			sliceRemaining -= sliceRead
		}
		if read < len(chunkBuffer) {
			break
		}
	}
	return &baiduPCSDigest{
		Size:      info.Size(),
		SliceMD5:  fmt.Sprintf("%x", sliceMD5.Sum(nil)),
		ChunkMD5s: chunkMD5s,
		BlockSize: blockSize,
	}, nil
}

func calculateBaiduPCSDigest(ctx context.Context, file *os.File) (*baiduPCSDigest, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	blockSize := int64(baiduPCSMinBlockSize)
	fullMD5 := md5.New()
	fullSHA1 := sha1.New()
	sliceMD5 := md5.New()
	chunkMD5s := make([]string, 0, (info.Size()+blockSize-1)/blockSize)
	chunkBuffer := make([]byte, blockSize)
	var size int64
	var sliceRemaining int64 = baiduPCSSliceSize
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, readErr := io.ReadFull(file, chunkBuffer)
		if readErr == io.ErrUnexpectedEOF {
			readErr = nil
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		chunk := chunkBuffer[:read]
		chunkHash := md5.Sum(chunk)
		chunkMD5s = append(chunkMD5s, fmt.Sprintf("%x", chunkHash))
		if _, err := fullMD5.Write(chunk); err != nil {
			return nil, err
		}
		if _, err := fullSHA1.Write(chunk); err != nil {
			return nil, err
		}
		if sliceRemaining > 0 {
			sliceRead := int64(read)
			if sliceRead > sliceRemaining {
				sliceRead = sliceRemaining
			}
			if _, err := sliceMD5.Write(chunk[:sliceRead]); err != nil {
				return nil, err
			}
			sliceRemaining -= sliceRead
		}
		size += int64(read)
		if read < len(chunkBuffer) {
			break
		}
	}
	return &baiduPCSDigest{
		Size:      size,
		SHA1:      strings.ToUpper(fmt.Sprintf("%x", fullSHA1.Sum(nil))),
		MD5:       fmt.Sprintf("%x", fullMD5.Sum(nil)),
		SliceMD5:  fmt.Sprintf("%x", sliceMD5.Sum(nil)),
		ChunkMD5s: chunkMD5s,
		BlockSize: blockSize,
	}, nil
}

func (p *baiduPCSProvider) ensureDirectory(ctx context.Context, remotePath string) error {
	remotePath = normalizeBaiduOpenPath(remotePath)
	if remotePath == "/" {
		return nil
	}
	p.directoryMu.Lock()
	defer p.directoryMu.Unlock()
	if p.confirmedDirectories == nil {
		p.confirmedDirectories = map[string]struct{}{"/": {}}
	}
	if _, ok := p.confirmedDirectories[remotePath]; ok {
		p.log(ctx, slog.LevelInfo, "baidu pcs directory already confirmed", "remote_path", remotePath)
		return nil
	}
	currentPath := "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		nextPath := normalizeBaiduOpenPath(pathpkg.Join(currentPath, segment))
		if _, ok := p.confirmedDirectories[nextPath]; ok {
			currentPath = nextPath
			continue
		}
		entry, found, err := p.findEntry(ctx, currentPath, segment)
		if err != nil {
			return fmt.Errorf("resolve BaiduPCS directory %s: %w", currentPath, err)
		}
		if found {
			if !entry.IsDir {
				return fmt.Errorf("BaiduPCS path component is a file: %s", nextPath)
			}
			p.confirmedDirectories[nextPath] = struct{}{}
			currentPath = nextPath
			continue
		}
		if err := p.createDirectory(ctx, nextPath); err != nil {
			created, foundAfterError, lookupErr := p.findEntry(ctx, currentPath, segment)
			if lookupErr == nil && foundAfterError && created.IsDir {
				p.confirmedDirectories[nextPath] = struct{}{}
				currentPath = nextPath
				continue
			}
			return fmt.Errorf("create BaiduPCS directory %s: %w", nextPath, err)
		}
		p.confirmedDirectories[nextPath] = struct{}{}
		currentPath = nextPath
	}
	p.confirmedDirectories[remotePath] = struct{}{}
	return nil
}

func (p *baiduPCSProvider) findEntry(ctx context.Context, directoryPath, name string) (baiduPCSListedEntry, bool, error) {
	entries, err := p.listFiles(ctx, directoryPath)
	if err != nil {
		return baiduPCSListedEntry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name || pathpkg.Base(entry.Path) == name {
			return entry, true, nil
		}
	}
	return baiduPCSListedEntry{}, false, nil
}

type baiduPCSListedEntry struct {
	ID    string
	Name  string
	Path  string
	IsDir bool
	Size  int64
	MD5   string
}

func (p *baiduPCSProvider) listFiles(ctx context.Context, directoryPath string) ([]baiduPCSListedEntry, error) {
	directoryPath = normalizeBaiduOpenPath(directoryPath)
	const pageSize = 100
	entries := make([]baiduPCSListedEntry, 0)
	for page := 1; ; page++ {
		query, err := p.webQuery(ctx, true)
		if err != nil {
			return nil, err
		}
		query.Set("dir", directoryPath)
		query.Set("order", "name")
		query.Set("desc", "0")
		query.Set("num", strconv.Itoa(pageSize))
		query.Set("page", strconv.Itoa(page))
		var response baiduPCSListResponse
		if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/api/list", query, nil, "", 0, false, &response); err != nil {
			return nil, err
		}
		p.log(ctx, slog.LevelInfo, "baidu pcs list result",
			"remote_path", directoryPath,
			"page", page,
			"item_count", len(response.List),
			"has_more", response.HasMore != 0,
		)
		for _, item := range response.List {
			name := strings.TrimSpace(item.ServerFilename)
			itemPath := normalizeBaiduOpenPath(item.Path)
			if name == "" && itemPath != "/" {
				name = pathpkg.Base(itemPath)
			}
			if itemPath == "/" || name == "" {
				itemPath = normalizeBaiduOpenPath(pathpkg.Join(directoryPath, name))
			}
			entries = append(entries, baiduPCSListedEntry{
				ID:    strings.TrimSpace(string(item.FSID)),
				Name:  name,
				Path:  itemPath,
				IsDir: item.IsDir != 0,
				Size:  parseBaiduOpenInt64(string(item.Size)),
				MD5:   decodeBaiduOpenMD5(item.MD5),
			})
		}
		if response.HasMore == 0 && len(response.List) < pageSize {
			break
		}
		if len(response.List) == 0 {
			break
		}
	}
	return entries, nil
}

func (p *baiduPCSProvider) createDirectory(ctx context.Context, directoryPath string) error {
	directoryPath = normalizeBaiduOpenPath(directoryPath)
	query, err := p.webQuery(ctx, true)
	if err != nil {
		return err
	}
	query.Set("rtype", "1")
	form := url.Values{}
	form.Set("path", directoryPath)
	form.Set("target_path", baiduPCSTargetPath(directoryPath))
	form.Set("size", "0")
	form.Set("isdir", "1")
	form.Set("rtype", "1")
	form.Set("ondup", "fail")
	var response baiduPCSCreateResponse
	encoded := form.Encode()
	p.log(ctx, slog.LevelInfo, "baidu pcs directory create started", "remote_path", directoryPath, "rtype", 1)
	err = p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/create", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response)
	if err != nil {
		return err
	}
	actualPath := strings.TrimSpace(response.Path)
	if response.Info != nil && strings.TrimSpace(response.Info.Path) != "" {
		actualPath = strings.TrimSpace(response.Info.Path)
	}
	fsID := baiduPCSResponseFSID(&response)
	p.log(ctx, slog.LevelInfo, "baidu pcs directory create result",
		"remote_path", directoryPath,
		"actual_path", actualPath,
		"fs_id", fsID,
		"rtype", 1,
	)
	if actualPath != "" {
		actualPath = normalizeBaiduOpenPath(actualPath)
		if actualPath != directoryPath {
			return &baiduPCSDirectoryPathError{Expected: directoryPath, Actual: actualPath, FSID: fsID}
		}
		return nil
	}

	parentPath := pathpkg.Dir(directoryPath)
	name := pathpkg.Base(directoryPath)
	entry, found, lookupErr := p.findEntry(ctx, parentPath, name)
	if lookupErr != nil {
		return fmt.Errorf("BaiduPCS directory create returned no path and verification failed: %w", lookupErr)
	}
	if !found {
		return fmt.Errorf("BaiduPCS directory create returned no path and directory is not visible: %s", directoryPath)
	}
	if !entry.IsDir {
		return fmt.Errorf("BaiduPCS directory create returned a file at %s", directoryPath)
	}
	actualPath = normalizeBaiduOpenPath(entry.Path)
	if actualPath != directoryPath {
		return &baiduPCSDirectoryPathError{Expected: directoryPath, Actual: actualPath, FSID: entry.ID}
	}
	return nil
}

func (p *baiduPCSProvider) precreate(ctx context.Context, remotePath string, modTime time.Time, digest *baiduPCSDigest, collisionPolicy string) (*baiduPCSPrecreateResponse, error) {
	initialBlockCount := len(digest.ChunkMD5s)
	if initialBlockCount > 2 {
		initialBlockCount = 2
	}
	blockList, err := json.Marshal(digest.ChunkMD5s[:initialBlockCount])
	if err != nil {
		return nil, err
	}
	query, err := p.webQuery(ctx, true)
	if err != nil {
		return nil, err
	}
	query.Set("rtype", "1")
	form := url.Values{}
	form.Set("path", normalizeBaiduOpenPath(remotePath))
	form.Set("target_path", baiduPCSTargetPath(remotePath))
	form.Set("autoinit", "1")
	form.Set("local_mtime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("block_list", string(blockList))
	form.Set("ondup", baiduPCSOnDup(collisionPolicy))
	var response baiduPCSPrecreateResponse
	encoded := form.Encode()
	p.log(ctx, slog.LevelInfo, "baidu pcs precreate started",
		"remote_path", remotePath,
		"size", digest.Size,
		"block_count", initialBlockCount,
	)
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/precreate", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (p *baiduPCSProvider) createFile(ctx context.Context, remotePath string, size int64, uploadID string, digest *baiduPCSDigest, rtype int, collisionPolicy string) (*baiduPCSCreateResponse, error) {
	blockList, err := json.Marshal(digest.ChunkMD5s)
	if err != nil {
		return nil, err
	}
	query, err := p.webQuery(ctx, true)
	if err != nil {
		return nil, err
	}
	query.Set("rtype", strconv.Itoa(rtype))
	form := url.Values{}
	form.Set("uploadid", strings.TrimSpace(uploadID))
	form.Set("path", normalizeBaiduOpenPath(remotePath))
	form.Set("target_path", baiduPCSTargetPath(remotePath))
	form.Set("size", strconv.FormatInt(size, 10))
	form.Set("isdir", "0")
	form.Set("rtype", strconv.Itoa(rtype))
	form.Set("block_list", string(blockList))
	form.Set("ondup", baiduPCSOnDup(collisionPolicy))
	var response baiduPCSCreateResponse
	encoded := form.Encode()
	p.log(ctx, slog.LevelInfo, "baidu pcs file create started",
		"remote_path", remotePath,
		"size", size,
		"rtype", rtype,
		"block_count", len(digest.ChunkMD5s),
		"uploadid_present", strings.TrimSpace(uploadID) != "",
	)
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/create", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (p *baiduPCSProvider) locateUpload(ctx context.Context, remotePath, uploadID string) (string, error) {
	query := url.Values{}
	query.Set("method", "locateupload")
	query.Set("upload_version", "2.0")
	query.Set("app_id", baiduPCSAppID)
	query.Set("path", normalizeBaiduOpenPath(remotePath))
	query.Set("uploadid", strings.TrimSpace(uploadID))
	var response baiduPCSLocateUploadResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSAPIBaseURL+"/rest/2.0/pcs/file", query, nil, "", 0, false, &response); err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs locateupload failed; using default upload server",
			"remote_path", remotePath,
			"error", p.logError(err),
		)
		return baiduPCSUploadBaseURL + "/rest/2.0/pcs/superfile2", nil
	}
	serverCount := len(response.Servers) + len(response.Server)
	p.log(ctx, slog.LevelInfo, "baidu pcs locateupload result",
		"remote_path", remotePath,
		"server_count", serverCount,
	)
	for _, candidate := range response.Servers {
		server := strings.TrimRight(strings.TrimSpace(candidate.Server), "/")
		parsed, err := url.Parse(server)
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
			continue
		}
		if parsed.Path == "" || parsed.Path == "/" {
			parsed.Path = "/rest/2.0/pcs/superfile2"
		}
		p.log(ctx, slog.LevelInfo, "baidu pcs locateupload selected server",
			"remote_path", remotePath,
			"server_host", parsed.Host,
		)
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	for _, candidate := range response.Server {
		server := strings.TrimRight(strings.TrimSpace(candidate), "/")
		parsed, err := url.Parse(server)
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
			continue
		}
		if parsed.Path == "" || parsed.Path == "/" {
			parsed.Path = "/rest/2.0/pcs/superfile2"
		}
		p.log(ctx, slog.LevelInfo, "baidu pcs locateupload selected server",
			"remote_path", remotePath,
			"server_host", parsed.Host,
		)
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	p.log(ctx, slog.LevelWarn, "baidu pcs locateupload returned no usable server; using default upload server",
		"remote_path", remotePath,
		"server_count", serverCount,
	)
	return baiduPCSUploadBaseURL + "/rest/2.0/pcs/superfile2", nil
}

func (p *baiduPCSProvider) uploadChunks(ctx context.Context, serverURL, remotePath, uploadID string, parts []int, file *os.File, digest *baiduPCSDigest) error {
	if len(parts) == 0 {
		return nil
	}
	workerCount := p.chunkConcurrency
	if workerCount <= 0 {
		workerCount = baiduPCSChunkConcurrency
	}
	if workerCount > len(parts) {
		workerCount = len(parts)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var next atomic.Int64
	progress := newBaiduPCSUploadProgress(func(bytesTransferred int64) {
		p.reportProgress(ctx, bytesTransferred)
	})
	errorsCh := make(chan error, 1)
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func(workerIndex int) {
			defer workers.Done()
			if workerIndex > 0 {
				if err := waitBaiduOpen(workerCtx, time.Duration(workerIndex)*baiduPCSWorkerStagger); err != nil {
					return
				}
			}
			for {
				index := int(next.Add(1) - 1)
				if index >= len(parts) {
					return
				}
				part := parts[index]
				var err error
				for attempt := 0; attempt < baiduPCSChunkRetryLimit; attempt++ {
					err = p.uploadChunk(workerCtx, serverURL, remotePath, uploadID, part, file, digest, progress)
					if err == nil {
						break
					}
					if workerCtx.Err() != nil || attempt+1 >= baiduPCSChunkRetryLimit {
						break
					}
					if waitErr := waitBaiduOpen(workerCtx, time.Duration(attempt+1)*time.Second); waitErr != nil {
						return
					}
				}
				if err != nil {
					select {
					case errorsCh <- fmt.Errorf("BaiduPCS upload block %d after %d attempts: %w", part, baiduPCSChunkRetryLimit, err):
					default:
					}
					cancel()
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	select {
	case err := <-errorsCh:
		return err
	default:
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
}

func (p *baiduPCSProvider) uploadChunk(ctx context.Context, serverURL, remotePath, uploadID string, part int, file *os.File, digest *baiduPCSDigest, progress *baiduPCSUploadProgress) error {
	partOffset := int64(part) * digest.BlockSize
	partSize := digest.Size - partOffset
	if partSize > digest.BlockSize {
		partSize = digest.BlockSize
	}
	if partSize <= 0 {
		return fmt.Errorf("invalid block size for block %d", part)
	}
	prefix, suffix, contentType, err := baiduPCSMultipartEnvelope(pathpkg.Base(remotePath))
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("method", "upload")
	query.Set("type", "tmpfile")
	query.Set("path", normalizeBaiduOpenPath(remotePath))
	query.Set("uploadid", strings.TrimSpace(uploadID))
	query.Set("uploadsign", "0")
	query.Set("partseq", strconv.Itoa(part))
	query.Set("partoffset", strconv.FormatInt(partOffset, 10))
	query.Set("app_id", baiduPCSAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("dp-logid", p.nextDPLogID())
	body := io.MultiReader(
		bytes.NewReader(prefix),
		&baiduPCSProgressReader{
			reader: io.NewSectionReader(file, partOffset, partSize),
			offset: partOffset,
			report: func(absoluteBytes int64) {
				if progress != nil {
					progress.update(part, absoluteBytes-partOffset)
				}
			},
		},
		bytes.NewReader(suffix),
	)
	bodyLength := int64(len(prefix)) + partSize + int64(len(suffix))
	var response baiduPCSUploadResponse
	p.log(ctx, slog.LevelInfo, "baidu pcs superfile2 upload started",
		"remote_path", remotePath,
		"part", part,
		"part_offset", partOffset,
		"part_size", partSize,
		"server_host", baiduPCSURLHost(serverURL),
	)
	err = p.doJSONRequestWithOptions(ctx, http.MethodPost, strings.TrimRight(serverURL, "/"), query, body, contentType, bodyLength, false, false, &response)
	if err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs superfile2 upload failed",
			"remote_path", remotePath,
			"part", part,
			"error", p.logError(err),
		)
		return err
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs superfile2 upload completed",
		"remote_path", remotePath,
		"part", part,
		"returned_md5", response.MD5,
	)
	return nil
}

func baiduPCSMultipartEnvelope(filename string) (prefix, suffix []byte, contentType string, err error) {
	var envelope bytes.Buffer
	multipartWriter := multipart.NewWriter(&envelope)
	if _, err := multipartWriter.CreateFormFile("file", filename); err != nil {
		return nil, nil, "", err
	}
	prefixLength := envelope.Len()
	if err := multipartWriter.Close(); err != nil {
		return nil, nil, "", err
	}
	data := envelope.Bytes()
	return append([]byte(nil), data[:prefixLength]...), append([]byte(nil), data[prefixLength:]...), multipartWriter.FormDataContentType(), nil
}

func (p *baiduPCSProvider) rapidUploadFromFile(ctx context.Context, remotePath, uploadID string, file *os.File, modTime time.Time, digest *baiduPCSDigest, rtype int) (RemoteFile, error) {
	uk, err := p.userUK(ctx)
	if err != nil {
		return RemoteFile{}, err
	}
	dataTime := time.Now().Unix()
	encodedContentMD5 := encodeBaiduOpenMD5(digest.MD5)
	offset := baiduPCSDataOffset(encodedContentMD5, uk, dataTime, digest.Size)
	dataLength := int64(baiduPCSDataContentSize)
	if digest.Size < dataLength {
		dataLength = digest.Size
	}
	data := make([]byte, dataLength)
	if dataLength > 0 {
		read, readErr := file.ReadAt(data, offset)
		if readErr != nil && !(errors.Is(readErr, io.EOF) && int64(read) == dataLength) {
			return RemoteFile{}, readErr
		}
		if int64(read) != dataLength {
			return RemoteFile{}, fmt.Errorf("read rapid upload sample: got %d bytes, want %d", read, dataLength)
		}
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs rapidupload prepared",
		"remote_path", remotePath,
		"size", digest.Size,
		"rtype", rtype,
		"uploadid_present", strings.TrimSpace(uploadID) != "",
		"data_offset", offset,
		"data_length", len(data),
		"content_md5", digest.MD5,
		"slice_md5", digest.SliceMD5,
		"encoded_content_md5", encodedContentMD5,
		"encoded_slice_md5", encodeBaiduOpenMD5(digest.SliceMD5),
	)
	return p.rapidUploadWithData(ctx, remotePath, uploadID, modTime, digest, rtype, offset, dataTime, data)
}

func (p *baiduPCSProvider) rapidUploadWithData(ctx context.Context, remotePath, uploadID string, modTime time.Time, digest *baiduPCSDigest, rtype int, offset, dataTime int64, data []byte) (RemoteFile, error) {
	query, err := p.webQuery(ctx, true)
	if err != nil {
		return RemoteFile{}, err
	}
	query.Set("rtype", strconv.Itoa(rtype))
	form := url.Values{}
	if strings.TrimSpace(uploadID) != "" {
		form.Set("uploadid", strings.TrimSpace(uploadID))
	}
	form.Set("path", normalizeBaiduOpenPath(remotePath))
	form.Set("target_path", baiduPCSTargetPath(remotePath))
	form.Set("content-length", strconv.FormatInt(digest.Size, 10))
	form.Set("content-md5", encodeBaiduOpenMD5(digest.MD5))
	form.Set("slice-md5", encodeBaiduOpenMD5(digest.SliceMD5))
	form.Set("local_mtime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("data_time", strconv.FormatInt(dataTime, 10))
	form.Set("data_offset", strconv.FormatInt(offset, 10))
	form.Set("data_content", base64.StdEncoding.EncodeToString(data))
	encoded := form.Encode()
	var response baiduPCSRapidResponse
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/rapidupload", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response); err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs rapidupload response failed",
			"remote_path", remotePath,
			"rtype", rtype,
			"error", p.logError(err),
		)
		return RemoteFile{}, err
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs rapidupload response",
		"remote_path", remotePath,
		"rtype", rtype,
		"return_type", response.ReturnType,
		"fs_id", strings.TrimSpace(string(response.FSID)),
		"info_fs_id", baiduPCSInfoFSID(response.Info),
		"path", response.Path,
	)
	if response.code() != 0 {
		return RemoteFile{}, &baiduPCSAPIError{Code: response.code(), Message: response.message()}
	}
	id := strings.TrimSpace(string(response.FSID))
	if id == "" && response.Info != nil {
		id = strings.TrimSpace(string(response.Info.FSID))
	}
	if id == "" {
		p.log(ctx, slog.LevelWarn, "baidu pcs rapidupload returned no fs_id",
			"remote_path", remotePath,
			"rtype", rtype,
		)
		return RemoteFile{}, errors.New("BaiduPCS rapid upload did not complete")
	}
	actualPath := strings.TrimSpace(response.Path)
	actualSize := ""
	if response.Info != nil {
		if strings.TrimSpace(response.Info.Path) != "" {
			actualPath = strings.TrimSpace(response.Info.Path)
		}
		actualSize = strings.TrimSpace(string(response.Info.Size))
	}
	if actualPath != "" && normalizeBaiduOpenPath(actualPath) != normalizeBaiduOpenPath(remotePath) {
		return RemoteFile{}, &baiduPCSRemotePathError{Expected: normalizeBaiduOpenPath(remotePath), Actual: normalizeBaiduOpenPath(actualPath), FSID: id}
	}
	if actualSize != "" {
		parsedSize, parseErr := strconv.ParseInt(actualSize, 10, 64)
		if parseErr != nil {
			return RemoteFile{}, fmt.Errorf("BaiduPCS rapid upload returned invalid size %q (fs_id=%s)", actualSize, id)
		}
		if parsedSize != digest.Size {
			return RemoteFile{}, fmt.Errorf("BaiduPCS rapid upload returned size %d, want %d (fs_id=%s)", parsedSize, digest.Size, id)
		}
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs rapidupload returned fs_id",
		"remote_path", remotePath,
		"fs_id", id,
	)
	return RemoteFile{ID: id, Size: digest.Size}, nil
}

// repairUnexpectedRapidUploadPath resolves a server-side collision rename.
// The rapid-upload result is the file that must survive; any file currently at
// the requested path is removed before the result is renamed into place.
func (p *baiduPCSProvider) repairUnexpectedRapidUploadPath(ctx context.Context, pathErr *baiduPCSRemotePathError, expectedPath string, size int64) (RemoteFile, error) {
	if pathErr == nil {
		return RemoteFile{}, errors.New("BaiduPCS rapid upload path error is missing")
	}
	expectedPath = normalizeBaiduOpenPath(expectedPath)
	actualPath := normalizeBaiduOpenPath(pathErr.Actual)
	if actualPath == "/" || actualPath == expectedPath {
		return RemoteFile{}, fmt.Errorf("invalid rapid upload path repair source %q", actualPath)
	}
	fsID := strings.TrimSpace(pathErr.FSID)
	if fsID == "" {
		return RemoteFile{}, errors.New("rapid upload path repair has no fs_id")
	}
	cleanup := func(primary error) (RemoteFile, error) {
		cleanupErr := p.deleteFile(ctx, actualPath)
		if cleanupErr != nil {
			return RemoteFile{}, fmt.Errorf("%w; cleanup rapid upload result %s failed: %v", primary, actualPath, cleanupErr)
		}
		return RemoteFile{}, primary
	}
	if pathpkg.Dir(actualPath) != pathpkg.Dir(expectedPath) {
		return cleanup(fmt.Errorf("rapid upload path repair cannot move %q to %q across directories", actualPath, expectedPath))
	}

	parentPath := pathpkg.Dir(expectedPath)
	name := pathpkg.Base(expectedPath)
	entry, found, err := p.findEntry(ctx, parentPath, name)
	if err != nil {
		return cleanup(fmt.Errorf("check rapid upload rename target %s: %w", expectedPath, err))
	}
	if found {
		if entry.IsDir {
			return cleanup(fmt.Errorf("BaiduPCS rapid upload rename target is a directory: %s", expectedPath))
		}
		if err := p.deleteFile(ctx, entry.Path); err != nil {
			return cleanup(fmt.Errorf("delete existing BaiduPCS target %s: %w", expectedPath, err))
		}
	}

	if err := p.renameFile(ctx, fsID, actualPath, name); err != nil {
		return cleanup(fmt.Errorf("rename rapid upload result %s to %s: %w", actualPath, expectedPath, err))
	}
	return p.verifyRemoteFile(ctx, fsID, expectedPath, size)
}

func (p *baiduPCSProvider) renameFile(ctx context.Context, fsID, oldPath, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == "/" || pathpkg.Base(newName) != newName {
		return fmt.Errorf("invalid BaiduPCS rename target name %q", newName)
	}
	return p.fileManager(ctx, "rename", fsID, oldPath, newName)
}

func (p *baiduPCSProvider) deleteFile(ctx context.Context, remotePath string) error {
	return p.fileManager(ctx, "delete", "", remotePath, "")
}

func (p *baiduPCSProvider) fileManager(ctx context.Context, operation, fsID, remotePath, newName string) error {
	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation != "delete" && operation != "rename" {
		return fmt.Errorf("unsupported BaiduPCS file manager operation %q", operation)
	}
	fsID = strings.TrimSpace(fsID)
	remotePath = normalizeBaiduOpenPath(remotePath)
	if operation == "rename" && fsID == "" {
		return fmt.Errorf("BaiduPCS %s requires fs_id", operation)
	}
	if operation == "rename" {
		if _, err := strconv.ParseInt(fsID, 10, 64); err != nil {
			return fmt.Errorf("BaiduPCS %s fs_id %q is invalid: %w", operation, fsID, err)
		}
	}
	if remotePath == "/" {
		return fmt.Errorf("BaiduPCS %s requires a file path", operation)
	}
	if operation == "rename" && (newName == "" || pathpkg.Base(newName) != newName) {
		return fmt.Errorf("BaiduPCS rename requires a file name, got %q", newName)
	}

	var fileList []byte
	var err error
	if operation == "delete" {
		fileList, err = json.Marshal([]string{remotePath})
	} else {
		fileList, err = json.Marshal([]baiduPCSFileManagerItem{{
			ID:      json.Number(fsID),
			Path:    remotePath,
			NewName: newName,
		}})
	}
	if err != nil {
		return fmt.Errorf("encode BaiduPCS %s filelist: %w", operation, err)
	}
	form := url.Values{}
	form.Set("filelist", string(fileList))
	query := url.Values{}
	query.Set("async", "2")
	query.Set("onnest", "fail")
	query.Set("opera", operation)
	if operation == "delete" {
		query.Set("newVerify", "1")
	}
	encoded := form.Encode()
	var response baiduPCSFileManagerResponse
	p.log(ctx, slog.LevelInfo, "baidu pcs file manager operation started",
		"operation", operation,
		"remote_path", remotePath,
		"new_name", newName,
		"fs_id", fsID,
	)
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/filemanager", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), true, &response); err != nil {
		return err
	}
	taskID := strings.TrimSpace(string(response.TaskID))
	p.log(ctx, slog.LevelInfo, "baidu pcs file manager operation accepted",
		"operation", operation,
		"remote_path", remotePath,
		"new_name", newName,
		"fs_id", fsID,
		"task_id_present", taskID != "",
	)
	if taskID == "" {
		return nil
	}
	return p.waitFileManagerTask(ctx, taskID)
}

func (p *baiduPCSProvider) waitFileManagerTask(ctx context.Context, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	for attempt := 0; attempt < baiduPCSFileManagerTaskAttempts; attempt++ {
		query := url.Values{}
		query.Set("taskid", taskID)
		query.Set("app_id", baiduPCSAppID)
		query.Set("web", "1")
		query.Set("clienttype", "0")
		query.Set("dp-logid", p.nextDPLogID())
		var response baiduPCSShareTaskQueryResponse
		if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/share/taskquery", query, nil, "", 0, false, &response); err != nil {
			return fmt.Errorf("query BaiduPCS file manager task %s: %w", taskID, err)
		}
		taskErrno := parseBaiduOpenInt64(string(response.TaskErrno))
		status := strings.ToLower(strings.TrimSpace(response.Status))
		if taskErrno != 0 {
			message := strings.TrimSpace(response.ShowMsg)
			if message == "" {
				message = "task failed"
			}
			return fmt.Errorf("BaiduPCS file manager task %s failed: task_errno=%d message=%s", taskID, taskErrno, message)
		}
		switch status {
		case "success", "succeeded", "done", "complete", "completed":
			return nil
		case "failed", "failure", "error":
			message := strings.TrimSpace(response.ShowMsg)
			if message == "" {
				message = "task failed"
			}
			return fmt.Errorf("BaiduPCS file manager task %s failed: %s", taskID, message)
		}
		if attempt+1 < baiduPCSFileManagerTaskAttempts {
			if err := waitBaiduOpen(ctx, baiduPCSVerifyRetryDelay); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("BaiduPCS file manager task %s did not complete", taskID)
}

func (p *baiduPCSProvider) userUK(ctx context.Context) (int64, error) {
	if err := p.ensureWebSession(ctx, true); err != nil {
		return 0, err
	}
	p.sessionMu.Lock()
	uk := p.uk
	p.sessionMu.Unlock()
	if uk <= 0 {
		return 0, errors.New("BaiduPCS web session returned no uk")
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs web user resolved", "uk", uk)
	return uk, nil
}

func (p *baiduPCSProvider) MaxConcurrentFiles(ctx context.Context) (int, error) {
	p.vipMu.Lock()
	if p.vipLoaded {
		limit := p.maxConcurrentFiles
		p.vipMu.Unlock()
		return limit, nil
	}
	p.vipMu.Unlock()

	query, err := p.webQuery(ctx, false)
	if err != nil {
		return 1, err
	}
	query.Set("version", strconv.FormatInt(time.Now().UnixMilli(), 10))
	var response baiduPCSLoginStatusResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/api/loginStatus", query, nil, "", 0, false, &response); err != nil {
		return 1, err
	}
	identity, err := parseBaiduJSONInt64(response.LoginInfo.VIPIdentity)
	if err != nil || identity < 0 {
		if err == nil {
			err = errors.New("vip_identity must be non-negative")
		}
		p.vipMu.Lock()
		p.vipLoaded = true
		p.vipIdentity = 0
		p.maxConcurrentFiles = 1
		p.vipMu.Unlock()
		return 1, err
	}
	limit := 1
	if identity/10 == 2 {
		limit = 3
	}
	p.vipMu.Lock()
	p.vipLoaded = true
	p.vipIdentity = identity
	p.maxConcurrentFiles = limit
	p.vipMu.Unlock()
	p.log(ctx, slog.LevelInfo, "baidu pcs web upload concurrency resolved",
		"vip_type", identity/10,
		"max_concurrent_files", limit,
	)
	return limit, nil
}

func baiduPCSDataOffset(encodedContentMD5 string, uk, dataTime, fileSize int64) int64 {
	if fileSize <= baiduPCSDataContentSize {
		return 0
	}
	seed := strconv.FormatInt(uk, 10) + encodedContentMD5 + strconv.FormatInt(dataTime, 10)
	sum := md5.Sum([]byte(seed))
	raw := binary.BigEndian.Uint32(sum[:4])
	return int64(uint64(raw) % uint64(fileSize-baiduPCSDataContentSize+1))
}

func (p *baiduPCSProvider) verifyRemoteFile(ctx context.Context, fsID, expectedPath string, size int64) (RemoteFile, error) {
	fsID = strings.TrimSpace(fsID)
	expectedPath = normalizeBaiduOpenPath(expectedPath)
	parent := pathpkg.Dir(expectedPath)
	name := pathpkg.Base(expectedPath)
	for attempt := 0; attempt < baiduPCSVerifyAttempts; attempt++ {
		entries, err := p.listFiles(ctx, parent)
		if err != nil {
			p.log(ctx, slog.LevelWarn, "baidu pcs remote verification list failed",
				"remote_path", expectedPath,
				"attempt", attempt+1,
				"error", p.logError(err),
			)
			return RemoteFile{}, err
		}
		for _, item := range entries {
			if item.Path != expectedPath || item.Name != name || item.IsDir || item.ID == "" {
				continue
			}
			if item.Size != size {
				return RemoteFile{}, fmt.Errorf("BaiduPCS remote file size %d does not match %d (fs_id=%s)", item.Size, size, item.ID)
			}
			if fsID != "" && item.ID != fsID {
				p.log(ctx, slog.LevelWarn, "baidu pcs remote verification found a different fs_id",
					"remote_path", expectedPath,
					"expected_fs_id", fsID,
					"actual_fs_id", item.ID,
					"attempt", attempt+1,
				)
				return RemoteFile{}, fmt.Errorf("BaiduPCS remote file fs_id %s does not match %s at %s", item.ID, fsID, expectedPath)
			}
			p.log(ctx, slog.LevelInfo, "baidu pcs remote verification succeeded",
				"remote_path", expectedPath,
				"fs_id", item.ID,
				"size", item.Size,
				"attempt", attempt+1,
			)
			return RemoteFile{ID: item.ID, Size: item.Size}, nil
		}
		if attempt+1 < baiduPCSVerifyAttempts {
			if err := waitBaiduOpen(ctx, baiduPCSVerifyRetryDelay); err != nil {
				return RemoteFile{}, err
			}
		}
	}
	p.log(ctx, slog.LevelWarn, "baidu pcs remote verification did not find file",
		"remote_path", expectedPath,
		"fs_id", fsID,
		"attempts", baiduPCSVerifyAttempts,
	)
	return RemoteFile{}, fmt.Errorf("BaiduPCS remote file is not visible yet: %s", expectedPath)
}

func (p *baiduPCSProvider) webQuery(ctx context.Context, needsToken bool) (url.Values, error) {
	query := url.Values{}
	query.Set("app_id", baiduPCSAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("dp-logid", p.nextDPLogID())
	if !needsToken {
		return query, nil
	}
	token, err := p.ensureBDSToken(ctx)
	if err != nil {
		return nil, err
	}
	query.Set("bdstoken", token)
	return query, nil
}

func (p *baiduPCSProvider) ensureWebSession(ctx context.Context, needUK bool) error {
	p.sessionMu.Lock()
	defer p.sessionMu.Unlock()
	needToken := strings.TrimSpace(p.bdstoken) == ""
	needUserKey := needUK && p.uk <= 0
	if !needToken && !needUserKey {
		return nil
	}
	fields := make([]string, 0, 2)
	if needToken {
		fields = append(fields, "bdstoken")
	}
	if needUserKey {
		fields = append(fields, "uk")
	}
	encodedFields, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("fields", string(encodedFields))
	query.Set("app_id", baiduPCSAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("dp-logid", p.nextDPLogID())
	var response baiduPCSWebTemplateResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/api/gettemplatevariable", query, nil, "", 0, false, &response); err != nil {
		return err
	}
	if needToken {
		p.bdstoken = strings.TrimSpace(jsonRawString(response.Result["bdstoken"]))
		if p.bdstoken == "" {
			return errors.New("BaiduPCS gettemplatevariable returned no bdstoken")
		}
	}
	if needUserKey {
		uk, parseErr := parseBaiduJSONInt64(response.Result["uk"])
		if parseErr != nil || uk <= 0 {
			if parseErr == nil {
				parseErr = errors.New("uk must be positive")
			}
			return parseErr
		}
		p.uk = uk
	}
	return nil
}

func (p *baiduPCSProvider) ensureBDSToken(ctx context.Context) (string, error) {
	if err := p.ensureWebSession(ctx, false); err != nil {
		return "", err
	}
	p.sessionMu.Lock()
	token := p.bdstoken
	p.sessionMu.Unlock()
	if strings.TrimSpace(token) == "" {
		return "", errors.New("BaiduPCS gettemplatevariable returned no bdstoken")
	}
	return token, nil
}

func (p *baiduPCSProvider) doJSONRequest(ctx context.Context, method, endpoint string, query url.Values, body any, contentType string, contentLength int64, needsToken bool, out any) error {
	return p.doJSONRequestWithOptions(ctx, method, endpoint, query, body, contentType, contentLength, needsToken, true, out)
}

func (p *baiduPCSProvider) doJSONRequestWithOptions(ctx context.Context, method, endpoint string, query url.Values, body any, contentType string, contentLength int64, needsToken, throttle bool, out any) error {
	if needsToken {
		var err error
		query, err = p.webQueryWithValues(ctx, query)
		if err != nil {
			return err
		}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs request preparation failed", "endpoint", endpoint, "error", p.logError(err))
		return err
	}
	values := cloneBaiduOpenValues(query)
	if _, ok := values["app_id"]; !ok && strings.Contains(parsed.Path, "/rest/2.0/pcs/") {
		values.Set("app_id", baiduPCSAppID)
	}
	parsed.RawQuery = values.Encode()
	var reader io.Reader
	switch typed := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(typed)
	case io.Reader:
		reader = typed
	default:
		err := fmt.Errorf("unsupported BaiduPCS request body %T", body)
		p.log(ctx, slog.LevelWarn, "baidu pcs request preparation failed", "operation", baiduPCSOperation(parsed), "error", p.logError(err))
		return err
	}
	requestID := p.requestSequence.Add(1)
	requestFields := baiduPCSRequestFields(requestID, method, parsed, values, contentLength)
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), reader)
	if err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs request preparation failed", append(requestFields, "error", p.logError(err))...)
		return err
	}
	if contentLength > 0 {
		req.ContentLength = contentLength
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Cookie", p.cookie)
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Encoding", "identity")
	requestStarted := time.Now()
	p.log(ctx, slog.LevelInfo, "baidu pcs request started", requestFields...)
	if throttle {
		if err := p.waitRequest(ctx); err != nil {
			p.log(ctx, slog.LevelWarn, "baidu pcs request throttling failed", append(requestFields, "error", p.logError(err))...)
			return err
		}
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		wrappedErr := fmt.Errorf("BaiduPCS request failed: %w", err)
		p.log(ctx, slog.LevelWarn, "baidu pcs request failed", append(requestFields,
			"duration_ms", time.Since(requestStarted).Milliseconds(),
			"error", p.logError(wrappedErr),
		)...)
		return wrappedErr
	}
	defer resp.Body.Close()
	responseBody, err := readBaiduPCSResponseBody(resp)
	if err != nil {
		wrappedErr := fmt.Errorf("read BaiduPCS response: %w", err)
		p.log(ctx, slog.LevelWarn, "baidu pcs response read failed", append(requestFields,
			"status", resp.StatusCode,
			"duration_ms", time.Since(requestStarted).Milliseconds(),
			"error", p.logError(wrappedErr),
		)...)
		return wrappedErr
	}
	var envelope baiduPCSAPIResponse
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		wrappedErr := fmt.Errorf("decode BaiduPCS response status=%d: %w", resp.StatusCode, err)
		p.log(ctx, slog.LevelWarn, "baidu pcs response decode failed", append(requestFields,
			"status", resp.StatusCode,
			"response_bytes", len(responseBody),
			"duration_ms", time.Since(requestStarted).Milliseconds(),
			"error", p.logError(wrappedErr),
		)...)
		return wrappedErr
	}
	code := envelope.code()
	responseFields := append([]any{}, requestFields...)
	responseFields = append(responseFields,
		"status", resp.StatusCode,
		"code", code,
		"message", envelope.message(),
		"response_bytes", len(responseBody),
		"duration_ms", time.Since(requestStarted).Milliseconds(),
	)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || code != 0 {
		p.log(ctx, slog.LevelWarn, "baidu pcs response received", responseFields...)
	} else {
		p.log(ctx, slog.LevelInfo, "baidu pcs response received", responseFields...)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &baiduPCSAPIError{StatusCode: resp.StatusCode, Code: code, Message: envelope.message()}
	}
	if code != 0 {
		return &baiduPCSAPIError{StatusCode: resp.StatusCode, Code: code, Message: envelope.message()}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		wrappedErr := fmt.Errorf("decode BaiduPCS response data: %w", err)
		p.log(ctx, slog.LevelWarn, "baidu pcs response data decode failed", append(requestFields,
			"status", resp.StatusCode,
			"code", code,
			"response_bytes", len(responseBody),
			"error", p.logError(wrappedErr),
		)...)
		return wrappedErr
	}
	return nil
}

func baiduPCSURLHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func baiduPCSInfoFSID(info *baiduPCSFileItem) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(string(info.FSID))
}

func parseBaiduJSONInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, errors.New("missing numeric value")
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
}

func jsonRawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

func (p *baiduPCSProvider) nextDPLogID() string {
	sequence := p.requestSequence.Add(1) % 10000000
	return fmt.Sprintf("%013d%07d", time.Now().UnixMilli()%10000000000000, sequence)
}

func readBaiduPCSResponseBody(response *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	encoding := strings.ToLower(response.Header.Get("Content-Encoding"))
	hasGzipHeader := strings.Contains(encoding, "gzip")
	hasGzipMagic := len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b
	if !hasGzipHeader && !hasGzipMagic {
		return raw, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		// Some Baidu/proxy responses retain the gzip header while returning
		// plain JSON. Leave those bodies intact so the JSON decoder can read them.
		if hasGzipHeader && !hasGzipMagic {
			return raw, nil
		}
		return nil, err
	}
	decompressed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return decompressed, nil
}

func (p *baiduPCSProvider) webQueryWithValues(ctx context.Context, values url.Values) (url.Values, error) {
	result := cloneBaiduOpenValues(values)
	if strings.TrimSpace(result.Get("bdstoken")) == "" {
		token, err := p.ensureBDSToken(ctx)
		if err != nil {
			return nil, err
		}
		result.Set("bdstoken", token)
	}
	return result, nil
}

func (p *baiduPCSProvider) waitRequest(ctx context.Context) error {
	if p.requestInterval <= 0 {
		return nil
	}
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	if !p.lastRequest.IsZero() {
		waitFor := p.lastRequest.Add(p.requestInterval).Sub(time.Now())
		if waitFor > 0 {
			if err := waitBaiduOpen(ctx, waitFor); err != nil {
				return err
			}
		}
	}
	p.lastRequest = time.Now()
	return nil
}

func baiduPCSResponseFSID(response any) string {
	switch typed := response.(type) {
	case *baiduPCSPrecreateResponse:
		if typed == nil {
			return ""
		}
		if id := strings.TrimSpace(string(typed.FSID)); id != "" {
			return id
		}
		if typed.Info != nil {
			return strings.TrimSpace(string(typed.Info.FSID))
		}
	case *baiduPCSCreateResponse:
		if typed == nil {
			return ""
		}
		if id := strings.TrimSpace(string(typed.FSID)); id != "" {
			return id
		}
		if typed.Info != nil {
			return strings.TrimSpace(string(typed.Info.FSID))
		}
	}
	return ""
}
