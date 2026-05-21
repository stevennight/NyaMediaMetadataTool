package fileaudit

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

type LocalFS struct{}

func (LocalFS) Walk(ctx context.Context, root string, fn func(FileInfo) error) error {
	return filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return fn(FileInfo{Path: name, Size: info.Size()})
	})
}

func (LocalFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return os.Open(name)
}
