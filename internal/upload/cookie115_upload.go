package upload

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	ec115 "github.com/SheltonZhu/115driver/pkg/crypto/ec115"
	pan115 "github.com/SheltonZhu/115driver/pkg/driver"
)

const (
	upload115TokenSalt           = "Qclm8MGWUv59TnrR0XPg"
	max115UploadInitSignAttempts = 3
)

type upload115Cipher interface {
	EncodeToken(timestamp int64) (string, error)
	Encrypt(plainText []byte) ([]byte, error)
	Decrypt(cipherText []byte) ([]byte, error)
}

func newECDH115UploadCipher() (upload115Cipher, error) {
	return ec115.NewEcdhCipher()
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

	version, err := fetch115AppVersion(ctx, p.client, p.appVersionEndpoint)
	version = strings.TrimSpace(version)
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
		return "", fmt.Errorf("115 app version API returned HTTP %d", response.StatusCode())
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

	available, err := p.client.UploadAvailable()
	if err != nil {
		return err
	}
	if !available || p.client.UploadMetaInfo == nil {
		return errors.New("115 upload is not available")
	}
	if fileSize > p.client.UploadMetaInfo.SizeLimit {
		return pan115.ErrUploadTooLarge
	}

	digest, err := p.client.GetDigestResult(&context115Reader{ctx: ctx, reader: file})
	if err != nil {
		return err
	}
	fastInfo, err := p.rapidUpload(ctx, digest.Size, fileName, dirID, digest.PreID, digest.QuickID, appVersion, file)
	if err != nil {
		return err
	}
	matched, err := fastInfo.Ok()
	if err != nil {
		return err
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
	if digest.Size <= pan115.KB {
		return p.client.UploadByOSS(&fastInfo.UploadOSSParams, file, dirID)
	}
	return p.client.UploadByMultipart(&fastInfo.UploadOSSParams, digest.Size, file, dirID)
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
		if err != nil {
			return nil, err
		}
		if response == nil || response.RawBody() == nil {
			return nil, errors.New("115 upload init returned no response")
		}
		body, readErr := io.ReadAll(response.RawBody())
		closeErr := response.RawBody().Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		decrypted, err := ecCipher.Decrypt(body)
		if err != nil {
			return nil, err
		}
		result := pan115.UploadInitResp{}
		if err := pan115.CheckErr(json.Unmarshal(decrypted, &result), &result, response); err != nil {
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
