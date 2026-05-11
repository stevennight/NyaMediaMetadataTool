package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/store"
)

var bifMagic = []byte{0x89, 0x42, 0x49, 0x46, 0x0d, 0x0a, 0x1a, 0x0a}

func GenerateBIF(ctx context.Context, cfg config.Config, media store.MediaFile) (string, error) {
	if !cfg.Processing.EnableBIF {
		return "", nil
	}
	if strings.TrimSpace(cfg.Tools.FFmpeg) == "" {
		return "", errors.New("ffmpeg is not configured")
	}

	outputPath := bifOutputPath(media.Path, cfg.Processing.BIFWidth, cfg.Processing.BIFInterval)
	if !cfg.Processing.OverwriteExisting {
		if _, err := os.Stat(outputPath); err == nil {
			return outputPath, nil
		}
	}

	tempDir, err := os.MkdirTemp("", "nyammd-bif-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	pattern := filepath.Join(tempDir, "frame-%06d.jpg")
	args := []string{
		"-y",
		"-i", media.Path,
		"-vf", fmt.Sprintf("fps=1/%d,scale=%d:-1", cfg.Processing.BIFInterval, cfg.Processing.BIFWidth),
		"-q:v", "4",
		pattern,
	}
	cmd := exec.CommandContext(ctx, cfg.Tools.FFmpeg, args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("bif ffmpeg: %w: %s", err, strings.TrimSpace(stderrBuf.String()))
	}

	frames, err := filepath.Glob(filepath.Join(tempDir, "frame-*.jpg"))
	if err != nil {
		return "", err
	}
	if len(frames) == 0 {
		return "", errors.New("no BIF frames generated")
	}
	sort.Strings(frames)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}
	if err := writeBIF(outputPath, cfg.Processing.BIFInterval, frames); err != nil {
		return "", err
	}
	return outputPath, nil
}

func bifOutputPath(mediaPath string, width int, interval int) string {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	return fmt.Sprintf("%s-%d-%d.bif", base, width, interval)
}

func writeBIF(outputPath string, intervalSeconds int, frames []string) error {
	images := make([][]byte, 0, len(frames))
	for _, frame := range frames {
		content, err := os.ReadFile(frame)
		if err != nil {
			return err
		}
		images = append(images, content)
	}

	var header bytes.Buffer
	header.Write(bifMagic)
	if _, err := header.Write([]byte{0x00, 0x00, 0x00, 0x00}); err != nil {
		return err
	}
	if err := binary.Write(&header, binary.LittleEndian, uint32(len(images))); err != nil {
		return err
	}
	if err := binary.Write(&header, binary.LittleEndian, uint32(intervalSeconds*1000)); err != nil {
		return err
	}
	if _, err := header.Write(make([]byte, 44)); err != nil {
		return err
	}

	indexStart := header.Len()
	indexSize := (len(images) + 1) * 8
	currentOffset := uint32(indexStart + indexSize)

	for idx, image := range images {
		if err := binary.Write(&header, binary.LittleEndian, uint32(idx)); err != nil {
			return err
		}
		if err := binary.Write(&header, binary.LittleEndian, currentOffset); err != nil {
			return err
		}
		currentOffset += uint32(len(image))
	}
	if err := binary.Write(&header, binary.LittleEndian, uint32(0xffffffff)); err != nil {
		return err
	}
	if err := binary.Write(&header, binary.LittleEndian, currentOffset); err != nil {
		return err
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(header.Bytes()); err != nil {
		return err
	}
	for _, image := range images {
		if _, err := file.Write(image); err != nil {
			return err
		}
	}
	return nil
}
