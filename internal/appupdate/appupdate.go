package appupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRepository = "stevennight/NyaMediaMetadataTool"
	maxMetadataBytes  = 2 << 20
	maxChecksumBytes  = 1 << 20
	maxInstallerBytes = 512 << 20
)

var stableVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type SupportReason string

const (
	ReasonDevelopmentBuild SupportReason = "developmentBuild"
	ReasonNotInstalled     SupportReason = "notInstalled"
	ReasonUnsupported      SupportReason = "unsupportedPlatform"
)

type CheckResult struct {
	Status         string        `json:"status"`
	CurrentVersion string        `json:"currentVersion"`
	Reason         SupportReason `json:"reason,omitempty"`
	Version        string        `json:"version,omitempty"`
	ReleaseName    string        `json:"releaseName,omitempty"`
	ReleaseNotes   string        `json:"releaseNotes,omitempty"`
	PublishedAt    string        `json:"publishedAt,omitempty"`
}

type Client struct {
	Repository string
	HTTPClient *http.Client
	APIBaseURL string
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type release struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	PublishedAt string         `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}

type semanticVersion struct {
	major int
	minor int
	patch int
}

func DefaultRepository() string {
	return defaultRepository
}

func (c Client) Check(ctx context.Context, currentVersion string, installed bool) (CheckResult, error) {
	if reason := supportReason(currentVersion, installed); reason != "" {
		return CheckResult{
			Status:         "unsupported",
			CurrentVersion: currentVersion,
			Reason:         reason,
		}, nil
	}

	current, err := parseVersion(currentVersion)
	if err != nil {
		return CheckResult{}, err
	}
	latestRelease, err := c.latestRelease(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	latest, version, err := releaseVersion(latestRelease)
	if err != nil {
		return CheckResult{}, err
	}
	if !latest.greaterThan(current) {
		return CheckResult{Status: "upToDate", CurrentVersion: currentVersion}, nil
	}
	return CheckResult{
		Status:         "available",
		CurrentVersion: currentVersion,
		Version:        version,
		ReleaseName:    firstNonEmpty(strings.TrimSpace(latestRelease.Name), latestRelease.TagName),
		ReleaseNotes:   truncate(strings.TrimSpace(latestRelease.Body), 4000),
		PublishedAt:    latestRelease.PublishedAt,
	}, nil
}

func (c Client) Download(ctx context.Context, currentVersion, requestedVersion, downloadDir string, installed bool) (string, error) {
	if reason := supportReason(currentVersion, installed); reason != "" {
		return "", fmt.Errorf("automatic updates are unavailable: %s", reason)
	}
	current, err := parseVersion(currentVersion)
	if err != nil {
		return "", err
	}
	requested, err := parseVersion(requestedVersion)
	if err != nil {
		return "", fmt.Errorf("invalid requested update version: %w", err)
	}
	if !requested.greaterThan(current) {
		return "", errors.New("the requested update is not newer than the installed version")
	}

	latestRelease, err := c.latestRelease(ctx)
	if err != nil {
		return "", err
	}
	latest, version, err := releaseVersion(latestRelease)
	if err != nil {
		return "", err
	}
	if latest != requested || version != requestedVersion {
		return "", errors.New("the selected update is no longer the latest release; check again")
	}

	installer, checksums, err := selectAssets(latestRelease.Assets, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	checksumData, err := c.downloadBytes(ctx, checksums.BrowserDownloadURL, maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("download release checksums: %w", err)
	}
	expected, ok := checksumForFile(string(checksumData), installer.Name)
	if !ok {
		return "", fmt.Errorf("release checksums do not contain %s", installer.Name)
	}
	if err := os.MkdirAll(downloadDir, 0o700); err != nil {
		return "", fmt.Errorf("prepare update directory: %w", err)
	}
	target := filepath.Join(downloadDir, installer.Name)
	temp, err := os.CreateTemp(downloadDir, ".update-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create update file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	hash := sha256.New()
	if err := c.downloadTo(ctx, installer.BrowserDownloadURL, installer.Size, io.MultiWriter(temp, hash)); err != nil {
		temp.Close()
		return "", fmt.Errorf("download update installer: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close update installer: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return "", errors.New("downloaded update installer failed SHA-256 verification")
	}
	if err := os.Chmod(tempPath, 0o700); err != nil {
		return "", fmt.Errorf("set update installer permissions: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("replace previous update installer: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return "", fmt.Errorf("store update installer: %w", err)
	}
	return target, nil
}

func supportReason(currentVersion string, installed bool) SupportReason {
	if _, err := parseVersion(currentVersion); err != nil {
		return ReasonDevelopmentBuild
	}
	if runtime.GOOS != "windows" {
		return ReasonUnsupported
	}
	if !installed {
		return ReasonNotInstalled
	}
	return ""
}

func (c Client) latestRelease(ctx context.Context) (release, error) {
	repository := strings.TrimSpace(c.Repository)
	if repository == "" {
		repository = defaultRepository
	}
	if !validRepository(repository) {
		return release{}, errors.New("invalid update repository")
	}
	base := strings.TrimRight(c.APIBaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "NyaMediaMetadataTool-Updater")

	response, err := c.httpClient().Do(request)
	if err != nil {
		return release{}, fmt.Errorf("request latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return release{}, fmt.Errorf("request latest release: GitHub returned %s", response.Status)
	}
	var result release
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err := decoder.Decode(&result); err != nil {
		return release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if result.Draft || result.Prerelease {
		return release{}, errors.New("latest GitHub release is not a stable published release")
	}
	return result, nil
}

func (c Client) downloadBytes(ctx context.Context, url string, maximum int64) ([]byte, error) {
	var builder strings.Builder
	if err := c.downloadTo(ctx, url, maximum, &builder); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

func (c Client) downloadTo(ctx context.Context, url string, declaredSize int64, writer io.Writer) error {
	if declaredSize <= 0 || declaredSize > maxInstallerBytes {
		return errors.New("release asset has an invalid size")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "NyaMediaMetadataTool-Updater")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", response.Status)
	}
	if response.ContentLength > declaredSize || response.ContentLength > maxInstallerBytes {
		return errors.New("release asset exceeds the allowed size")
	}
	limited := &io.LimitedReader{R: response.Body, N: declaredSize + 1}
	written, err := io.Copy(writer, limited)
	if err != nil {
		return err
	}
	if written > declaredSize || limited.N == 0 {
		return errors.New("release asset exceeds its declared size")
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Minute}
}

func releaseVersion(value release) (semanticVersion, string, error) {
	version := strings.TrimPrefix(strings.TrimSpace(value.TagName), "v")
	parsed, err := parseVersion(version)
	if err != nil || value.TagName != "v"+version {
		return semanticVersion{}, "", fmt.Errorf("latest release tag %q is not vMAJOR.MINOR.PATCH", value.TagName)
	}
	return parsed, version, nil
}

func parseVersion(value string) (semanticVersion, error) {
	match := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return semanticVersion{}, fmt.Errorf("%q is not a stable semantic version", value)
	}
	parts := make([]int, 3)
	for index := range parts {
		parsed, err := strconv.Atoi(match[index+1])
		if err != nil {
			return semanticVersion{}, err
		}
		parts[index] = parsed
	}
	return semanticVersion{major: parts[0], minor: parts[1], patch: parts[2]}, nil
}

func (v semanticVersion) greaterThan(other semanticVersion) bool {
	if v.major != other.major {
		return v.major > other.major
	}
	if v.minor != other.minor {
		return v.minor > other.minor
	}
	return v.patch > other.patch
}

func selectAssets(assets []releaseAsset, arch string) (releaseAsset, releaseAsset, error) {
	architecture := map[string]string{"amd64": "amd64", "386": "386", "arm64": "arm64"}[arch]
	if architecture == "" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("unsupported Windows architecture %q", arch)
	}
	suffix := "-windows-" + architecture + "-installer.exe"
	var installer releaseAsset
	var checksums releaseAsset
	for _, asset := range assets {
		lower := strings.ToLower(asset.Name)
		switch {
		case asset.Name == "SHA256SUMS":
			checksums = asset
		case strings.HasPrefix(lower, "nyamediametadatatool-") && strings.HasSuffix(lower, suffix):
			installer = asset
		}
	}
	if installer.Name == "" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("release does not contain a Windows %s installer", architecture)
	}
	if filepath.Base(installer.Name) != installer.Name {
		return releaseAsset{}, releaseAsset{}, errors.New("release installer has an unsafe file name")
	}
	if checksums.Name == "" {
		return releaseAsset{}, releaseAsset{}, errors.New("release does not contain SHA256SUMS")
	}
	return installer, checksums, nil
}

func checksumForFile(checksums, fileName string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != fileName || len(fields[0]) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(fields[0]); err == nil {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func validRepository(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if !(character >= 'a' && character <= 'z') &&
				!(character >= 'A' && character <= 'Z') &&
				!(character >= '0' && character <= '9') &&
				character != '-' && character != '_' && character != '.' {
				return false
			}
		}
	}
	return true
}

func truncate(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
