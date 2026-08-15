package upload

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
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
	baiduPCSBaseURL          = "https://pan.baidu.com"
	baiduPCSAPIBaseURL       = "https://pcs.baidu.com"
	baiduPCSUploadBaseURL    = "https://c2.pcs.baidu.com"
	baiduPCSAppID            = "250528"
	baiduPCSDefaultUserAgent = "netdisk;P2SP;3.0.0.8;netdisk;11.12.3;ANG-AN00;android-android;10.0;JSbridge4.4.0;jointBridge;1.1.0;"
	baiduPCSRequestTimeout   = 5 * time.Minute
	baiduPCSSliceSize        = 256 * 1024
	baiduPCSDataContentSize  = 4 * 1024
	baiduPCSMinBlockSize     = 4 * 1024 * 1024
	baiduPCSMiddleBlockSize  = 16 * 1024 * 1024
	baiduPCSMaxBlockSize     = 64 * 1024 * 1024
	baiduPCSMiddleThreshold  = 8 * 1024 * 1024 * 1024
	baiduPCSMaxThreshold     = 32 * 1024 * 1024 * 1024
	baiduPCSRapidAfterBlocks = 8
	baiduPCSVerifyAttempts   = 8
	baiduPCSVerifyRetryDelay = time.Second
)

type baiduPCSProvider struct {
	cookie    string
	bdstoken  string
	userAgent string
	logger    *slog.Logger

	httpClient       *http.Client
	requestInterval  time.Duration
	requestMu        sync.Mutex
	lastRequest      time.Time
	requestSequence  atomic.Uint64
	bdstokenMu       sync.Mutex
	ukMu             sync.Mutex
	uk               int64
	progressReporter func(int64)
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

type baiduPCSUserInfoResponse struct {
	baiduPCSAPIResponse
	Records []struct {
		UK json.Number `json:"uk"`
	} `json:"records"`
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
		cookie:          strings.TrimSpace(cookie),
		bdstoken:        strings.TrimSpace(bdstoken),
		userAgent:       strings.TrimSpace(userAgent),
		logger:          logger,
		httpClient:      &http.Client{Timeout: baiduPCSRequestTimeout, Transport: transport},
		requestInterval: requestInterval,
	}, nil
}

func (p *baiduPCSProvider) setProgressReporter(reporter func(int64)) {
	p.progressReporter = reporter
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
	if p.progressReporter != nil {
		p.progressReporter(0)
	}

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

	digest, err := calculateBaiduPCSDigest(ctx, file)
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

	precreated, err := p.precreate(ctx, remotePath, info.ModTime(), digest)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS precreate %s: %w", remotePath, err)}
	}
	if precreated == nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: errors.New("BaiduPCS precreate returned an empty response")}
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
	createdID := baiduPCSResponseFSID(precreated)
	if precreated.ReturnType == 2 {
		p.log(ctx, slog.LevelInfo, "baidu pcs precreate reported rapid upload",
			"remote_path", remotePath,
			"fs_id", createdID,
		)
		if createdID == "" {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: errors.New("BaiduPCS precreate rapid upload returned no fs_id")}
		}
		verified, verifyErr := p.verifyRemoteFile(ctx, createdID, remotePath, size)
		if verifyErr != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("verify BaiduPCS rapid upload %s: %w", remotePath, verifyErr)}
		}
		verified.LocalSHA1 = localSHA1
		verified.SHA1 = localSHA1
		verified.Outcome = intendedOutcome
		if p.progressReporter != nil {
			p.progressReporter(size)
		}
		return verified, nil
	}
	if strings.TrimSpace(precreated.UploadID) == "" {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: errors.New("BaiduPCS precreate returned no uploadid")}
	}

	parts := append([]int(nil), precreated.BlockList...)
	if len(parts) == 0 && size > 0 {
		parts = []int{0}
	}
	for _, part := range parts {
		if part < 0 || part >= len(digest.ChunkMD5s) {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS precreate returned invalid block index %d", part)}
		}
	}
	serverURL, err := p.locateUpload(ctx, remotePath, precreated.UploadID)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("locate BaiduPCS upload server: %w", err)}
	}
	p.log(ctx, slog.LevelInfo, "baidu pcs upload server selected",
		"remote_path", remotePath,
		"server_host", baiduPCSURLHost(serverURL),
	)
	for index, part := range parts {
		if err := p.uploadChunk(ctx, serverURL, remotePath, precreated.UploadID, part, file, digest); err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("BaiduPCS upload block %d for %s: %w", part, remotePath, err)}
		}
		// The web client retries rapid upload after several temporary blocks.
		// This preserves the traffic-saving path while retaining a normal
		// create fallback when the server does not recognize the slice.
		if (index+1)%baiduPCSRapidAfterBlocks == 0 || index+1 == len(parts) {
			rapid, rapidErr := p.rapidUploadFromFile(ctx, remotePath, precreated.UploadID, file, info.ModTime(), digest, 1)
			if rapidErr != nil {
				p.log(ctx, slog.LevelWarn, "baidu pcs rapid upload attempt failed; continuing with upload",
					"remote_path", remotePath,
					"uploaded_block", part,
					"uploaded_block_position", index+1,
					"uploaded_block_count", len(parts),
					"error", p.logError(rapidErr),
				)
			} else if rapid.ID == "" {
				p.log(ctx, slog.LevelWarn, "baidu pcs rapid upload attempt returned no fs_id; continuing with upload",
					"remote_path", remotePath,
					"uploaded_block", part,
					"uploaded_block_position", index+1,
					"uploaded_block_count", len(parts),
				)
			} else {
				p.log(ctx, slog.LevelInfo, "baidu pcs rapid upload succeeded after block upload",
					"remote_path", remotePath,
					"fs_id", rapid.ID,
					"uploaded_block_position", index+1,
					"uploaded_block_count", len(parts),
				)
				verified, verifyErr := p.verifyRemoteFile(ctx, rapid.ID, remotePath, size)
				if verifyErr != nil {
					return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("verify BaiduPCS rapid upload %s: %w", remotePath, verifyErr)}
				}
				verified.LocalSHA1 = localSHA1
				verified.SHA1 = localSHA1
				verified.Outcome = intendedOutcome
				if p.progressReporter != nil {
					p.progressReporter(size)
				}
				return verified, nil
			}
		}
	}

	created, err := p.createFile(ctx, remotePath, size, precreated.UploadID, digest, 3)
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
	createdID = baiduPCSResponseFSID(created)
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
	if p.progressReporter != nil {
		p.progressReporter(size)
	}
	return verified, nil
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
	if info.Size() >= baiduPCSMaxThreshold {
		blockSize = baiduPCSMaxBlockSize
	} else if info.Size() >= baiduPCSMiddleThreshold {
		blockSize = baiduPCSMiddleBlockSize
	}
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
	currentPath := "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		nextPath := normalizeBaiduOpenPath(pathpkg.Join(currentPath, segment))
		entry, found, err := p.findEntry(ctx, currentPath, segment)
		if err != nil {
			return fmt.Errorf("resolve BaiduPCS directory %s: %w", currentPath, err)
		}
		if found {
			if !entry.IsDir {
				return fmt.Errorf("BaiduPCS path component is a file: %s", nextPath)
			}
			currentPath = nextPath
			continue
		}
		if err := p.createDirectory(ctx, nextPath); err != nil {
			created, foundAfterError, lookupErr := p.findEntry(ctx, currentPath, segment)
			if lookupErr == nil && foundAfterError && created.IsDir {
				currentPath = nextPath
				continue
			}
			return fmt.Errorf("create BaiduPCS directory %s: %w", nextPath, err)
		}
		currentPath = nextPath
	}
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
	form.Set("target_path", pathpkg.Dir(directoryPath)+"/")
	form.Set("size", "0")
	form.Set("isdir", "1")
	form.Set("rtype", "1")
	form.Set("ondup", "fail")
	var response baiduPCSAPIResponse
	encoded := form.Encode()
	p.log(ctx, slog.LevelInfo, "baidu pcs directory create started", "remote_path", directoryPath, "rtype", 1)
	err = p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/create", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response)
	if err == nil {
		p.log(ctx, slog.LevelInfo, "baidu pcs directory create result", "remote_path", directoryPath, "rtype", 1)
	}
	return err
}

func (p *baiduPCSProvider) precreate(ctx context.Context, remotePath string, modTime time.Time, digest *baiduPCSDigest) (*baiduPCSPrecreateResponse, error) {
	blockList, err := json.Marshal(digest.ChunkMD5s)
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
	form.Set("target_path", pathpkg.Dir(normalizeBaiduOpenPath(remotePath))+"/")
	form.Set("size", strconv.FormatInt(digest.Size, 10))
	form.Set("isdir", "0")
	form.Set("autoinit", "1")
	form.Set("local_mtime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("local_ctime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("content-md5", digest.MD5)
	form.Set("slice-md5", digest.SliceMD5)
	form.Set("block_list", string(blockList))
	form.Set("ondup", "overwrite")
	var response baiduPCSPrecreateResponse
	encoded := form.Encode()
	p.log(ctx, slog.LevelInfo, "baidu pcs precreate started",
		"remote_path", remotePath,
		"size", digest.Size,
		"block_count", len(digest.ChunkMD5s),
		"content_md5", digest.MD5,
		"slice_md5", digest.SliceMD5,
	)
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduPCSBaseURL+"/api/precreate", query, []byte(encoded), "application/x-www-form-urlencoded", int64(len(encoded)), false, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (p *baiduPCSProvider) createFile(ctx context.Context, remotePath string, size int64, uploadID string, digest *baiduPCSDigest, rtype int) (*baiduPCSCreateResponse, error) {
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
	form.Set("target_path", pathpkg.Dir(normalizeBaiduOpenPath(remotePath))+"/")
	form.Set("size", strconv.FormatInt(size, 10))
	form.Set("isdir", "0")
	form.Set("rtype", strconv.Itoa(rtype))
	form.Set("block_list", string(blockList))
	form.Set("ondup", "overwrite")
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

func (p *baiduPCSProvider) uploadChunk(ctx context.Context, serverURL, remotePath, uploadID string, part int, file *os.File, digest *baiduPCSDigest) error {
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
	body := io.MultiReader(
		bytes.NewReader(prefix),
		&baiduPCSProgressReader{reader: io.NewSectionReader(file, partOffset, partSize), offset: partOffset, report: p.progressReporter},
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
	err = p.doJSONRequest(ctx, http.MethodPost, strings.TrimRight(serverURL, "/"), query, body, contentType, bodyLength, false, &response)
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
	offset := baiduPCSDataOffset(digest.MD5, uk, dataTime, digest.Size)
	dataLength := int64(baiduPCSDataContentSize)
	if digest.Size < dataLength {
		dataLength = digest.Size
	}
	data := make([]byte, dataLength)
	if dataLength > 0 {
		if _, err := file.ReadAt(data, offset); err != nil && err != io.EOF {
			return RemoteFile{}, err
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
		"encoded_content_md5", encodeBaiduOpenMD5(digest.MD5),
		"encoded_slice_md5", encodeBaiduOpenMD5(digest.SliceMD5),
	)
	return p.rapidUploadWithData(ctx, remotePath, uploadID, modTime, digest, rtype, offset, dataTime, data)
}

func (p *baiduPCSProvider) rapidUploadWithData(ctx context.Context, remotePath, uploadID string, modTime time.Time, digest *baiduPCSDigest, rtype int, offset, dataTime int64, data []byte) (RemoteFile, error) {
	blockList, err := json.Marshal(digest.ChunkMD5s)
	if err != nil {
		return RemoteFile{}, err
	}
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
	form.Set("target_path", pathpkg.Dir(normalizeBaiduOpenPath(remotePath))+"/")
	form.Set("content-length", strconv.FormatInt(digest.Size, 10))
	form.Set("content-md5", encodeBaiduOpenMD5(digest.MD5))
	form.Set("slice-md5", encodeBaiduOpenMD5(digest.SliceMD5))
	form.Set("local_mtime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("local_ctime", strconv.FormatInt(modTime.Unix(), 10))
	form.Set("data_time", strconv.FormatInt(dataTime, 10))
	form.Set("data_offset", strconv.FormatInt(offset, 10))
	form.Set("data_length", strconv.Itoa(len(data)))
	form.Set("data_content", strings.TrimRight(base64.StdEncoding.EncodeToString(data), "="))
	form.Set("block_list", string(blockList))
	form.Set("mode", "1")
	form.Set("autoinit", "1")
	form.Set("checkexist", "0")
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
	p.log(ctx, slog.LevelInfo, "baidu pcs rapidupload returned fs_id",
		"remote_path", remotePath,
		"fs_id", id,
	)
	return RemoteFile{ID: id, Size: digest.Size}, nil
}

func (p *baiduPCSProvider) userUK(ctx context.Context) (int64, error) {
	p.ukMu.Lock()
	defer p.ukMu.Unlock()
	if p.uk > 0 {
		return p.uk, nil
	}
	query := url.Values{}
	query.Set("need_selfinfo", "1")
	var response baiduPCSUserInfoResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/api/user/getinfo", query, nil, "", 0, false, &response); err != nil {
		return 0, err
	}
	if len(response.Records) == 0 {
		return 0, errors.New("BaiduPCS user info returned no uk")
	}
	uk, err := strconv.ParseInt(strings.TrimSpace(string(response.Records[0].UK)), 10, 64)
	if err != nil || uk <= 0 {
		if err == nil {
			err = errors.New("uk must be positive")
		}
		return 0, err
	}
	p.uk = uk
	p.log(ctx, slog.LevelInfo, "baidu pcs user info resolved",
		"uk", uk,
	)
	return uk, nil
}

func baiduPCSDataOffset(contentMD5 string, uk, dataTime, fileSize int64) int64 {
	if fileSize <= baiduPCSDataContentSize {
		return 0
	}
	sum := md5.Sum([]byte(fmt.Sprintf("%d%s%d", uk, contentMD5, dataTime)))
	raw, err := strconv.ParseInt(fmt.Sprintf("%x", sum[:4]), 16, 64)
	if err != nil {
		return 0
	}
	return raw % (fileSize - baiduPCSDataContentSize + 1)
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

func (p *baiduPCSProvider) ensureBDSToken(ctx context.Context) (string, error) {
	p.bdstokenMu.Lock()
	defer p.bdstokenMu.Unlock()
	if strings.TrimSpace(p.bdstoken) != "" {
		return p.bdstoken, nil
	}
	query := url.Values{}
	query.Set("fields", `["bdstoken"]`)
	query.Set("app_id", baiduPCSAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	var response struct {
		baiduPCSAPIResponse
		Result struct {
			BDSToken string `json:"bdstoken"`
		} `json:"result"`
	}
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduPCSBaseURL+"/api/gettemplatevariable", query, nil, "", 0, false, &response); err != nil {
		return "", err
	}
	p.bdstoken = strings.TrimSpace(response.Result.BDSToken)
	if p.bdstoken == "" {
		return "", errors.New("BaiduPCS gettemplatevariable returned no bdstoken")
	}
	return p.bdstoken, nil
}

func (p *baiduPCSProvider) doJSONRequest(ctx context.Context, method, endpoint string, query url.Values, body any, contentType string, contentLength int64, needsToken bool, out any) error {
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
	if err := p.waitRequest(ctx); err != nil {
		p.log(ctx, slog.LevelWarn, "baidu pcs request throttling failed", append(requestFields, "error", p.logError(err))...)
		return err
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
