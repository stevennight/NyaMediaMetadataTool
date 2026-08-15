package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	baiduWebBaseURL       = "https://pan.baidu.com"
	baiduPCSBaseURL       = "https://pcs.baidu.com"
	baiduDefaultUploadURL = "https://c2.pcs.baidu.com/rest/2.0/pcs/superfile2"
	baiduAppID            = "250528"
	baiduChunkSize        = int64(4 * 1024 * 1024)
	baiduSampleSize       = int64(256 * 1024)
)

var initialBlockMD5s = []string{
	"5910a591dd8fc18c32a8f3df4fdc1761",
	"a5fc157d78e6ad1c7e114b056c92821e",
}

type apiEnvelope struct {
	Errno     int64  `json:"errno"`
	ErrMsg    string `json:"errmsg"`
	ErrorCode int64  `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (e apiEnvelope) code() int64 {
	if e.Errno != 0 {
		return e.Errno
	}
	return e.ErrorCode
}

func (e apiEnvelope) message() string {
	if strings.TrimSpace(e.ErrMsg) != "" {
		return strings.TrimSpace(e.ErrMsg)
	}
	return strings.TrimSpace(e.ErrorMsg)
}

type probeDigest struct {
	Size       int64
	ModTime    time.Time
	FullMD5    string
	SliceMD5   string
	ChunkCount int
}

type probe struct {
	client    *http.Client
	cookie    string
	bdstoken  string
	uk        string
	userAgent string
	sequence  atomic.Uint64
}

type templateResponse struct {
	apiEnvelope
	Result map[string]json.RawMessage `json:"result"`
}

type precreateResponse struct {
	apiEnvelope
	ReturnType int    `json:"return_type"`
	UploadID   string `json:"uploadid"`
	BlockList  []int  `json:"block_list"`
}

type locateResponse struct {
	apiEnvelope
	Server  []string `json:"server"`
	Servers []struct {
		Server string `json:"server"`
	} `json:"servers"`
}

type rapidResponse struct {
	apiEnvelope
	ReturnType int `json:"return_type"`
	Info       struct {
		FSID json.RawMessage `json:"fs_id"`
		Path string          `json:"path"`
		Size json.RawMessage `json:"size"`
	} `json:"info"`
}

func main() {
	filePath := flag.String("file", "", "local file path")
	remotePath := flag.String("remote", "", "remote path, for example /Video/NEW/example.mkv")
	warmupParts := flag.Int("warmup-parts", 0, "number of 4 MiB temporary parts to upload before rapidupload; 6 approximates the captured browser flow")
	timeout := flag.Duration("timeout", 5*time.Minute, "HTTP request timeout")
	userAgent := flag.String("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/152.0.0.0 Safari/537.36", "HTTP User-Agent")
	flag.Parse()

	if strings.TrimSpace(*filePath) == "" || strings.TrimSpace(*remotePath) == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *warmupParts < 0 {
		fatalf("-warmup-parts must be non-negative")
	}
	cookie := strings.TrimSpace(os.Getenv("BAIDU_COOKIE"))
	if cookie == "" {
		fatalf("BAIDU_COOKIE is required; set it locally and do not commit it")
	}

	remote := normalizeRemotePath(*remotePath)
	file, err := os.Open(*filePath)
	if err != nil {
		fatalf("open local file: %v", err)
	}
	defer file.Close()

	digest, err := calculateDigest(file)
	if err != nil {
		fatalf("calculate digest: %v", err)
	}
	if info, statErr := file.Stat(); statErr != nil || info.Size() != digest.Size {
		fatalf("local file changed while hashing")
	}

	p := &probe{
		client:    &http.Client{Timeout: *timeout},
		cookie:    cookie,
		bdstoken:  strings.TrimSpace(os.Getenv("BAIDU_BDSTOKEN")),
		uk:        strings.TrimSpace(os.Getenv("BAIDU_UK")),
		userAgent: strings.TrimSpace(*userAgent),
	}
	if err := p.bootstrap(context.Background()); err != nil {
		fatalf("resolve Baidu web session values: %v", err)
	}

	encodedContentMD5 := encodeBaiduMD5(digest.FullMD5)
	encodedSliceMD5 := encodeBaiduMD5(digest.SliceMD5)
	fmt.Printf("local size=%d chunks=%d full_md5=%s encoded_content_md5=%s encoded_slice_md5=%s uk=%s\n",
		digest.Size, digest.ChunkCount, digest.FullMD5, encodedContentMD5, encodedSliceMD5, p.uk)

	precreated, err := p.precreate(context.Background(), remote, digest)
	if err != nil {
		fatalf("precreate: %v", err)
	}
	fmt.Printf("precreate errno=0 return_type=%d uploadid_present=%t returned_block_list=%v\n",
		precreated.ReturnType, precreated.UploadID != "", precreated.BlockList)
	if precreated.UploadID == "" {
		fatalf("precreate returned no uploadid")
	}

	if *warmupParts > 0 {
		serverURL, locateErr := p.locateUpload(context.Background(), remote, precreated.UploadID)
		if locateErr != nil {
			fmt.Printf("locateupload warning=%v; using default=%s\n", locateErr, baiduDefaultUploadURL)
		}
		warmupCount := *warmupParts
		if warmupCount > digest.ChunkCount {
			warmupCount = digest.ChunkCount
		}
		for part := 0; part < warmupCount; part++ {
			if err := p.uploadPart(context.Background(), serverURL, remote, precreated.UploadID, part, file, digest.Size); err != nil {
				fatalf("warmup part %d: %v", part, err)
			}
			fmt.Printf("warmup part=%d/%d complete\n", part+1, warmupCount)
		}
	}

	result, returnType, err := p.rapidUpload(context.Background(), remote, precreated.UploadID, file, digest, encodedContentMD5, encodedSliceMD5)
	if err != nil {
		fatalf("rapidupload probe failed: %v\nNo final create was called; any warmup data is only an unfinished upload session.", err)
	}
	fmt.Printf("rapidupload success fs_id=%s path=%s size=%d return_type=%d\n", result, remote, digest.Size, returnType)
}

func (p *probe) bootstrap(ctx context.Context) error {
	if p.bdstoken != "" && p.uk != "" {
		return nil
	}
	query := p.webQuery(false)
	query.Set("fields", `["bdstoken","uk"]`)
	body, err := p.request(ctx, http.MethodGet, baiduWebBaseURL+"/api/gettemplatevariable", query, nil, "")
	if err != nil {
		return err
	}
	var response templateResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode response: %w; body=%s", err, bodySnippet(body))
	}
	if code := response.code(); code != 0 {
		return fmt.Errorf("api error code=%d message=%s", code, response.message())
	}
	if p.bdstoken == "" {
		p.bdstoken = jsonRawString(response.Result["bdstoken"])
	}
	if p.uk == "" {
		p.uk = jsonRawString(response.Result["uk"])
	}
	if p.bdstoken == "" {
		return errors.New("gettemplatevariable returned no bdstoken")
	}
	if p.uk == "" {
		return errors.New("gettemplatevariable returned no uk")
	}
	return nil
}

func (p *probe) precreate(ctx context.Context, remote string, digest probeDigest) (precreateResponse, error) {
	blockList := initialBlockMD5s
	if digest.Size <= baiduChunkSize {
		blockList = initialBlockMD5s[:1]
	}
	encodedBlockList, err := json.Marshal(blockList)
	if err != nil {
		return precreateResponse{}, err
	}
	form := url.Values{}
	form.Set("path", remote)
	form.Set("autoinit", "1")
	form.Set("target_path", targetPath(remote))
	form.Set("local_mtime", strconv.FormatInt(digest.ModTime.Unix(), 10))
	form.Set("block_list", string(encodedBlockList))
	query := p.webQuery(true)
	query.Set("rtype", "1")
	body, err := p.request(ctx, http.MethodPost, baiduWebBaseURL+"/api/precreate", query, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return precreateResponse{}, err
	}
	var response precreateResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return precreateResponse{}, fmt.Errorf("decode response: %w; body=%s", err, bodySnippet(body))
	}
	if code := response.code(); code != 0 {
		return precreateResponse{}, fmt.Errorf("api error code=%d message=%s; body=%s", code, response.message(), bodySnippet(body))
	}
	return response, nil
}

func (p *probe) locateUpload(ctx context.Context, remote, uploadID string) (string, error) {
	query := url.Values{}
	query.Set("method", "locateupload")
	query.Set("upload_version", "2.0")
	query.Set("app_id", baiduAppID)
	query.Set("path", remote)
	query.Set("uploadid", uploadID)
	body, err := p.request(ctx, http.MethodGet, baiduPCSBaseURL+"/rest/2.0/pcs/file", query, nil, "")
	if err != nil {
		return baiduDefaultUploadURL, err
	}
	var response locateResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return baiduDefaultUploadURL, fmt.Errorf("decode response: %w; body=%s", err, bodySnippet(body))
	}
	if code := response.code(); code != 0 {
		return baiduDefaultUploadURL, fmt.Errorf("api error code=%d message=%s", code, response.message())
	}
	for _, server := range response.Server {
		if endpoint := normalizeUploadURL(server); endpoint != "" {
			return endpoint, nil
		}
	}
	for _, server := range response.Servers {
		if endpoint := normalizeUploadURL(server.Server); endpoint != "" {
			return endpoint, nil
		}
	}
	return baiduDefaultUploadURL, errors.New("locateupload returned no usable server")
}

func (p *probe) uploadPart(ctx context.Context, serverURL, remote, uploadID string, part int, file *os.File, size int64) error {
	offset := int64(part) * baiduChunkSize
	partSize := size - offset
	if partSize > baiduChunkSize {
		partSize = baiduChunkSize
	}
	if partSize <= 0 {
		return fmt.Errorf("invalid part size: offset=%d size=%d", offset, size)
	}

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	partWriter, err := multipartWriter.CreateFormFile("file", filepath.Base(file.Name()))
	if err != nil {
		return err
	}
	if _, err := io.Copy(partWriter, io.NewSectionReader(file, offset, partSize)); err != nil {
		return err
	}
	if err := multipartWriter.Close(); err != nil {
		return err
	}

	query := url.Values{}
	query.Set("method", "upload")
	query.Set("path", remote)
	query.Set("uploadid", uploadID)
	query.Set("uploadsign", "0")
	query.Set("partseq", strconv.Itoa(part))
	query.Set("app_id", baiduAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("dp-logid", p.nextLogID())
	bodyBytes, err := p.request(ctx, http.MethodPost, serverURL, query, bytes.NewReader(body.Bytes()), multipartWriter.FormDataContentType())
	if err != nil {
		return err
	}
	var response struct {
		apiEnvelope
		MD5 string `json:"md5"`
	}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return fmt.Errorf("decode response: %w; body=%s", err, bodySnippet(bodyBytes))
	}
	if code := response.code(); code != 0 {
		return fmt.Errorf("api error code=%d message=%s", code, response.message())
	}
	if strings.TrimSpace(response.MD5) == "" {
		return fmt.Errorf("response did not contain md5; body=%s", bodySnippet(bodyBytes))
	}
	return nil
}

func (p *probe) rapidUpload(ctx context.Context, remote, uploadID string, file *os.File, digest probeDigest, encodedContentMD5, encodedSliceMD5 string) (string, int, error) {
	dataTime := time.Now().Unix()
	offset := calculateDataOffset(p.uk, encodedContentMD5, dataTime, digest.Size)
	data, err := readSample(file, offset, digest.Size)
	if err != nil {
		return "", 0, err
	}
	form := url.Values{}
	form.Set("uploadid", uploadID)
	form.Set("path", remote)
	form.Set("content-length", strconv.FormatInt(digest.Size, 10))
	form.Set("content-md5", encodedContentMD5)
	form.Set("slice-md5", encodedSliceMD5)
	form.Set("target_path", targetPath(remote))
	form.Set("local_mtime", strconv.FormatInt(digest.ModTime.Unix(), 10))
	form.Set("data_time", strconv.FormatInt(dataTime, 10))
	form.Set("data_offset", strconv.FormatInt(offset, 10))
	form.Set("data_content", base64.StdEncoding.EncodeToString(data))
	query := p.webQuery(true)
	query.Set("rtype", "1")
	body, err := p.request(ctx, http.MethodPost, baiduWebBaseURL+"/api/rapidupload", query, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return "", 0, err
	}
	var response rapidResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", 0, fmt.Errorf("decode response: %w; body=%s", err, bodySnippet(body))
	}
	if code := response.code(); code != 0 {
		return "", response.ReturnType, fmt.Errorf("api error code=%d message=%s; body=%s", code, response.message(), bodySnippet(body))
	}
	fsID := jsonRawString(response.Info.FSID)
	if fsID == "" {
		return "", response.ReturnType, fmt.Errorf("errno=0 but info.fs_id is empty; data_time=%d data_offset=%d data_length=%d body=%s", dataTime, offset, len(data), bodySnippet(body))
	}
	if response.Info.Path != "" && normalizeRemotePath(response.Info.Path) != remote {
		return "", response.ReturnType, fmt.Errorf("rapidupload returned unexpected path %q, want %q (fs_id=%s)", response.Info.Path, remote, fsID)
	}
	if sizeText := jsonRawString(response.Info.Size); sizeText != "" && sizeText != strconv.FormatInt(digest.Size, 10) {
		return "", response.ReturnType, fmt.Errorf("rapidupload returned unexpected size %q, want %d (fs_id=%s)", sizeText, digest.Size, fsID)
	}
	fmt.Printf("rapidupload response errno=0 data_time=%d data_offset=%d data_length=%d\n", dataTime, offset, len(data))
	return fsID, response.ReturnType, nil
}

func (p *probe) request(ctx context.Context, method, endpoint string, query url.Values, body io.Reader, contentType string) ([]byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	merged := parsed.Query()
	for key, values := range query {
		merged.Del(key)
		for _, value := range values {
			merged.Add(key, value)
		}
	}
	parsed.RawQuery = merged.Encode()
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Cookie", p.cookie)
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Encoding", "identity")
	if requestBody, ok := body.(*bytes.Reader); ok {
		req.ContentLength = int64(requestBody.Len())
	}
	if requestBody, ok := body.(*strings.Reader); ok {
		req.ContentLength = int64(requestBody.Len())
	}
	response, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d from %s: %s", response.StatusCode, parsed.Path, bodySnippet(responseBody))
	}
	return responseBody, nil
}

func (p *probe) webQuery(needsToken bool) url.Values {
	query := url.Values{}
	query.Set("app_id", baiduAppID)
	query.Set("channel", "chunlei")
	query.Set("web", "1")
	query.Set("clienttype", "0")
	query.Set("dp-logid", p.nextLogID())
	if needsToken {
		query.Set("bdstoken", p.bdstoken)
	}
	return query
}

func (p *probe) nextLogID() string {
	sequence := p.sequence.Add(1) % 10000000
	return fmt.Sprintf("%013d%07d", time.Now().UnixMilli()%10000000000000, sequence)
}

func calculateDigest(file *os.File) (probeDigest, error) {
	info, err := file.Stat()
	if err != nil {
		return probeDigest{}, err
	}
	sampleLength := info.Size()
	if sampleLength > baiduSampleSize {
		sampleLength = baiduSampleSize
	}
	sample := make([]byte, sampleLength)
	if sampleLength > 0 {
		read, readErr := file.ReadAt(sample, 0)
		if readErr != nil && !(errors.Is(readErr, io.EOF) && int64(read) == sampleLength) {
			return probeDigest{}, readErr
		}
		sample = sample[:read]
	}
	full := md5.New()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return probeDigest{}, err
	}
	if _, err := io.Copy(full, file); err != nil {
		return probeDigest{}, err
	}
	chunkCount := 0
	if info.Size() > 0 {
		chunkCount = int((info.Size() + baiduChunkSize - 1) / baiduChunkSize)
	}
	sliceHash := md5.Sum(sample)
	return probeDigest{
		Size:       info.Size(),
		ModTime:    info.ModTime(),
		FullMD5:    hex.EncodeToString(full.Sum(nil)),
		SliceMD5:   hex.EncodeToString(sliceHash[:]),
		ChunkCount: chunkCount,
	}, nil
}

func readSample(file *os.File, offset, size int64) ([]byte, error) {
	length := size - offset
	if length > baiduSampleSize {
		length = baiduSampleSize
	}
	if length <= 0 {
		return []byte{}, nil
	}
	data := make([]byte, length)
	read, err := file.ReadAt(data, offset)
	if err != nil && !(errors.Is(err, io.EOF) && int64(read) == length) {
		return nil, err
	}
	return data[:read], nil
}

func calculateDataOffset(uk, encodedContentMD5 string, dataTime, fileSize int64) int64 {
	if fileSize <= baiduSampleSize {
		return 0
	}
	seed := uk + encodedContentMD5 + strconv.FormatInt(dataTime, 10)
	digest := md5.Sum([]byte(seed))
	raw := binary.BigEndian.Uint32(digest[:4])
	return int64(uint64(raw) % uint64(fileSize-baiduSampleSize+1))
}

func encodeBaiduMD5(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 32 {
		return value
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return value
		}
	}
	reordered := value[8:16] + value[0:8] + value[24:32] + value[16:24]
	encoded := make([]byte, len(reordered))
	for index := range reordered {
		nibble, _ := strconv.ParseUint(string(reordered[index]), 16, 4)
		encoded[index] = "0123456789abcdef"[byte(nibble)^byte(15&index)]
	}
	nibble, _ := strconv.ParseUint(string([]byte{encoded[9]}), 16, 4)
	encoded[9] = byte('g' + nibble)
	return string(encoded)
}

func normalizeRemotePath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func targetPath(remote string) string {
	parent := pathpkg.Dir(normalizeRemotePath(remote))
	if parent == "." {
		parent = "/"
	}
	return strings.TrimRight(parent, "/") + "/"
}

func normalizeUploadURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return ""
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/rest/2.0/pcs/superfile2"
	}
	return strings.TrimRight(parsed.String(), "/")
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

func bodySnippet(body []byte) string {
	const maxLength = 1200
	value := strings.TrimSpace(string(body))
	if len(value) > maxLength {
		return value[:maxLength] + "..."
	}
	return value
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
