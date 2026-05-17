package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

type ffprobeStreams struct {
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeStream struct {
	Index              int                `json:"index"`
	CodecName          string             `json:"codec_name"`
	CodecType          string             `json:"codec_type"`
	Width              int                `json:"width"`
	Height             int                `json:"height"`
	Channels           int                `json:"channels"`
	SampleRate         int                `json:"sample_rate,string"`
	BitRate            string             `json:"bit_rate"`
	AvgFrameRate       string             `json:"avg_frame_rate"`
	RFrameRate         string             `json:"r_frame_rate"`
	DisplayAspectRatio string             `json:"display_aspect_ratio"`
	SampleAspectRatio  string             `json:"sample_aspect_ratio"`
	Tags               map[string]string  `json:"tags"`
	Disposition        ffprobeDisposition `json:"disposition"`
}

type ffprobeDisposition struct {
	Default int `json:"default"`
	Forced  int `json:"forced"`
}

type subtitleArtifact struct {
	Path string
	Skip bool
	Note string
}

func GenerateSubtitles(ctx context.Context, cfg config.Config, media store.MediaFile) ([]string, error) {
	if !cfg.Processing.EnableSubtitles {
		return nil, nil
	}

	streams, err := listSubtitleStreams(ctx, cfg, media.Path)
	if err != nil {
		return nil, err
	}

	artifacts := make([]string, 0)
	for _, stream := range streams {
		artifact, err := exportSubtitle(ctx, cfg, media.Path, stream)
		if err != nil {
			return artifacts, err
		}
		if artifact.Skip || artifact.Path == "" {
			continue
		}
		artifacts = append(artifacts, artifact.Path)
	}
	return artifacts, nil
}

func listSubtitleStreams(ctx context.Context, cfg config.Config, mediaPath string) ([]ffprobeStream, error) {
	if strings.TrimSpace(cfg.Tools.FFprobe) == "" {
		return nil, errors.New("ffprobe is not configured")
	}

	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.Tools.FFprobe, "-v", "error", "-print_format", "json", "-show_streams", mediaPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list subtitle streams: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var parsed ffprobeStreams
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe streams JSON: %w", err)
	}

	streams := make([]ffprobeStream, 0)
	for _, stream := range parsed.Streams {
		if stream.CodecType == "subtitle" {
			streams = append(streams, stream)
		}
	}
	return streams, nil
}

func exportSubtitle(ctx context.Context, cfg config.Config, mediaPath string, stream ffprobeStream) (subtitleArtifact, error) {
	strategy, ok := subtitleStrategy(stream.CodecName)
	if !ok {
		return subtitleArtifact{Skip: true, Note: "unsupported subtitle codec: " + stream.CodecName}, nil
	}
	if strings.TrimSpace(cfg.Tools.FFmpeg) == "" {
		return subtitleArtifact{}, errors.New("ffmpeg is not configured")
	}

	outputPath := subtitleOutputPath(mediaPath, stream, strategy.extension)
	if !cfg.Processing.OverwriteExisting {
		if _, err := os.Stat(outputPath); err == nil {
			return subtitleArtifact{Path: outputPath}, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return subtitleArtifact{}, err
	}

	args := []string{"-y", "-i", mediaPath, "-map", fmt.Sprintf("0:%d", stream.Index)}
	if strategy.codec != "" {
		args = append(args, "-c:s", strategy.codec)
		if strategy.codec == "copy" {
			args = append(args, "-vn", "-an")
		}
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, cfg.Tools.FFmpeg, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return subtitleArtifact{}, fmt.Errorf("extract subtitle stream %d: %w: %s", stream.Index, err, strings.TrimSpace(string(output)))
	}

	return subtitleArtifact{Path: outputPath}, nil
}

type subtitleExportStrategy struct {
	extension string
	codec     string
}

func subtitleStrategy(codec string) (subtitleExportStrategy, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "subrip", "srt":
		return subtitleExportStrategy{extension: "srt", codec: "copy"}, true
	case "ass":
		return subtitleExportStrategy{extension: "ass", codec: "copy"}, true
	case "ssa":
		return subtitleExportStrategy{extension: "ssa", codec: "copy"}, true
	case "webvtt":
		return subtitleExportStrategy{extension: "vtt", codec: "copy"}, true
	case "mov_text", "text":
		return subtitleExportStrategy{extension: "srt", codec: "srt"}, true
	default:
		return subtitleExportStrategy{}, false
	}
}

func subtitleOutputPath(mediaPath string, stream ffprobeStream, extension string) string {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	parts := []string{base}

	language := normalizeLanguage(stream.Tags["language"])
	if language != "" {
		parts = append(parts, language)
	}

	note := subtitleNote(stream)
	if note != "" {
		parts = append(parts, note)
	}

	return strings.Join(parts, ".") + "." + extension
}

func subtitleNote(stream ffprobeStream) string {
	parts := make([]string, 0, 3)
	if title := cleanSubtitleSegment(stream.Tags["title"]); title != "" {
		parts = append(parts, title)
	}
	if stream.Disposition.Forced == 1 {
		parts = append(parts, "forced")
	}
	if stream.Disposition.Default == 1 {
		parts = append(parts, "default")
	}

	seen := make(map[string]struct{}, len(parts))
	uniq := make([]string, 0, len(parts))
	for _, part := range parts {
		lower := strings.ToLower(part)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		uniq = append(uniq, part)
	}
	return strings.Join(uniq, ".")
}

func normalizeLanguage(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	return cleanSubtitleSegment(value)
}

var subtitleSegmentSanitizer = regexp.MustCompile(`[^\p{L}\p{N}\-]+`)

func cleanSubtitleSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = subtitleSegmentSanitizer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	return value
}
