package upload

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	sdk115 "github.com/xhofe/115-sdk-go"
)

type open115Digest struct {
	Size  int64
	SHA1  string
	PreID string
}

type open115CallbackResponse struct {
	State   bool   `json:"state"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func calculateOpen115Digest(ctx context.Context, file *os.File) (*open115Digest, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	preHasher := sha1.New()
	if _, err := io.Copy(preHasher, io.LimitReader(&context115Reader{ctx: ctx, reader: file}, 128*1024)); err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	fullHasher := sha1.New()
	size, err := io.Copy(fullHasher, &context115Reader{ctx: ctx, reader: file})
	if err != nil {
		return nil, err
	}
	return &open115Digest{
		Size:  size,
		SHA1:  strings.ToUpper(hex.EncodeToString(fullHasher.Sum(nil))),
		PreID: strings.ToUpper(hex.EncodeToString(preHasher.Sum(nil))),
	}, nil
}

func (p *open115Provider) uploadOpen115Content(ctx context.Context, parentID, name string, size int64, file *os.File, digest *open115Digest) error {
	if digest == nil {
		var err error
		digest, err = calculateOpen115Digest(ctx, file)
		if err != nil {
			return fmt.Errorf("hash local file: %w", err)
		}
	}
	initResponse, err := p.initializeOpen115Upload(ctx, parentID, name, size, file, digest)
	if err != nil {
		if isRetryable115Error(err) {
			return &uncertain115CommitError{stage: "115 Open upload initialization", err: err}
		}
		return fmt.Errorf("initialize 115 Open upload: %w", err)
	}
	if initResponse.Status == 2 {
		return nil
	}
	if strings.TrimSpace(initResponse.Bucket) == "" || strings.TrimSpace(initResponse.Object) == "" {
		return fmt.Errorf("115 Open upload init returned no OSS destination (status=%d)", initResponse.Status)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if size <= singleRequest115UploadLimit {
		return p.uploadOpen115Single(ctx, initResponse, size, file)
	}
	return p.uploadOpen115Multipart(ctx, initResponse, size, file)
}

func (p *open115Provider) initializeOpen115Upload(ctx context.Context, parentID, name string, size int64, file *os.File, digest *open115Digest) (*sdk115.UploadInitResp, error) {
	if err := p.waitRequest(ctx); err != nil {
		return nil, err
	}
	request := &sdk115.UploadInitReq{
		FileName: name,
		FileSize: size,
		Target:   parentID,
		FileID:   digest.SHA1,
		PreID:    digest.PreID,
	}
	response, err := p.client.UploadInit(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("115 Open upload init returned no response")
	}
	if response.Status == 2 {
		return response, nil
	}
	if response.Status != 6 && response.Status != 7 && response.Status != 8 {
		return response, nil
	}

	parts := strings.Split(strings.TrimSpace(response.SignCheck), "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("115 Open upload returned invalid sign_check %q", response.SignCheck)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse 115 Open sign_check start: %w", err)
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || start < 0 || end < start || end >= size {
		return nil, fmt.Errorf("parse 115 Open sign_check range %q", response.SignCheck)
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	signHasher := sha1.New()
	if _, err := io.Copy(signHasher, io.LimitReader(&context115Reader{ctx: ctx, reader: file}, end-start+1)); err != nil {
		return nil, err
	}
	request.SignKey = response.SignKey
	request.SignVal = strings.ToUpper(hex.EncodeToString(signHasher.Sum(nil)))
	if err := p.waitRequest(ctx); err != nil {
		return nil, err
	}
	response, err = p.client.UploadInit(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("115 Open verified upload init returned no response")
	}
	if response.Status == 6 || response.Status == 7 || response.Status == 8 {
		return nil, errors.New("115 Open upload init repeated the sign challenge")
	}
	return response, nil
}

func (p *open115Provider) uploadOpen115Single(ctx context.Context, params *sdk115.UploadInitResp, size int64, reader io.Reader) error {
	token, err := p.getOpen115OSSToken(ctx)
	if err != nil {
		return fmt.Errorf("get 115 Open OSS credentials: %w", err)
	}
	bucket, err := p.newOpen115OSSBucket(params.Bucket, token, false)
	if err != nil {
		return err
	}
	operationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	options := []oss.Option{oss.WithContext(operationCtx)}
	options = append(options, open115CallbackOptions(params)...)
	if listener := new115UploadProgressListener(0, size, size, p.progressReporter); listener != nil {
		options = append(options, oss.Progress(listener))
	}
	var callbackBody []byte
	options = append(options, oss.CallbackResult(&callbackBody))
	if err := bucket.PutObject(params.Object, reader, options...); err != nil {
		if is115PreCommitConnectionError(err) {
			return format115PreCommitConnectionError(err)
		}
		return &uncertain115CommitError{stage: "115 Open single-request OSS upload", err: err}
	}
	if err := validateOpen115Callback(callbackBody); err != nil {
		return &uncertain115CommitError{stage: "115 Open OSS callback", err: err}
	}
	return nil
}

func (p *open115Provider) uploadOpen115Multipart(ctx context.Context, params *sdk115.UploadInitResp, size int64, file *os.File) error {
	token, err := p.getOpen115OSSToken(ctx)
	if err != nil {
		return fmt.Errorf("get 115 Open OSS credentials: %w", err)
	}
	bucket, err := p.newOpen115OSSBucket(params.Bucket, token, true)
	if err != nil {
		return err
	}
	operationCtx, cancel := context.WithTimeout(ctx, time.Minute)
	session, err := bucket.InitiateMultipartUpload(params.Object, oss.Sequential(), oss.WithContext(operationCtx))
	cancel()
	if err != nil {
		return fmt.Errorf("initialize 115 Open OSS multipart upload: %w", err)
	}

	partSize := multipart115PartSizeFor(size)
	partCount := int((size + partSize - 1) / partSize)
	parts := make([]oss.UploadPart, 0, partCount)
	for partNumber := 1; partNumber <= partCount; partNumber++ {
		latestToken, latestBucket, refreshErr := p.refreshOpen115OSSBucket(ctx, params.Bucket, token, bucket, true)
		if refreshErr != nil {
			p.abortOpen115Multipart(ctx, bucket, session)
			return fmt.Errorf("refresh 115 Open OSS credentials before part %d/%d: %w", partNumber, partCount, refreshErr)
		}
		token, bucket = latestToken, latestBucket
		offset := int64(partNumber-1) * partSize
		currentSize := partSize
		if remaining := size - offset; remaining < currentSize {
			currentSize = remaining
		}
		var uploaded oss.UploadPart
		for attempt := 1; attempt <= max115OSSOperationAttempts; attempt++ {
			section := io.NewSectionReader(file, offset, currentSize)
			partCtx, partCancel := context.WithTimeout(ctx, 10*time.Minute)
			options := []oss.Option{oss.WithContext(partCtx)}
			if listener := new115UploadProgressListener(offset, currentSize, size, p.progressReporter); listener != nil {
				options = append(options, oss.Progress(listener))
			}
			uploaded, err = bucket.UploadPart(session, section, currentSize, partNumber, options...)
			partCancel()
			if err == nil {
				if p.progressReporter != nil {
					p.progressReporter(offset + currentSize)
				}
				break
			}
			if !isRetryable115Error(err) || attempt == max115OSSOperationAttempts {
				p.abortOpen115Multipart(ctx, bucket, session)
				return fmt.Errorf("upload 115 Open OSS part %d/%d: %w", partNumber, partCount, err)
			}
			if waitErr := p.waitUploadRetry(ctx, attempt); waitErr != nil {
				p.abortOpen115Multipart(ctx, bucket, session)
				return waitErr
			}
		}
		parts = append(parts, uploaded)
	}

	_, bucket, err = p.refreshOpen115OSSBucket(ctx, params.Bucket, token, bucket, true)
	if err != nil {
		return fmt.Errorf("refresh 115 Open OSS credentials before completion: %w", err)
	}
	completeCtx, completeCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer completeCancel()
	var callbackBody []byte
	options := []oss.Option{oss.WithContext(completeCtx), oss.CallbackResult(&callbackBody)}
	options = append(options, open115CallbackOptions(params)...)
	if _, err := bucket.CompleteMultipartUpload(session, parts, options...); err != nil {
		return &uncertain115CommitError{stage: "115 Open OSS multipart completion", err: err}
	}
	if err := validateOpen115Callback(callbackBody); err != nil {
		return &uncertain115CommitError{stage: "115 Open OSS multipart callback", err: err}
	}
	return nil
}

func open115CallbackOptions(params *sdk115.UploadInitResp) []oss.Option {
	if params == nil {
		return nil
	}
	callback := strings.TrimSpace(params.Callback.Value.Callback)
	callbackVar := strings.TrimSpace(params.Callback.Value.CallbackVar)
	options := make([]oss.Option, 0, 2)
	if callback != "" {
		options = append(options, oss.Callback(base64.StdEncoding.EncodeToString([]byte(callback))))
	}
	if callbackVar != "" {
		options = append(options, oss.CallbackVar(base64.StdEncoding.EncodeToString([]byte(callbackVar))))
	}
	return options
}

func validateOpen115Callback(body []byte) error {
	if len(body) == 0 {
		return nil
	}
	var response open115CallbackResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode callback response: %w", err)
	}
	if !response.State || response.Code != 0 {
		return fmt.Errorf("callback failed code=%d message=%s", response.Code, response.Message)
	}
	return nil
}

func (p *open115Provider) getOpen115OSSToken(ctx context.Context) (*sdk115.UploadGetTokenResp, error) {
	p.ossTokenMu.Lock()
	defer p.ossTokenMu.Unlock()
	if p.ossToken != nil && time.Now().Add(oss115TokenRefreshWindow).Before(p.ossTokenExpiresAt) {
		copyOfToken := *p.ossToken
		return &copyOfToken, nil
	}
	if err := p.waitRequest(ctx); err != nil {
		return nil, err
	}
	token, err := p.client.UploadGetToken(ctx)
	if err != nil {
		return nil, err
	}
	if token == nil || strings.TrimSpace(token.Endpoint) == "" || strings.TrimSpace(token.AccessKeyId) == "" || strings.TrimSpace(token.AccessKeySecret) == "" {
		return nil, errors.New("115 Open OSS credentials are incomplete")
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(token.Expiration))
	if err != nil {
		expiresAt = time.Now().Add(50 * time.Minute)
	}
	p.ossToken = token
	p.ossTokenExpiresAt = expiresAt
	copyOfToken := *token
	return &copyOfToken, nil
}

func (p *open115Provider) newOpen115OSSBucket(bucketName string, token *sdk115.UploadGetTokenResp, integrityChecks bool) (*oss.Bucket, error) {
	options := []oss.ClientOption{oss.SecurityToken(token.SecurityToken)}
	if p.ossHTTPClient != nil {
		options = append(options, oss.HTTPClient(p.ossHTTPClient))
	}
	if integrityChecks {
		options = append(options, oss.EnableMD5(true), oss.EnableCRC(true))
	}
	client, err := oss.New(token.Endpoint, token.AccessKeyId, token.AccessKeySecret, options...)
	if err != nil {
		return nil, fmt.Errorf("create 115 Open OSS client: %w", err)
	}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("open 115 Open OSS bucket: %w", err)
	}
	return bucket, nil
}

func (p *open115Provider) refreshOpen115OSSBucket(ctx context.Context, bucketName string, currentToken *sdk115.UploadGetTokenResp, currentBucket *oss.Bucket, integrityChecks bool) (*sdk115.UploadGetTokenResp, *oss.Bucket, error) {
	latest, err := p.getOpen115OSSToken(ctx)
	if err != nil {
		return currentToken, currentBucket, err
	}
	if currentToken != nil && latest.AccessKeyId == currentToken.AccessKeyId && latest.AccessKeySecret == currentToken.AccessKeySecret && latest.SecurityToken == currentToken.SecurityToken {
		return currentToken, currentBucket, nil
	}
	bucket, err := p.newOpen115OSSBucket(bucketName, latest, integrityChecks)
	if err != nil {
		return currentToken, currentBucket, err
	}
	return latest, bucket, nil
}

func (p *open115Provider) abortOpen115Multipart(ctx context.Context, bucket *oss.Bucket, session oss.InitiateMultipartUploadResult) {
	if ctx.Err() != nil || bucket == nil {
		return
	}
	abortCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_ = bucket.AbortMultipartUpload(session, oss.WithContext(abortCtx))
}
