package fileaudit

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type FileSystem interface {
	Walk(ctx context.Context, root string, fn func(FileInfo) error) error
	Open(ctx context.Context, name string) (io.ReadCloser, error)
}

type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type Options struct {
	LocalRoot       string
	RemoteRoot      string
	LocalFS         FileSystem
	RemoteFS        FileSystem
	VideoExtensions []string
	AllowSTRMProxy  bool
	CompareSize     bool
	CompareMD5      bool
}

type Report struct {
	LocalRoot   string  `json:"localRoot"`
	RemoteRoot  string  `json:"remoteRoot"`
	LocalCount  int     `json:"localCount"`
	RemoteCount int     `json:"remoteCount"`
	Issues      []Issue `json:"issues,omitempty"`
}

type Issue struct {
	Severity string `json:"severity"`
	Type     string `json:"type"`
	Path     string `json:"path"`
	Local    string `json:"local,omitempty"`
	Remote   string `json:"remote,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func Compare(ctx context.Context, opts Options) (Report, error) {
	if strings.TrimSpace(opts.LocalRoot) == "" {
		return Report{}, errors.New("local root is required")
	}
	if strings.TrimSpace(opts.RemoteRoot) == "" {
		return Report{}, errors.New("remote root is required")
	}
	if opts.LocalFS == nil {
		opts.LocalFS = LocalFS{}
	}
	if opts.RemoteFS == nil {
		return Report{}, errors.New("remote filesystem is required")
	}
	if len(opts.VideoExtensions) == 0 {
		opts.VideoExtensions = DefaultVideoExtensions()
	}

	localRoot := filepath.Clean(opts.LocalRoot)
	remoteRoot := cleanRemoteRoot(opts.RemoteRoot)
	localFiles, ignoredDirs, err := collectLocalFiles(ctx, opts.LocalFS, localRoot)
	if err != nil {
		return Report{}, err
	}
	remoteFiles, err := collectFiles(ctx, opts.RemoteFS, remoteRoot, false)
	if err != nil {
		return Report{}, err
	}

	report := Report{LocalRoot: localRoot, RemoteRoot: remoteRoot, LocalCount: len(localFiles), RemoteCount: len(remoteFiles)}
	remoteMatched := make(map[string]bool, len(remoteFiles))
	videoExts := extensionSet(opts.VideoExtensions)

	localPaths := sortedKeys(localFiles)
	for _, rel := range localPaths {
		local := localFiles[rel]
		remote, remoteRel, ok := findRemoteMatch(rel, remoteFiles, videoExts, opts.AllowSTRMProxy)
		if !ok {
			report.Issues = append(report.Issues, Issue{Severity: "error", Type: "missing_remote", Path: rel, Local: local.Path, Detail: "远端缺少该文件"})
			continue
		}
		remoteMatched[remoteRel] = true
		if opts.CompareSize && !isSTRMProxyMatch(rel, remoteRel, videoExts) && local.Size != remote.Size {
			report.Issues = append(report.Issues, Issue{Severity: "warning", Type: "size_mismatch", Path: rel, Local: fmt.Sprintf("%d", local.Size), Remote: fmt.Sprintf("%d", remote.Size), Detail: "文件大小不一致"})
		}
		if opts.CompareMD5 && !isSTRMProxyMatch(rel, remoteRel, videoExts) {
			localMD5, remoteMD5, err := compareMD5(ctx, opts.LocalFS, opts.RemoteFS, local.Path, remote.Path)
			if err != nil {
				report.Issues = append(report.Issues, Issue{Severity: "warning", Type: "md5_error", Path: rel, Detail: err.Error()})
			} else if localMD5 != remoteMD5 {
				report.Issues = append(report.Issues, Issue{Severity: "warning", Type: "md5_mismatch", Path: rel, Local: localMD5, Remote: remoteMD5, Detail: "MD5 不一致"})
			}
		}
	}

	ignoredRemoteDirs := map[string]bool{}
	for _, rel := range sortedKeys(remoteFiles) {
		if !remoteMatched[rel] {
			if ignoredDir := matchingIgnoredDir(rel, ignoredDirs); ignoredDir != "" {
				ignoredRemoteDirs[ignoredDir] = true
				continue
			}
			report.Issues = append(report.Issues, Issue{Severity: "warning", Type: "extra_remote", Path: rel, Remote: remoteFiles[rel].Path, Detail: "远端存在但本地缺少"})
		}
	}
	for _, rel := range sortedKeys(ignoredRemoteDirs) {
		report.Issues = append(report.Issues, Issue{Severity: "warning", Type: "extra_remote_dir", Path: rel, Remote: JoinRemote(remoteRoot, rel), Detail: "本地 .ignore 目录在远端存在"})
	}
	sortIssues(report.Issues)
	return report, nil
}

func DefaultVideoExtensions() []string {
	return []string{".mkv", ".mp4", ".ts", ".m2ts", ".mts", ".mov", ".m4v", ".avi", ".wmv", ".flv", ".webm", ".rmvb", ".rm", ".mpg", ".mpeg", ".vob", ".asf"}
}

func collectFiles(ctx context.Context, fs FileSystem, root string, local bool) (map[string]FileInfo, error) {
	files := map[string]FileInfo{}
	err := fs.Walk(ctx, root, func(info FileInfo) error {
		rel, err := relativePath(root, info.Path, local)
		if err != nil {
			return err
		}
		files[rel] = info
		return nil
	})
	return files, err
}

func collectLocalFiles(ctx context.Context, fs FileSystem, root string) (map[string]FileInfo, []string, error) {
	if _, ok := fs.(LocalFS); !ok {
		files, err := collectFiles(ctx, fs, root, true)
		return files, nil, err
	}
	files := map[string]FileInfo{}
	var ignoredDirs []string
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			if fileExists(filepath.Join(name, ".ignore")) {
				rel, err := relativePath(root, name, true)
				if err != nil {
					return err
				}
				if rel != "." {
					ignoredDirs = append(ignoredDirs, rel)
				}
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := relativePath(root, name, true)
		if err != nil {
			return err
		}
		files[rel] = FileInfo{Path: name, Size: info.Size()}
		return nil
	})
	sort.Strings(ignoredDirs)
	return files, ignoredDirs, err
}

func relativePath(root string, name string, local bool) (string, error) {
	if local {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(rel), nil
	}
	rel := strings.TrimPrefix(cleanRemoteRoot(name), strings.TrimRight(cleanRemoteRoot(root), "/")+"/")
	if rel == cleanRemoteRoot(name) && cleanRemoteRoot(root) != "." {
		return "", fmt.Errorf("remote path %q is outside root %q", name, root)
	}
	return rel, nil
}

func findRemoteMatch(rel string, remote map[string]FileInfo, videoExts map[string]bool, allowSTRM bool) (FileInfo, string, bool) {
	if file, ok := remote[rel]; ok {
		return file, rel, true
	}
	if !allowSTRM || !videoExts[strings.ToLower(path.Ext(rel))] {
		return FileInfo{}, "", false
	}
	strmRel := strings.TrimSuffix(rel, path.Ext(rel)) + ".strm"
	file, ok := remote[strmRel]
	return file, strmRel, ok
}

func isSTRMProxyMatch(localRel string, remoteRel string, videoExts map[string]bool) bool {
	return videoExts[strings.ToLower(path.Ext(localRel))] && strings.EqualFold(path.Ext(remoteRel), ".strm")
}

func compareMD5(ctx context.Context, localFS FileSystem, remoteFS FileSystem, localPath string, remotePath string) (string, string, error) {
	localHash, err := fileMD5(ctx, localFS, localPath)
	if err != nil {
		return "", "", fmt.Errorf("local md5 failed: %w", err)
	}
	remoteHash, err := fileMD5(ctx, remoteFS, remotePath)
	if err != nil {
		return "", "", fmt.Errorf("remote md5 failed: %w", err)
	}
	return localHash, remoteHash, nil
}

func fileMD5(ctx context.Context, fs FileSystem, name string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	reader, err := fs.Open(ctx, name)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extensionSet(exts []string) map[string]bool {
	set := make(map[string]bool, len(exts))
	for _, ext := range exts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		set[ext] = true
	}
	return set
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortIssues(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Type < issues[j].Type
		}
		return issues[i].Path < issues[j].Path
	})
}

func cleanRemoteRoot(value string) string {
	value = path.Clean(filepath.ToSlash(strings.TrimSpace(value)))
	if value == "." {
		return "."
	}
	return strings.TrimRight(value, "/")
}

func matchingIgnoredDir(rel string, ignoredDirs []string) string {
	for _, dir := range ignoredDirs {
		if rel == dir || strings.HasPrefix(rel, strings.TrimRight(dir, "/")+"/") {
			return dir
		}
	}
	return ""
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}
