package renamer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAppendHistoryBatchLockedConcurrentCallersKeepEveryBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rename-history.json")
	const batchCount = 32
	var wait sync.WaitGroup
	errors := make(chan error, batchCount)
	for index := 0; index < batchCount; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			unlock := lockHistory(path)
			defer unlock()
			errors <- appendHistoryBatchLocked(path, HistoryBatch{
				ID:    fmt.Sprintf("batch-%02d", index),
				Items: []HistoryItem{{Path: fmt.Sprintf("item-%02d", index)}},
			})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	batches, err := ListHistory(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != batchCount {
		t.Fatalf("history contains %d batches, want %d", len(batches), batchCount)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".rename-history-*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("history temp files were not cleaned up: matches=%v err=%v", matches, err)
	}
}

func TestUndoHistoryBatchRestoresFilesAndPersistsAtomically(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "before.mkv")
	renamed := filepath.Join(root, "after.mkv")
	if err := os.WriteFile(renamed, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "rename-history.json")
	unlock := lockHistory(path)
	err := appendHistoryBatchLocked(path, HistoryBatch{
		ID:    "batch",
		Items: []HistoryItem{{Moves: []RenameMove{{From: original, To: renamed}}}},
	})
	unlock()
	if err != nil {
		t.Fatal(err)
	}

	batch, err := UndoHistoryBatch(path, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if !batch.Undone {
		t.Fatal("history batch was not marked undone")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("original file was not restored: %v", err)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatalf("renamed path still exists or returned unexpected error: %v", err)
	}
	if _, err := ListHistory(path, 0); err != nil {
		t.Fatalf("atomic history file is unreadable: %v", err)
	}
}
