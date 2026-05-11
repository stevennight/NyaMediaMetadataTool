package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var bifMagic = []byte{0x89, 0x42, 0x49, 0x46, 0x0d, 0x0a, 0x1a, 0x0a}

func main() {
	outputDir := flag.String("o", "", "output directory (default: same as bif file)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: bifunpack [-o outdir] <file.bif>\n")
		os.Exit(1)
	}

	inputPath := flag.Arg(0)
	outDir := *outputDir
	if outDir == "" {
		outDir = filepath.Dir(inputPath)
	}

	if err := unpackBIF(inputPath, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func unpackBIF(path, outDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	header := make([]byte, 64)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	if string(header[:8]) != string(bifMagic) {
		return errors.New("not a valid BIF file (bad magic)")
	}

	count := binary.LittleEndian.Uint32(header[12:16])
	intervalMs := binary.LittleEndian.Uint32(header[16:20])

	if count == 0 {
		return errors.New("BIF contains no images")
	}

	index := make([]uint32, (count+1)*2)
	if err := binary.Read(f, binary.LittleEndian, &index); err != nil {
		return fmt.Errorf("read index: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	prefix := filepath.Join(outDir, base+"-frame")

	fmt.Printf("BIF: %s\n", filepath.Base(path))
	fmt.Printf("  images: %d\n", count)
	fmt.Printf("  interval: %dms\n", intervalMs)
	fmt.Printf("  output: %s\n", outDir)

	for i := uint32(0); i < count; i++ {
		timestamp := index[i*2]
		offset := index[i*2+1]
		nextOffset := index[(i+1)*2+1]
		size := nextOffset - offset

		image := make([]byte, size)
		if _, err := f.ReadAt(image, int64(offset)); err != nil {
			return fmt.Errorf("read image %d at offset %d: %w", i, offset, err)
		}

		target := fmt.Sprintf("%s-%04d-%dms.jpg", prefix, i+1, timestamp)
		if err := os.WriteFile(target, image, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		fmt.Printf("  -> %s (%d bytes)\n", filepath.Base(target), size)
	}

	fmt.Printf("Done. %d images extracted.\n", count)
	return nil
}
