package upload

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/store"
)

const (
	baiduOpenAPIBaseURL       = "https://pan.baidu.com/rest/2.0/xpan"
	baiduOpenLocateUploadURL  = "https://d.pcs.baidu.com/rest/2.0/pcs/file"
	baiduOpenOAuthTokenURL    = "https://openapi.baidu.com/oauth/2.0/token"
	baiduOpenDefaultUserAgent = "pan.baidu.com"
	baiduOpenPageSize         = 1000
	baiduOpenChunkSize        = 4 * 1024 * 1024
	baiduOpenRequestTimeout   = 5 * time.Minute
	baiduOpenMaxAttempts      = 3
	baiduOpenRetryDelay       = 500 * time.Millisecond
	baiduOpenVerifyAttempts   = 8
	baiduOpenVerifyRetryDelay = 1 * time.Second
)

var errBaiduOpenFileMetadataNotVisible = errors.New("Baidu Open file metadata is not visible yet")

type baiduOpenTokenState struct {
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAt    string
}

func newBaiduOpenTokenState(accessToken, refreshToken, expiresAt string) *baiduOpenTokenState {
	return &baiduOpenTokenState{
		accessToken:  strings.TrimSpace(accessToken),
		refreshToken: strings.TrimSpace(refreshToken),
		expiresAt:    strings.TrimSpace(expiresAt),
	}
}

func (state *baiduOpenTokenState) snapshot() (accessToken, refreshToken, expiresAt string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.accessToken, state.refreshToken, state.expiresAt
}

type baiduOpenProvider struct {
	clientID         string
	clientSecret     string
	userAgent        string
	httpClient       *http.Client
	tokens           *baiduOpenTokenState
	progressReporter func(int64)

	onTokenRefreshed func(accessToken, refreshToken, expiresAt string)

	requestMu       sync.Mutex
	requestInterval time.Duration
	lastRequest     time.Time

	verifyRetryDelay func(int) time.Duration
}

type baiduOpenRequestBodyFactory func() (io.ReadCloser, int64)

type baiduOpenProgressReader struct {
	reader      io.Reader
	offset      int64
	transferred int64
	report      func(int64)
}

func (reader *baiduOpenProgressReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	if read > 0 {
		reader.transferred += int64(read)
		if reader.report != nil {
			reader.report(reader.offset + reader.transferred)
		}
	}
	return read, err
}

type baiduOpenEntry struct {
	ID       string
	Name     string
	Path     string
	IsDir    bool
	Size     int64
	MD5      string
	Category int
}

type baiduOpenAPIResponse struct {
	Errno     int64  `json:"errno"`
	ErrMsg    string `json:"errmsg"`
	ErrorCode int64  `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (response baiduOpenAPIResponse) code() int64 {
	if response.Errno != 0 {
		return response.Errno
	}
	return response.ErrorCode
}

func (response baiduOpenAPIResponse) message() string {
	if strings.TrimSpace(response.ErrMsg) != "" {
		return strings.TrimSpace(response.ErrMsg)
	}
	return strings.TrimSpace(response.ErrorMsg)
}

type baiduOpenAPIError struct {
	StatusCode int
	Code       int64
	Message    string
}

func (err *baiduOpenAPIError) Error() string {
	return fmt.Sprintf("baiduopen api error status=%d code=%d message=%s", err.StatusCode, err.Code, err.Message)
}

type baiduOpenFileItem struct {
	FSID           json.Number `json:"fs_id"`
	Path           string      `json:"path"`
	ServerFilename string      `json:"server_filename"`
	Size           json.Number `json:"size"`
	MD5            string      `json:"md5"`
	BlockList      []string    `json:"block_list"`
	IsDir          int         `json:"isdir"`
	ServerMTime    int64       `json:"server_mtime"`
	Category       int         `json:"category"`
}

type baiduOpenListResponse struct {
	baiduOpenAPIResponse
	List []baiduOpenFileItem `json:"list"`
}

type baiduOpenFileMetasResponse struct {
	baiduOpenAPIResponse
	List []baiduOpenFileItem `json:"list"`
}

type baiduOpenPrecreateResponse struct {
	baiduOpenAPIResponse
	UploadID   string             `json:"uploadid"`
	ReturnType int                `json:"return_type"`
	BlockList  []int              `json:"block_list"`
	FSID       json.Number        `json:"fs_id"`
	Info       *baiduOpenFileItem `json:"info"`
}

type baiduOpenCreateResponse struct {
	baiduOpenAPIResponse
	FSID json.Number `json:"fs_id"`
	Path string      `json:"path"`
}

func baiduOpenFSID(value json.Number) string {
	return strings.TrimSpace(string(value))
}

const baiduOpenHexDigits = "0123456789abcdef"

func baiduOpenMD5IsValid(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != md5.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Baidu file metadata may encode the MD5 by transforming every nibble and
// storing a key nibble at position 9. Keep this decoder best-effort because
// older files can still return an unrecognized metadata value.
func decodeBaiduOpenMD5(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || baiduOpenMD5IsValid(value) || len(value) != md5.Size*2 {
		return value
	}
	key := value[9]
	if key < 'g' || key > 'v' {
		return value
	}

	transformed := []byte(value)
	transformed[9] = baiduOpenHexDigits[int(key-'g')]
	decoded := make([]byte, len(transformed))
	for index, char := range transformed {
		var nibble byte
		if char == '-' {
			nibble = byte(15 & index)
		} else {
			position := strings.IndexByte(baiduOpenHexDigits, char)
			if position < 0 {
				return value
			}
			nibble = byte(position)
		}
		decoded[index] = baiduOpenHexDigits[nibble^byte(15&index)]
	}

	result := string(decoded[8:16]) + string(decoded[0:8]) + string(decoded[24:32]) + string(decoded[16:24])
	if !baiduOpenMD5IsValid(result) {
		return value
	}
	return result
}

func normalizeBaiduOpenFileItemMD5(item *baiduOpenFileItem) {
	if item == nil {
		return
	}
	if len(item.BlockList) == 1 && strings.TrimSpace(item.BlockList[0]) != "" {
		item.MD5 = item.BlockList[0]
	}
	item.MD5 = decodeBaiduOpenMD5(item.MD5)
}

func baiduOpenPrecreateResponseSummary(response *baiduOpenPrecreateResponse) string {
	if response == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"errno=%d error_code=%d return_type=%d uploadid_present=%t block_count=%d fs_id_present=%t",
		response.Errno,
		response.ErrorCode,
		response.ReturnType,
		strings.TrimSpace(response.UploadID) != "",
		len(response.BlockList),
		baiduOpenFSID(response.FSID) != "",
	)
}

func baiduOpenCreateResponseSummary(response *baiduOpenCreateResponse) string {
	if response == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"errno=%d error_code=%d fs_id=%q path=%q",
		response.Errno,
		response.ErrorCode,
		baiduOpenFSID(response.FSID),
		strings.TrimSpace(response.Path),
	)
}

func normalizeBaiduOpenCollisionPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "skip", "fail", "replace":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "fail"
	}
}

func baiduOpenRTypes(collisionPolicy string) (precreateRType, createRType string) {
	if normalizeBaiduOpenCollisionPolicy(collisionPolicy) == "replace" {
		return "3", "3"
	}
	// rtype=0 is the documented no-rename/conflict strategy. The create
	// request must use the same value as precreate, so set it explicitly in
	// both requests instead of relying on different endpoint defaults.
	return "0", "0"
}

type baiduOpenDigest struct {
	Size      int64
	SHA1      string
	MD5       string
	SliceMD5  string
	ChunkMD5s []string
}

func newBaiduOpenProvider(clientID, clientSecret, accessToken, refreshToken, expiresAt, userAgent string, requestInterval time.Duration, onTokenRefreshed func(accessToken, refreshToken, expiresAt string)) (*baiduOpenProvider, error) {
	if strings.TrimSpace(accessToken) == "" && strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("baiduopen access_token or refresh_token is required")
	}
	return &baiduOpenProvider{
		clientID:         strings.TrimSpace(clientID),
		clientSecret:     strings.TrimSpace(clientSecret),
		userAgent:        strings.TrimSpace(userAgent),
		httpClient:       &http.Client{Timeout: baiduOpenRequestTimeout},
		tokens:           newBaiduOpenTokenState(accessToken, refreshToken, expiresAt),
		requestInterval:  requestInterval,
		onTokenRefreshed: onTokenRefreshed,
		verifyRetryDelay: func(int) time.Duration { return baiduOpenVerifyRetryDelay },
	}, nil
}

func (p *baiduOpenProvider) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.listFilesPage(ctx, "/", 0, 1); err != nil {
		return fmt.Errorf("check Baidu Open account: %w", err)
	}
	return nil
}

func (p *baiduOpenProvider) setProgressReporter(reporter func(int64)) {
	p.progressReporter = reporter
}

func (p *baiduOpenProvider) List(ctx context.Context, remotePath string) ([]RemoteEntry, error) {
	remotePath = normalizeBaiduOpenPath(remotePath)
	items, err := p.listFiles(ctx, remotePath)
	if err != nil {
		return nil, fmt.Errorf("list Baidu Open directory %s: %w", remotePath, err)
	}
	entries := make([]RemoteEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, RemoteEntry{
			ID:    item.ID,
			Name:  item.Name,
			Path:  item.Path,
			IsDir: item.IsDir,
			Size:  item.Size,
		})
	}
	return entries, nil
}

func (p *baiduOpenProvider) Upload(ctx context.Context, localPath, remotePath string, size int64, localSHA1, collisionPolicy string) (RemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return RemoteFile{}, err
	}
	remotePath = normalizeBaiduOpenPath(remotePath)
	name := pathpkg.Base(remotePath)
	if name == "." || name == "/" || name == "" {
		return RemoteFile{}, fmt.Errorf("invalid Baidu Open target path %q", remotePath)
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

	parentPath := pathpkg.Dir(remotePath)
	if err := p.ensureDirectory(ctx, parentPath); err != nil {
		return RemoteFile{}, err
	}
	collisionPolicy = normalizeBaiduOpenCollisionPolicy(collisionPolicy)
	precreateRType, createRType := baiduOpenRTypes(collisionPolicy)
	existing, found, err := p.findEntry(ctx, parentPath, name)
	if err != nil {
		return RemoteFile{}, err
	}
	localSHA1 = strings.ToUpper(strings.TrimSpace(localSHA1))
	intendedOutcome := store.UploadOutcomeCreated
	var digest *baiduOpenDigest
	ensureDigest := func() (*baiduOpenDigest, error) {
		if digest != nil {
			return digest, nil
		}
		resolved, digestErr := calculateBaiduOpenDigest(ctx, file)
		if digestErr != nil {
			return nil, digestErr
		}
		if resolved.Size != size {
			return nil, fmt.Errorf("local file changed after batch snapshot: %s", localPath)
		}
		digest = resolved
		localSHA1 = digest.SHA1
		return digest, nil
	}

	if found {
		if existing.IsDir {
			return RemoteFile{}, fmt.Errorf("Baidu Open target path is a directory: %s", remotePath)
		}
		switch collisionPolicy {
		case "skip":
			return RemoteFile{ID: existing.ID, Size: existing.Size, LocalSHA1: localSHA1, Outcome: store.UploadOutcomeSkipped}, nil
		case "fail":
			return RemoteFile{}, fmt.Errorf("Baidu Open target already exists with different content: %s", remotePath)
		case "replace":
			intendedOutcome = store.UploadOutcomeReplaced
			if existing.Size == size {
				resolved, digestErr := ensureDigest()
				if digestErr != nil {
					return RemoteFile{}, fmt.Errorf("hash local file for collision check: %w", digestErr)
				}
				if baiduOpenMD5IsValid(existing.MD5) && strings.EqualFold(existing.MD5, resolved.MD5) {
					return RemoteFile{ID: existing.ID, Size: existing.Size, SHA1: localSHA1, LocalSHA1: localSHA1, Outcome: store.UploadOutcomeUnchanged}, nil
				}
			}
		}
	}

	resolved, err := ensureDigest()
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("hash local file for upload: %w", err)}
	}
	precreated, err := p.precreate(ctx, remotePath, size, resolved, precreateRType)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("Baidu Open precreate %s: %w", remotePath, err)}
	}
	if precreated == nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: errors.New("Baidu Open precreate returned an empty response")}
	}
	if precreated.ReturnType == 2 {
		createdID := baiduOpenFSID(precreated.FSID)
		if createdID == "" && precreated.Info != nil {
			createdID = baiduOpenFSID(precreated.Info.FSID)
		}
		if createdID == "" {
			return RemoteFile{}, &UploadAttemptError{
				Outcome:   intendedOutcome,
				LocalSHA1: localSHA1,
				Err:       fmt.Errorf("Baidu Open precreate rapid upload returned no fs_id (%s)", baiduOpenPrecreateResponseSummary(precreated)),
			}
		}
		if precreated.Info != nil && strings.TrimSpace(precreated.Info.Path) != "" && normalizeBaiduOpenPath(precreated.Info.Path) != remotePath {
			return RemoteFile{}, &UploadAttemptError{
				Outcome:   intendedOutcome,
				LocalSHA1: localSHA1,
				Err:       fmt.Errorf("Baidu Open precreate rapid upload returned unexpected path %q, want %q (fs_id=%s)", precreated.Info.Path, remotePath, createdID),
			}
		}
		remote, verifyErr := p.waitForRemoteFileByID(ctx, createdID, remotePath, size, resolved.MD5)
		if verifyErr != nil {
			return RemoteFile{}, &UploadAttemptError{
				Outcome:   intendedOutcome,
				LocalSHA1: localSHA1,
				Err:       fmt.Errorf("verify Baidu Open rapid upload %s: %w; precreate=%s", remotePath, verifyErr, baiduOpenPrecreateResponseSummary(precreated)),
			}
		}
		remote.LocalSHA1 = localSHA1
		remote.SHA1 = localSHA1
		remote.Outcome = intendedOutcome
		return remote, nil
	}
	if strings.TrimSpace(precreated.UploadID) == "" {
		return RemoteFile{}, &UploadAttemptError{
			Outcome:   intendedOutcome,
			LocalSHA1: localSHA1,
			Err:       fmt.Errorf("Baidu Open precreate returned no uploadid (%s)", baiduOpenPrecreateResponseSummary(precreated)),
		}
	}
	serverURL, err := p.locateUpload(ctx, remotePath, precreated.UploadID)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("Baidu Open locate upload server for %s: %w", remotePath, err)}
	}
	// return_type is an internal Baidu status value. The response block_list
	// is the client-facing instruction for which blocks still need uploading.
	// Baidu documents an empty response block_list as equivalent to [0].
	parts := precreated.BlockList
	if len(parts) == 0 {
		parts = []int{0}
	}
	if len(resolved.ChunkMD5s) == 0 {
		return RemoteFile{}, &UploadAttemptError{
			Outcome:   intendedOutcome,
			LocalSHA1: localSHA1,
			Err:       fmt.Errorf("Baidu Open precreate requested block 0 for an empty file (%s)", baiduOpenPrecreateResponseSummary(precreated)),
		}
	}
	createBlockMD5s := append([]string(nil), resolved.ChunkMD5s...)
	for _, part := range parts {
		if part < 0 || part >= len(resolved.ChunkMD5s) {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("Baidu Open precreate returned invalid block index %d", part)}
		}
		returnedMD5, err := p.uploadChunk(ctx, remotePath, precreated.UploadID, part, file, resolved, serverURL)
		if err != nil {
			return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("Baidu Open upload block %d for %s: %w", part, remotePath, err)}
		}
		createBlockMD5s[part] = returnedMD5
	}
	created, err := p.createFile(ctx, remotePath, size, precreated.UploadID, createBlockMD5s, createRType)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{Outcome: intendedOutcome, LocalSHA1: localSHA1, Err: fmt.Errorf("Baidu Open create %s: %w", remotePath, err)}
	}
	if created == nil || baiduOpenFSID(created.FSID) == "" {
		return RemoteFile{}, &UploadAttemptError{
			Outcome:   intendedOutcome,
			LocalSHA1: localSHA1,
			Err:       fmt.Errorf("Baidu Open create returned no fs_id (%s)", baiduOpenCreateResponseSummary(created)),
		}
	}
	createdID := baiduOpenFSID(created.FSID)
	if strings.TrimSpace(created.Path) != "" && normalizeBaiduOpenPath(created.Path) != remotePath {
		return RemoteFile{}, &UploadAttemptError{
			Outcome:   intendedOutcome,
			LocalSHA1: localSHA1,
			Err:       fmt.Errorf("Baidu Open create returned unexpected path %q, want %q (fs_id=%s)", created.Path, remotePath, createdID),
		}
	}
	remote, err := p.waitForRemoteFileByID(ctx, createdID, remotePath, size)
	if err != nil {
		return RemoteFile{}, &UploadAttemptError{
			Outcome:   intendedOutcome,
			LocalSHA1: localSHA1,
			Err:       fmt.Errorf("verify Baidu Open upload %s: %w; create=%s; precreate=%s", remotePath, err, baiduOpenCreateResponseSummary(created), baiduOpenPrecreateResponseSummary(precreated)),
		}
	}
	remote.LocalSHA1 = localSHA1
	remote.SHA1 = localSHA1
	remote.Outcome = intendedOutcome
	return remote, nil
}

func calculateBaiduOpenDigest(ctx context.Context, file *os.File) (*baiduOpenDigest, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	fullMD5 := md5.New()
	fullSHA1 := sha1.New()
	sliceMD5 := md5.New()
	remainingSlice := int64(256 * 1024)
	chunkMD5s := make([]string, 0)
	buffer := make([]byte, baiduOpenChunkSize)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, err := io.ReadFull(file, buffer)
		if err == io.ErrUnexpectedEOF {
			if read == 0 {
				break
			}
			err = nil
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		part := buffer[:read]
		partHash := md5.Sum(part)
		chunkMD5s = append(chunkMD5s, hex.EncodeToString(partHash[:]))
		if _, err := fullMD5.Write(part); err != nil {
			return nil, err
		}
		if _, err := fullSHA1.Write(part); err != nil {
			return nil, err
		}
		if remainingSlice > 0 {
			sliceRead := int64(read)
			if sliceRead > remainingSlice {
				sliceRead = remainingSlice
			}
			if _, err := sliceMD5.Write(part[:sliceRead]); err != nil {
				return nil, err
			}
			remainingSlice -= sliceRead
		}
		size += int64(read)
		if read < len(buffer) {
			break
		}
	}
	return &baiduOpenDigest{
		Size:      size,
		SHA1:      strings.ToUpper(hex.EncodeToString(fullSHA1.Sum(nil))),
		MD5:       hex.EncodeToString(fullMD5.Sum(nil)),
		SliceMD5:  hex.EncodeToString(sliceMD5.Sum(nil)),
		ChunkMD5s: chunkMD5s,
	}, nil
}

func (p *baiduOpenProvider) ensureDirectory(ctx context.Context, remotePath string) error {
	remotePath = normalizeBaiduOpenPath(remotePath)
	if remotePath == "/" {
		return nil
	}
	currentPath := "/"
	for _, segment := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		nextPath := normalizeBaiduOpenPath(pathpkg.Join(currentPath, segment))
		entry, found, err := p.findEntry(ctx, currentPath, segment)
		if err != nil {
			return fmt.Errorf("resolve Baidu Open directory %s: %w", currentPath, err)
		}
		if found {
			if !entry.IsDir {
				return fmt.Errorf("Baidu Open path component is a file: %s", nextPath)
			}
			currentPath = nextPath
			continue
		}
		if _, err := p.createDirectory(ctx, nextPath); err != nil {
			// Another worker may have created the directory between list and
			// create. Re-list before returning the original error.
			created, foundAfterError, lookupErr := p.findEntry(ctx, currentPath, segment)
			if lookupErr == nil && foundAfterError && created.IsDir {
				currentPath = nextPath
				continue
			}
			return fmt.Errorf("create Baidu Open directory %s: %w", nextPath, err)
		}
		currentPath = nextPath
	}
	return nil
}

func (p *baiduOpenProvider) findEntry(ctx context.Context, directoryPath, name string) (baiduOpenEntry, bool, error) {
	items, err := p.listFiles(ctx, directoryPath)
	if err != nil {
		return baiduOpenEntry{}, false, err
	}
	for _, item := range items {
		if item.Name == name {
			return item, true, nil
		}
	}
	return baiduOpenEntry{}, false, nil
}

func (p *baiduOpenProvider) listFiles(ctx context.Context, directoryPath string) ([]baiduOpenEntry, error) {
	directoryPath = normalizeBaiduOpenPath(directoryPath)
	items := make([]baiduOpenEntry, 0)
	for start := 0; ; {
		page, err := p.listFilesPage(ctx, directoryPath, start, baiduOpenPageSize)
		if err != nil {
			return nil, err
		}
		for _, item := range page.List {
			entry := baiduOpenEntryFromFileItem(directoryPath, item)
			if entry.ID == "" || entry.Name == "" {
				continue
			}
			items = append(items, entry)
		}
		if len(page.List) < baiduOpenPageSize {
			break
		}
		start += len(page.List)
	}
	return items, nil
}

func (p *baiduOpenProvider) listFilesPage(ctx context.Context, directoryPath string, start, limit int) (*baiduOpenListResponse, error) {
	query := url.Values{}
	query.Set("method", "list")
	query.Set("dir", normalizeBaiduOpenPath(directoryPath))
	query.Set("start", strconv.Itoa(start))
	query.Set("limit", strconv.Itoa(limit))
	query.Set("order", "name")
	query.Set("desc", "0")
	query.Set("web", "1")
	var response baiduOpenListResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduOpenAPIBaseURL+"/file", query, nil, "", nil, &response); err != nil {
		return nil, err
	}
	for index := range response.List {
		normalizeBaiduOpenFileItemMD5(&response.List[index])
	}
	return &response, nil
}

func (p *baiduOpenProvider) fileMetas(ctx context.Context, fsID string) (*baiduOpenFileMetasResponse, error) {
	fsID = strings.TrimSpace(fsID)
	if fsID == "" {
		return nil, errors.New("Baidu Open file metadata requires fs_id")
	}
	fsIDs, err := json.Marshal([]json.Number{json.Number(fsID)})
	if err != nil {
		return nil, fmt.Errorf("encode Baidu Open fs_id %q: %w", fsID, err)
	}
	query := url.Values{}
	query.Set("method", "filemetas")
	query.Set("fsids", string(fsIDs))
	query.Set("dlink", "0")
	query.Set("thumb", "0")
	query.Set("extra", "0")
	query.Set("needmedia", "0")
	var response baiduOpenFileMetasResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduOpenAPIBaseURL+"/multimedia", query, nil, "", nil, &response); err != nil {
		return nil, err
	}
	for index := range response.List {
		normalizeBaiduOpenFileItemMD5(&response.List[index])
	}
	return &response, nil
}

func (p *baiduOpenProvider) createDirectory(ctx context.Context, directoryPath string) (*baiduOpenCreateResponse, error) {
	query := url.Values{}
	query.Set("method", "create")
	form := url.Values{}
	form.Set("path", normalizeBaiduOpenPath(directoryPath))
	form.Set("isdir", "1")
	form.Set("size", "0")
	form.Set("block_list", "[]")
	form.Set("rtype", "0")
	var response baiduOpenCreateResponse
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduOpenAPIBaseURL+"/file", query, []byte(form.Encode()), "application/x-www-form-urlencoded", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (p *baiduOpenProvider) precreate(ctx context.Context, remotePath string, size int64, digest *baiduOpenDigest, rtype string) (*baiduOpenPrecreateResponse, error) {
	blockList, err := json.Marshal(digest.ChunkMD5s)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("method", "precreate")
	form := url.Values{}
	form.Set("path", normalizeBaiduOpenPath(remotePath))
	form.Set("size", strconv.FormatInt(size, 10))
	form.Set("isdir", "0")
	form.Set("autoinit", "1")
	form.Set("block_list", string(blockList))
	form.Set("content-md5", strings.TrimSpace(digest.MD5))
	form.Set("slice-md5", strings.TrimSpace(digest.SliceMD5))
	if strings.TrimSpace(rtype) != "" {
		form.Set("rtype", strings.TrimSpace(rtype))
	}
	var response baiduOpenPrecreateResponse
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduOpenAPIBaseURL+"/file", query, []byte(form.Encode()), "application/x-www-form-urlencoded", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (p *baiduOpenProvider) createFile(ctx context.Context, remotePath string, size int64, uploadID string, chunkMD5s []string, rtype string) (*baiduOpenCreateResponse, error) {
	blockList, err := json.Marshal(chunkMD5s)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("method", "create")
	form := url.Values{}
	form.Set("path", normalizeBaiduOpenPath(remotePath))
	form.Set("size", strconv.FormatInt(size, 10))
	form.Set("isdir", "0")
	form.Set("uploadid", strings.TrimSpace(uploadID))
	form.Set("block_list", string(blockList))
	if strings.TrimSpace(rtype) != "" {
		form.Set("rtype", strings.TrimSpace(rtype))
	}
	var response baiduOpenCreateResponse
	if err := p.doJSONRequest(ctx, http.MethodPost, baiduOpenAPIBaseURL+"/file", query, []byte(form.Encode()), "application/x-www-form-urlencoded", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

type baiduOpenUploadServer struct {
	Server string `json:"server"`
}

type baiduOpenUploadResponse struct {
	baiduOpenAPIResponse
	MD5 string `json:"md5"`
}

type baiduOpenLocateUploadResponse struct {
	baiduOpenAPIResponse
	Servers []baiduOpenUploadServer `json:"servers"`
}

func (p *baiduOpenProvider) locateUpload(ctx context.Context, remotePath, uploadID string) (string, error) {
	query := url.Values{}
	query.Set("method", "locateupload")
	query.Set("appid", "250528")
	query.Set("path", normalizeBaiduOpenPath(remotePath))
	query.Set("uploadid", strings.TrimSpace(uploadID))
	query.Set("upload_version", "2.0")
	var response baiduOpenLocateUploadResponse
	if err := p.doJSONRequest(ctx, http.MethodGet, baiduOpenLocateUploadURL, query, nil, "", nil, &response); err != nil {
		return "", err
	}
	for _, candidate := range response.Servers {
		server := strings.TrimRight(strings.TrimSpace(candidate.Server), "/")
		parsed, err := url.Parse(server)
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || strings.TrimSpace(parsed.Host) == "" {
			continue
		}
		if parsed.Path == "" || parsed.Path == "/" {
			parsed.Path = "/rest/2.0/pcs/superfile2"
		}
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	return "", errors.New("Baidu Open locate upload returned no HTTPS upload server")
}

func baiduOpenMultipartEnvelope(filename string) (prefix, suffix []byte, contentType string, err error) {
	var envelope bytes.Buffer
	multipartWriter := multipart.NewWriter(&envelope)
	if _, err := multipartWriter.CreateFormFile("file", filename); err != nil {
		return nil, nil, "", err
	}
	prefixLength := envelope.Len()
	if err := multipartWriter.Close(); err != nil {
		return nil, nil, "", err
	}
	envelopeBytes := envelope.Bytes()
	prefix = append([]byte(nil), envelopeBytes[:prefixLength]...)
	suffix = append([]byte(nil), envelopeBytes[prefixLength:]...)
	return prefix, suffix, multipartWriter.FormDataContentType(), nil
}

func (p *baiduOpenProvider) uploadChunk(ctx context.Context, remotePath, uploadID string, part int, file *os.File, digest *baiduOpenDigest, serverURL string) (string, error) {
	partOffset := int64(part) * baiduOpenChunkSize
	partSize := int64(baiduOpenChunkSize)
	remaining := digest.Size - partOffset
	if remaining < partSize {
		partSize = remaining
	}
	if partSize <= 0 {
		return "", fmt.Errorf("invalid Baidu Open block size for block %d", part)
	}
	prefix, suffix, contentType, err := baiduOpenMultipartEnvelope(pathpkg.Base(remotePath))
	if err != nil {
		return "", err
	}
	query := url.Values{}
	query.Set("method", "upload")
	query.Set("type", "tmpfile")
	query.Set("path", normalizeBaiduOpenPath(remotePath))
	query.Set("uploadid", strings.TrimSpace(uploadID))
	query.Set("partseq", strconv.Itoa(part))
	if strings.TrimSpace(serverURL) == "" {
		return "", errors.New("Baidu Open upload server is required")
	}
	reporter := p.progressReporter
	bodyFactory := func() (io.ReadCloser, int64) {
		if reporter != nil {
			reporter(partOffset)
		}
		payload := &baiduOpenProgressReader{
			reader: io.NewSectionReader(file, partOffset, partSize),
			offset: partOffset,
			report: reporter,
		}
		body := io.MultiReader(bytes.NewReader(prefix), payload, bytes.NewReader(suffix))
		bodyLength := int64(len(prefix)) + partSize + int64(len(suffix))
		return io.NopCloser(body), bodyLength
	}
	var response baiduOpenUploadResponse
	if err := p.doJSONRequestWithBodyFactory(ctx, http.MethodPost, strings.TrimSpace(serverURL), query, bodyFactory, contentType, nil, &response); err != nil {
		return "", err
	}
	returnedMD5 := strings.TrimSpace(response.MD5)
	expectedMD5 := strings.TrimSpace(digest.ChunkMD5s[part])
	if returnedMD5 == "" {
		return "", fmt.Errorf("Baidu Open upload block %d returned no md5", part)
	}
	if !strings.EqualFold(returnedMD5, expectedMD5) {
		return "", fmt.Errorf("Baidu Open upload block %d returned md5 %q, expected %q", part, returnedMD5, expectedMD5)
	}
	return strings.ToLower(returnedMD5), nil
}

func (p *baiduOpenProvider) waitForRemoteFileByID(ctx context.Context, fsID, expectedPath string, size int64, expectedMD5 ...string) (RemoteFile, error) {
	fsID = strings.TrimSpace(fsID)
	expectedPath = normalizeBaiduOpenPath(expectedPath)
	for attempt := 0; attempt < baiduOpenVerifyAttempts; attempt++ {
		response, err := p.fileMetas(ctx, fsID)
		if err != nil {
			return RemoteFile{}, err
		}
		var item *baiduOpenFileItem
		for index := range response.List {
			candidate := &response.List[index]
			if baiduOpenFSID(candidate.FSID) == fsID {
				item = candidate
				break
			}
		}
		if item != nil {
			actualPath := normalizeBaiduOpenPath(item.Path)
			if actualPath != expectedPath {
				return RemoteFile{}, fmt.Errorf("Baidu Open file metadata path %q does not match %q (fs_id=%s)", item.Path, expectedPath, fsID)
			}
			if item.IsDir != 0 {
				return RemoteFile{}, fmt.Errorf("Baidu Open file metadata is a directory (fs_id=%s)", fsID)
			}
			actualSize, sizeErr := strconv.ParseInt(strings.TrimSpace(string(item.Size)), 10, 64)
			if sizeErr != nil {
				return RemoteFile{}, fmt.Errorf("Baidu Open file metadata returned invalid size %q (fs_id=%s)", item.Size, fsID)
			}
			if actualSize != size {
				return RemoteFile{}, fmt.Errorf("Baidu Open file metadata size %d does not match %d (fs_id=%s)", actualSize, size, fsID)
			}
			if len(expectedMD5) > 0 && baiduOpenMD5IsValid(item.MD5) && strings.TrimSpace(expectedMD5[0]) != "" && !strings.EqualFold(item.MD5, expectedMD5[0]) {
				return RemoteFile{}, fmt.Errorf("Baidu Open file metadata md5 %q does not match %q (fs_id=%s)", item.MD5, expectedMD5[0], fsID)
			}
			return RemoteFile{ID: fsID, Size: actualSize}, nil
		}
		if attempt+1 < baiduOpenVerifyAttempts {
			delay := baiduOpenVerifyRetryDelay
			if p.verifyRetryDelay != nil {
				delay = p.verifyRetryDelay(attempt)
			}
			if err := waitBaiduOpen(ctx, delay); err != nil {
				return RemoteFile{}, err
			}
		}
	}
	return RemoteFile{}, fmt.Errorf("%w: fs_id=%s path=%s", errBaiduOpenFileMetadataNotVisible, fsID, expectedPath)
}

func (p *baiduOpenProvider) doJSONRequest(ctx context.Context, method, endpoint string, query url.Values, body []byte, contentType string, headers http.Header, out any) error {
	return p.doJSONRequestWithBodyFactory(ctx, method, endpoint, query, baiduOpenBytesBodyFactory(body), contentType, headers, out)
}

func baiduOpenBytesBodyFactory(body []byte) baiduOpenRequestBodyFactory {
	if body == nil {
		return nil
	}
	return func() (io.ReadCloser, int64) {
		return io.NopCloser(bytes.NewReader(body)), int64(len(body))
	}
}

func (p *baiduOpenProvider) doJSONRequestWithBodyFactory(ctx context.Context, method, endpoint string, query url.Values, bodyFactory baiduOpenRequestBodyFactory, contentType string, headers http.Header, out any) error {
	return p.doJSONRequestAttempt(ctx, method, endpoint, query, bodyFactory, contentType, headers, out, false, 0)
}

func (p *baiduOpenProvider) doJSONRequestAttempt(ctx context.Context, method, endpoint string, query url.Values, bodyFactory baiduOpenRequestBodyFactory, contentType string, headers http.Header, out any, authRetried bool, attempt int) error {
	accessToken, err := p.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	requestQuery := cloneBaiduOpenValues(query)
	requestQuery.Set("access_token", accessToken)
	parsed.RawQuery = requestQuery.Encode()
	var requestBody io.ReadCloser
	var contentLength int64
	if bodyFactory != nil {
		requestBody, contentLength = bodyFactory()
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), requestBody)
	if err != nil {
		return err
	}
	if bodyFactory != nil {
		req.ContentLength = contentLength
	}
	userAgent := p.userAgent
	if userAgent == "" {
		userAgent = baiduOpenDefaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if err := p.waitRequest(ctx); err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		if attempt+1 < baiduOpenMaxAttempts && isRetryableBaiduOpenTransportError(err) {
			if err := waitBaiduOpen(ctx, baiduOpenRetryDelay*time.Duration(1<<attempt)); err != nil {
				return err
			}
			return p.doJSONRequestAttempt(ctx, method, endpoint, query, bodyFactory, contentType, headers, out, authRetried, attempt+1)
		}
		return fmt.Errorf("Baidu Open request failed: %w", err)
	}
	responseBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read Baidu Open response: %w", err)
	}
	var envelope baiduOpenAPIResponse
	decodeErr := json.Unmarshal(responseBody, &envelope)
	code := envelope.code()
	message := envelope.message()
	if !authRetried && shouldRefreshBaiduOpenToken(resp.StatusCode, code) {
		if err := p.refreshAccessToken(ctx, accessToken, true); err != nil {
			return err
		}
		return p.doJSONRequestAttempt(ctx, method, endpoint, query, bodyFactory, contentType, headers, out, true, 0)
	}
	if attempt+1 < baiduOpenMaxAttempts && isRetryableBaiduOpenResponse(resp.StatusCode, code, message) {
		if err := waitBaiduOpen(ctx, baiduOpenRetryDelay*time.Duration(1<<attempt)); err != nil {
			return err
		}
		return p.doJSONRequestAttempt(ctx, method, endpoint, query, bodyFactory, contentType, headers, out, authRetried, attempt+1)
	}
	if decodeErr != nil {
		return fmt.Errorf("decode Baidu Open response status=%d: %w", resp.StatusCode, decodeErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Baidu Open API returned HTTP %d", resp.StatusCode)
	}
	if code != 0 {
		return &baiduOpenAPIError{StatusCode: resp.StatusCode, Code: code, Message: message}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode Baidu Open response data: %w", err)
	}
	return nil
}

func (p *baiduOpenProvider) ensureAccessToken(ctx context.Context) (string, error) {
	accessToken, refreshToken, expiresAt := p.tokens.snapshot()
	if baiduOpenTokenUsable(accessToken, expiresAt, time.Now()) {
		return accessToken, nil
	}
	if strings.TrimSpace(refreshToken) == "" {
		return "", errors.New("baiduopen refresh_token is required")
	}
	if err := p.refreshAccessToken(ctx, accessToken, false); err != nil {
		return "", err
	}
	accessToken, _, _ = p.tokens.snapshot()
	if accessToken == "" {
		return "", errors.New("baiduopen access_token unavailable")
	}
	return accessToken, nil
}

func (p *baiduOpenProvider) refreshAccessToken(ctx context.Context, staleAccessToken string, force bool) error {
	p.tokens.mu.Lock()
	defer p.tokens.mu.Unlock()
	if force {
		if p.tokens.accessToken != "" && p.tokens.accessToken != staleAccessToken && baiduOpenTokenUsable(p.tokens.accessToken, p.tokens.expiresAt, time.Now()) {
			return nil
		}
	} else if baiduOpenTokenUsable(p.tokens.accessToken, p.tokens.expiresAt, time.Now()) {
		return nil
	}
	if p.clientID == "" || p.clientSecret == "" {
		return errors.New("baiduopen client_id and client_secret are required")
	}
	if p.tokens.refreshToken == "" {
		return errors.New("baiduopen refresh_token is required")
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", p.tokens.refreshToken)
	values.Set("client_id", p.clientID)
	values.Set("client_secret", p.clientSecret)
	token, err := requestBaiduOpenOAuthToken(ctx, p.httpClient, values)
	if err != nil {
		return err
	}
	p.tokens.accessToken = strings.TrimSpace(token.AccessToken)
	if strings.TrimSpace(token.RefreshToken) != "" {
		p.tokens.refreshToken = strings.TrimSpace(token.RefreshToken)
	}
	p.tokens.expiresAt = baiduOpenExpiresAt(token.ExpiresIn, time.Now())
	if p.onTokenRefreshed != nil {
		p.onTokenRefreshed(p.tokens.accessToken, p.tokens.refreshToken, p.tokens.expiresAt)
	}
	return nil
}

type baiduOpenOAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type baiduOpenOAuthResponse struct {
	baiduOpenOAuthToken
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// BaiduOpenOAuthToken is the token response returned by Baidu's OAuth API.
type BaiduOpenOAuthToken = baiduOpenOAuthToken

// BaiduOpenAuthorizationURL builds the authorization URL for a Baidu Open app.
func BaiduOpenAuthorizationURL(clientID, redirectURI, state string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", strings.TrimSpace(clientID))
	values.Set("redirect_uri", strings.TrimSpace(redirectURI))
	values.Set("scope", "basic,netdisk")
	values.Set("state", strings.TrimSpace(state))
	return "https://openapi.baidu.com/oauth/2.0/authorize?" + values.Encode()
}

// ExchangeBaiduOpenAuthorizationCode exchanges an OAuth authorization code.
func ExchangeBaiduOpenAuthorizationCode(ctx context.Context, client *http.Client, clientID, clientSecret, code, redirectURI string) (*BaiduOpenOAuthToken, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", strings.TrimSpace(code))
	values.Set("client_id", strings.TrimSpace(clientID))
	values.Set("client_secret", strings.TrimSpace(clientSecret))
	values.Set("redirect_uri", strings.TrimSpace(redirectURI))
	return requestBaiduOpenOAuthTokenWithClient(ctx, client, values)
}

// RefreshBaiduOpenOAuthToken refreshes a Baidu Open access token.
func RefreshBaiduOpenOAuthToken(ctx context.Context, client *http.Client, clientID, clientSecret, refreshToken string) (*BaiduOpenOAuthToken, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", strings.TrimSpace(refreshToken))
	values.Set("client_id", strings.TrimSpace(clientID))
	values.Set("client_secret", strings.TrimSpace(clientSecret))
	return requestBaiduOpenOAuthTokenWithClient(ctx, client, values)
}

// ValidateBaiduOpenAccessToken checks the token against Baidu's user endpoint.
func ValidateBaiduOpenAccessToken(ctx context.Context, client *http.Client, accessToken string) error {
	if client == nil {
		client = http.DefaultClient
	}
	endpoint, err := url.Parse("https://pan.baidu.com/rest/2.0/xpan/nas")
	if err != nil {
		return err
	}
	query := endpoint.Query()
	query.Set("method", "uinfo")
	query.Set("access_token", strings.TrimSpace(accessToken))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", baiduOpenDefaultUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Baidu Open access token validation failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Baidu Open access token validation response: %w", err)
	}
	var response baiduOpenAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode Baidu Open access token validation response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Baidu Open access token validation returned HTTP %d", resp.StatusCode)
	}
	if code := response.code(); code != 0 {
		return &baiduOpenAPIError{StatusCode: resp.StatusCode, Code: code, Message: response.message()}
	}
	return nil
}

func requestBaiduOpenOAuthToken(ctx context.Context, client *http.Client, values url.Values) (*baiduOpenOAuthToken, error) {
	return requestBaiduOpenOAuthTokenWithClient(ctx, client, values)
}

func requestBaiduOpenOAuthTokenWithClient(ctx context.Context, client *http.Client, values url.Values) (*BaiduOpenOAuthToken, error) {
	if client == nil {
		client = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; attempt < baiduOpenMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baiduOpenOAuthTokenURL, bytes.NewBufferString(values.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", baiduOpenDefaultUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt+1 < baiduOpenMaxAttempts && isRetryableBaiduOpenTransportError(err) {
				if err := waitBaiduOpen(ctx, baiduOpenRetryDelay*time.Duration(1<<attempt)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("Baidu Open OAuth request failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if attempt+1 < baiduOpenMaxAttempts && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError) {
			if err := waitBaiduOpen(ctx, baiduOpenRetryDelay*time.Duration(1<<attempt)); err != nil {
				return nil, err
			}
			continue
		}
		var response baiduOpenOAuthResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode Baidu Open OAuth response status=%d: %w", resp.StatusCode, err)
		}
		if response.Error != "" {
			return nil, fmt.Errorf("Baidu Open OAuth error=%s description=%s", response.Error, response.ErrorDescription)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("Baidu Open OAuth returned HTTP %d", resp.StatusCode)
		}
		if strings.TrimSpace(response.AccessToken) == "" {
			return nil, errors.New("Baidu Open OAuth response missing access_token")
		}
		return &response.baiduOpenOAuthToken, nil
	}
	return nil, fmt.Errorf("Baidu Open OAuth failed after retries: %w", lastErr)
}

func (p *baiduOpenProvider) waitRequest(ctx context.Context) error {
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

func baiduOpenEntryFromFileItem(parentPath string, item baiduOpenFileItem) baiduOpenEntry {
	name := strings.TrimSpace(item.ServerFilename)
	itemPath := normalizeBaiduOpenPath(item.Path)
	if itemPath == "/" || name == "" {
		itemPath = normalizeBaiduOpenPath(pathpkg.Join(parentPath, name))
	}
	return baiduOpenEntry{
		ID:       strings.TrimSpace(string(item.FSID)),
		Name:     name,
		Path:     itemPath,
		IsDir:    item.IsDir != 0,
		Size:     parseBaiduOpenInt64(string(item.Size)),
		MD5:      strings.TrimSpace(item.MD5),
		Category: item.Category,
	}
}

func normalizeBaiduOpenPath(value string) string {
	clean := pathpkg.Clean("/" + strings.TrimSpace(value))
	if clean == "." {
		return "/"
	}
	return clean
}

func cloneBaiduOpenValues(source url.Values) url.Values {
	result := make(url.Values, len(source)+1)
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func parseBaiduOpenInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func baiduOpenTokenUsable(accessToken, expiresAt string, now time.Time) bool {
	if strings.TrimSpace(accessToken) == "" {
		return false
	}
	if strings.TrimSpace(expiresAt) == "" {
		return true
	}
	expires, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return false
	}
	return now.Add(time.Minute).Before(expires)
}

func baiduOpenExpiresAt(expiresIn int64, now time.Time) string {
	if expiresIn <= 0 {
		return ""
	}
	return now.UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
}

func shouldRefreshBaiduOpenToken(statusCode int, code int64) bool {
	return statusCode == http.StatusUnauthorized || code == -6 || code == 110 || code == 111
}

func isRetryableBaiduOpenResponse(statusCode int, code int64, message string) bool {
	if statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError {
		return true
	}
	if code == 429 || code == 31034 {
		return true
	}
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{"rate limit", "too many", "temporarily unavailable", "timeout", "频繁", "频控"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isRetryableBaiduOpenTransportError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"connection reset", "connection refused", "connection aborted", "server closed idle connection", "unexpected eof", "eof"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func waitBaiduOpen(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
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
