package upload

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	ec115 "github.com/SheltonZhu/115driver/pkg/crypto/ec115"
	pan115 "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

const (
	upload115TokenSalt           = "Qclm8MGWUv59TnrR0XPg"
	max115UploadInitSignAttempts = 3
	singleRequest115UploadLimit  = 16 * 1024 * 1024
	multipart115PartSize         = 16 * 1024 * 1024
	max115OSSOperationAttempts   = 3
	max115MultipartParts         = 10000
	oss115TokenRefreshWindow     = 15 * time.Minute
	uncertain115CommitMarker     = "115 remote commit result is uncertain"
)

type upload115Cipher interface {
	EncodeToken(timestamp int64) (string, error)
	Encrypt(plainText []byte) ([]byte, error)
	Decrypt(cipherText []byte) ([]byte, error)
}

type uncertain115CommitError struct {
	stage string
	err   error
}

func (err *uncertain115CommitError) Error() string {
	return fmt.Sprintf("%s during %s: %v", uncertain115CommitMarker, err.stage, err.err)
}

func (err *uncertain115CommitError) Unwrap() error {
	return err.err
}

func newECDH115UploadCipher() (upload115Cipher, error) {
	return ec115.NewEcdhCipher()
}

func new115OSSHTTPClient() *http.Client {
	transport := new115HTTPTransport()
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func new115APIHTTPClient() *http.Client {
	return &http.Client{
		Transport: new115HTTPTransport(),
		Timeout:   time.Minute,
	}
}

func new115HTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.ExpectContinueTimeout = time.Second
	return transport
}

type appVersion115Response struct {
	Error string `json:"error,omitempty"`
	Data  struct {
		Win struct {
			Version string `json:"version_code"`
		} `json:"win"`
	} `json:"data"`
}

// resolve115AppVersion caches the selected version for this provider. Version
// discovery is best-effort: uploads remain usable when the public version
// endpoint is temporarily unavailable by using the latest known-safe fallback.
func (p *cookie115Provider) resolve115AppVersion(ctx context.Context) (string, error) {
	p.appVersionMu.Lock()
	defer p.appVersionMu.Unlock()
	if p.appVersionResolved {
		return p.appVersion, p.appVersionResolutionErr
	}

	if err := p.waitRequest(ctx); err != nil {
		return fallback115AppVersion, err
	}
	version, err := fetch115AppVersion(ctx, p.client, p.appVersionEndpoint)
	version = strings.TrimSpace(version)
	var statusErr *http115StatusError
	if err == nil {
		p.observe115HTTPStatus(http.StatusOK, 0)
	} else if errors.As(err, &statusErr) {
		p.observe115HTTPStatus(statusErr.statusCode, statusErr.retryAfter)
	}
	if err != nil || version == "" {
		version = fallback115AppVersion
	}
	p.appVersion = version
	p.appVersionResolutionErr = err
	p.appVersionResolved = true
	if p.configuredUserAgent == "" {
		p.client.SetUserAgent(default115BrowserUserAgent(version))
	}
	return version, err
}

func fetch115AppVersion(ctx context.Context, client *pan115.Pan115Client, endpoint string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = pan115.ApiGetVersion
	}
	response, err := client.NewRequest().
		SetContext(ctx).
		SetDoNotParseResponse(true).
		Get(endpoint)
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("115 app version API returned no response")
	}
	if response.RawBody() == nil {
		return "", errors.New("115 app version API returned no body")
	}
	defer response.RawBody().Close()
	if response.IsError() {
		return "", &http115StatusError{
			statusCode: response.StatusCode(),
			retryAfter: parse115RetryAfter(response.Header().Get("Retry-After"), time.Now()),
		}
	}

	result := appVersion115Response{}
	if err := json.NewDecoder(response.RawBody()).Decode(&result); err != nil {
		return "", fmt.Errorf("decode 115 app version: %w", err)
	}
	if message := strings.TrimSpace(result.Error); message != "" {
		return "", errors.New(message)
	}
	version := strings.TrimSpace(result.Data.Win.Version)
	if version == "" {
		return "", errors.New("115 app version API returned no Windows version")
	}
	return version, nil
}

func default115BrowserUserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = fallback115AppVersion
	}
	return "Mozilla/5.0 115Browser/" + version
}

func (p *cookie115Provider) rapidUploadOrByMultipart(ctx context.Context, dirID string, fileName string, fileSize int64, file *os.File) error {
	appVersion, _ := p.resolve115AppVersion(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	available, err := p.uploadAvailable(ctx)
	if err != nil {
		return fmt.Errorf("get upload information: %w", err)
	}
	if !available || p.client.UploadMetaInfo == nil {
		return errors.New("115 upload is not available")
	}
	if fileSize > p.client.UploadMetaInfo.SizeLimit {
		return pan115.ErrUploadTooLarge
	}

	digest, err := p.client.GetDigestResult(&context115Reader{ctx: ctx, reader: file})
	if err != nil {
		return fmt.Errorf("hash local file: %w", err)
	}
	fastInfo, err := p.rapidUpload(ctx, digest.Size, fileName, dirID, digest.PreID, digest.QuickID, appVersion, file)
	if err != nil {
		if isRetryable115Error(err) && !is115RateLimitError(err) {
			return &uncertain115CommitError{stage: "rapid upload initialization", err: err}
		}
		return fmt.Errorf("initialize rapid upload: %w", err)
	}
	matched, err := fastInfo.Ok()
	if err != nil {
		return &uncertain115CommitError{stage: "interpret rapid upload initialization response", err: err}
	}
	if matched {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if digest.Size <= singleRequest115UploadLimit {
		if err := p.uploadByOSSSingle(ctx, &fastInfo.UploadOSSParams, file); err != nil {
			return fmt.Errorf("upload to OSS with one request: %w", err)
		}
		return nil
	}
	if err := p.uploadByOSSMultipart(ctx, &fastInfo.UploadOSSParams, digest.Size, file); err != nil {
		return fmt.Errorf("upload to OSS with multipart: %w", err)
	}
	return nil
}

func (p *cookie115Provider) uploadAvailable(ctx context.Context) (bool, error) {
	if p.client.UserID != 0 && strings.TrimSpace(p.client.Userkey) != "" && p.client.UploadMetaInfo != nil {
		return true, nil
	}
	if err := p.waitRequest(ctx); err != nil {
		return false, err
	}
	result := pan115.UploadInfoResp{}
	request := p.client.NewRequest().
		SetContext(ctx).
		ForceContentType("application/json;charset=UTF-8").
		SetResult(&result)
	response, err := request.Post(pan115.ApiUploadInfo)
	if err := p.check115APIResponse(err, &result, response); err != nil {
		return false, err
	}
	p.client.Userkey = result.Userkey
	p.client.UserID = result.UserID
	p.client.UploadMetaInfo = &result.UploadMetaInfo
	return p.client.UserID != 0 && strings.TrimSpace(p.client.Userkey) != "", nil
}

func (p *cookie115Provider) uploadByOSSSingle(ctx context.Context, params *pan115.UploadOSSParams, reader io.Reader) error {
	token, err := p.get115OSSToken(ctx)
	if err != nil {
		return fmt.Errorf("get OSS credentials: %w", err)
	}
	bucket, err := p.new115OSSBucket(params, token, false)
	if err != nil {
		return err
	}
	operationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	options := append(pan115.OssOption(params, token), oss.WithContext(operationCtx))
	if err := bucket.PutObject(params.Object, reader, options...); err != nil {
		return &uncertain115CommitError{stage: "single-request OSS upload", err: err}
	}
	return nil
}

func (p *cookie115Provider) uploadByOSSMultipart(ctx context.Context, params *pan115.UploadOSSParams, fileSize int64, file *os.File) error {
	token, err := p.get115OSSToken(ctx)
	if err != nil {
		return fmt.Errorf("get OSS credentials: %w", err)
	}
	bucket, err := p.new115OSSBucket(params, token, true)
	if err != nil {
		return err
	}

	initOptions := []oss.Option{
		oss.SetHeader(pan115.OssSecurityTokenHeaderName, token.SecurityToken),
		oss.UserAgentHeader(pan115.OSSUserAgent),
		oss.EnableSha1(),
		oss.Sequential(),
	}
	var uploadSession oss.InitiateMultipartUploadResult
	for attempt := 1; attempt <= max115OSSOperationAttempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, time.Minute)
		options := append(append([]oss.Option{}, initOptions...), oss.WithContext(operationCtx))
		uploadSession, err = bucket.InitiateMultipartUpload(params.Object, options...)
		cancel()
		if err == nil {
			break
		}
		if !isRetryable115Error(err) || attempt == max115OSSOperationAttempts {
			return fmt.Errorf("initialize OSS multipart upload: %w", err)
		}
		if waitErr := p.waitUploadRetry(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}

	partSize := multipart115PartSizeFor(fileSize)
	partCount := int((fileSize + partSize - 1) / partSize)
	parts := make([]oss.UploadPart, 0, partCount)
	for partNumber := 1; partNumber <= partCount; partNumber++ {
		token, bucket, err = p.refresh115OSSBucket(ctx, params, token, bucket, true)
		if err != nil {
			p.abort115MultipartUpload(bucket, uploadSession, token)
			return fmt.Errorf("refresh OSS credentials before part %d/%d: %w", partNumber, partCount, err)
		}
		offset := int64(partNumber-1) * partSize
		remaining := fileSize - offset
		currentPartSize := partSize
		if remaining < currentPartSize {
			currentPartSize = remaining
		}

		var uploadedPart oss.UploadPart
		for attempt := 1; attempt <= max115OSSOperationAttempts; attempt++ {
			section := io.NewSectionReader(file, offset, currentPartSize)
			operationCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			options := []oss.Option{
				oss.SetHeader(pan115.OssSecurityTokenHeaderName, token.SecurityToken),
				oss.UserAgentHeader(pan115.OSSUserAgent),
				oss.WithContext(operationCtx),
			}
			uploadedPart, err = bucket.UploadPart(uploadSession, section, currentPartSize, partNumber, options...)
			cancel()
			if err == nil {
				break
			}
			if !isRetryable115Error(err) || attempt == max115OSSOperationAttempts {
				p.abort115MultipartUpload(bucket, uploadSession, token)
				return fmt.Errorf("upload OSS part %d/%d: %w", partNumber, partCount, err)
			}
			if waitErr := p.waitUploadRetry(ctx, attempt); waitErr != nil {
				p.abort115MultipartUpload(bucket, uploadSession, token)
				return waitErr
			}
		}
		parts = append(parts, uploadedPart)
	}

	token, bucket, err = p.refresh115OSSBucket(ctx, params, token, bucket, true)
	if err != nil {
		return fmt.Errorf("refresh OSS credentials before completion: %w", err)
	}
	var callbackBody []byte
	completeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	completeOptions := append(
		pan115.OssOption(params, token),
		oss.CallbackResult(&callbackBody),
		oss.WithContext(completeCtx),
	)
	if _, err := bucket.CompleteMultipartUpload(uploadSession, parts, completeOptions...); err != nil {
		return &uncertain115CommitError{stage: "OSS multipart completion", err: err}
	}
	result := pan115.UploadResult{}
	if err := json.Unmarshal(callbackBody, &result); err != nil {
		return &uncertain115CommitError{stage: "OSS multipart callback decoding", err: err}
	}
	if err := result.Err(string(callbackBody)); err != nil {
		return &uncertain115CommitError{stage: "115 OSS callback", err: err}
	}
	return nil
}

func (p *cookie115Provider) new115OSSBucket(params *pan115.UploadOSSParams, token *pan115.UploadOSSTokenResp, integrityChecks bool) (*oss.Bucket, error) {
	ossHTTPClient := p.ossHTTPClient
	if ossHTTPClient == nil {
		ossHTTPClient = new115OSSHTTPClient()
		p.ossHTTPClient = ossHTTPClient
	}
	options := []oss.ClientOption{oss.HTTPClient(ossHTTPClient)}
	if integrityChecks {
		options = append(options, oss.EnableMD5(true), oss.EnableCRC(true))
	}
	client, err := oss.New(
		p.client.GetOSSEndpoint(false),
		token.AccessKeyID,
		token.AccessKeySecret,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("create OSS client: %w", err)
	}
	bucket, err := client.Bucket(params.Bucket)
	if err != nil {
		return nil, fmt.Errorf("open OSS bucket: %w", err)
	}
	return bucket, nil
}

func (p *cookie115Provider) refresh115OSSBucket(ctx context.Context, params *pan115.UploadOSSParams, currentToken *pan115.UploadOSSTokenResp, currentBucket *oss.Bucket, integrityChecks bool) (*pan115.UploadOSSTokenResp, *oss.Bucket, error) {
	latestToken, err := p.get115OSSToken(ctx)
	if err != nil {
		return currentToken, currentBucket, err
	}
	if currentToken != nil &&
		latestToken.AccessKeyID == currentToken.AccessKeyID &&
		latestToken.AccessKeySecret == currentToken.AccessKeySecret &&
		latestToken.SecurityToken == currentToken.SecurityToken {
		return currentToken, currentBucket, nil
	}
	latestBucket, err := p.new115OSSBucket(params, latestToken, integrityChecks)
	if err != nil {
		return currentToken, currentBucket, err
	}
	return latestToken, latestBucket, nil
}

func (p *cookie115Provider) abort115MultipartUpload(bucket *oss.Bucket, uploadSession oss.InitiateMultipartUploadResult, token *pan115.UploadOSSTokenResp) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = bucket.AbortMultipartUpload(
		uploadSession,
		oss.SetHeader(pan115.OssSecurityTokenHeaderName, token.SecurityToken),
		oss.UserAgentHeader(pan115.OSSUserAgent),
		oss.WithContext(ctx),
	)
}

func multipart115PartSizeFor(fileSize int64) int64 {
	partSize := int64(multipart115PartSize)
	minimumForPartLimit := (fileSize + max115MultipartParts - 1) / max115MultipartParts
	if minimumForPartLimit > partSize {
		partSize = minimumForPartLimit
	}
	return partSize
}

func (p *cookie115Provider) get115OSSToken(ctx context.Context) (*pan115.UploadOSSTokenResp, error) {
	p.ossTokenMu.Lock()
	defer p.ossTokenMu.Unlock()
	if p.ossToken != nil && time.Now().Add(oss115TokenRefreshWindow).Before(p.ossTokenExpiresAt) {
		copyOfToken := *p.ossToken
		return &copyOfToken, nil
	}
	if err := p.waitRequest(ctx); err != nil {
		return nil, err
	}
	result := pan115.UploadOSSTokenResp{}
	request := p.client.NewRequest().
		SetContext(ctx).
		ForceContentType("application/json;charset=UTF-8").
		SetResult(&result)
	response, err := request.Get(pan115.ApiUploadOSSToken)
	if err := p.check115APIResponse(err, &result, response); err != nil {
		return nil, err
	}
	expiresAt := result.Expiration
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(50 * time.Minute)
	}
	p.ossToken = &result
	p.ossTokenExpiresAt = expiresAt
	copyOfToken := result
	return &copyOfToken, nil
}

func (p *cookie115Provider) rapidUpload(ctx context.Context, fileSize int64, fileName string, dirID string, preID string, fileID string, appVersion string, reader io.ReadSeeker) (*pan115.UploadInitResp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	newCipher := p.newUploadCipher
	if newCipher == nil {
		newCipher = newECDH115UploadCipher
	}
	ecCipher, err := newCipher()
	if err != nil {
		return nil, err
	}

	appVersion = strings.TrimSpace(appVersion)
	if appVersion == "" {
		appVersion = fallback115AppVersion
	}
	target := "U_1_" + dirID
	fileSizeText := strconv.FormatInt(fileSize, 10)
	userID := strconv.FormatInt(p.client.UserID, 10)
	form := url.Values{}
	form.Set("appid", "0")
	form.Set("appversion", appVersion)
	form.Set("userid", userID)
	form.Set("filename", fileName)
	form.Set("filesize", fileSizeText)
	form.Set("fileid", fileID)
	form.Set("target", target)
	form.Set("sig", p.client.GenerateSignature(fileID, target))
	// The current desktop upload contract no longer sends the legacy
	// "topupload" switch. Omitting it keeps the init request on the current
	// protocol path selected by appversion and token.

	signKey, signValue := "", ""
	for attempt := 0; attempt < max115UploadInitSignAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := p.waitRequest(ctx); err != nil {
			return nil, err
		}
		nowMilli := p.nowMilli
		if nowMilli == nil {
			nowMilli = func() int64 { return pan115.NowMilli().ToInt64() }
		}
		timestamp := nowMilli()
		encodedToken, err := ecCipher.EncodeToken(timestamp)
		if err != nil {
			return nil, err
		}
		timestampText := strconv.FormatInt(timestamp, 10)
		form.Set("t", timestampText)
		form.Set("token", generate115UploadToken(p.client.UserID, fileID, timestampText, fileSizeText, signKey, signValue, appVersion))
		if signKey != "" && signValue != "" {
			form.Set("sign_key", signKey)
			form.Set("sign_val", signValue)
		}

		encrypted, err := ecCipher.Encrypt([]byte(form.Encode()))
		if err != nil {
			return nil, err
		}
		response, err := p.client.NewRequest().
			SetContext(ctx).
			SetQueryParam("k_ec", encodedToken).
			SetBody(encrypted).
			SetHeaderVerbatim("Content-Type", "application/x-www-form-urlencoded").
			SetDoNotParseResponse(true).
			Post(pan115.ApiUploadInit)
		if statusErr := p.check115HTTPStatus(err, response); statusErr != nil {
			if response != nil && response.RawBody() != nil {
				_ = response.RawBody().Close()
			}
			return nil, statusErr
		}
		if response == nil || response.RawBody() == nil {
			return nil, &uncertain115CommitError{
				stage: "rapid upload initialization response",
				err:   errors.New("115 upload init returned no response"),
			}
		}
		body, readErr := io.ReadAll(response.RawBody())
		closeErr := response.RawBody().Close()
		if readErr != nil {
			return nil, &uncertain115CommitError{stage: "read rapid upload initialization response", err: readErr}
		}
		if closeErr != nil {
			return nil, &uncertain115CommitError{stage: "close rapid upload initialization response", err: closeErr}
		}
		decrypted, err := ecCipher.Decrypt(body)
		if err != nil {
			return nil, &uncertain115CommitError{stage: "decrypt rapid upload initialization response", err: err}
		}
		result := pan115.UploadInitResp{}
		if err := json.Unmarshal(decrypted, &result); err != nil {
			return nil, &uncertain115CommitError{stage: "decode rapid upload initialization response", err: err}
		}
		if err := p.check115APIResponse(nil, &result, response); err != nil {
			return nil, err
		}
		result.SHA1 = fileID
		if result.Status != 7 {
			return &result, nil
		}
		if attempt == max115UploadInitSignAttempts-1 {
			return nil, errors.New("115 upload init repeated the sign challenge")
		}

		signKey = result.SignKey
		signValue, err = p.client.UploadDigestRange(reader, result.SignCheck)
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("115 upload init exhausted its attempts")
}

func generate115UploadToken(userID int64, fileID string, timestamp string, fileSize string, signKey string, signValue string, appVersion string) string {
	userIDText := strconv.FormatInt(userID, 10)
	userIDMD5 := md5.Sum([]byte(userIDText))
	tokenMD5 := md5.Sum([]byte(upload115TokenSalt + fileID + fileSize + signKey + signValue + userIDText + timestamp + hex.EncodeToString(userIDMD5[:]) + appVersion))
	return hex.EncodeToString(tokenMD5[:])
}

type context115Reader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *context115Reader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
