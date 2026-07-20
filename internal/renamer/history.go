package renamer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"NyaMediaMetadataTool/internal/config"
)

type HistoryBatch struct {
	ID        string        `json:"id"`
	CreatedAt string        `json:"createdAt"`
	Undone    bool          `json:"undone"`
	UndoneAt  string        `json:"undoneAt,omitempty"`
	Items     []HistoryItem `json:"items"`
}

type HistoryItem struct {
	Path    string       `json:"path"`
	NewPath string       `json:"newPath"`
	Status  string       `json:"status"`
	Message string       `json:"message"`
	Moves   []RenameMove `json:"moves"`
}

type RenameMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type UndoCheckResult struct {
	CanUndo bool            `json:"canUndo"`
	Batch   HistoryBatch    `json:"batch"`
	Items   []UndoCheckItem `json:"items"`
}

type UndoCheckItem struct {
	From   string `json:"from"`
	To     string `json:"to"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

type historyFile struct {
	Batches []HistoryBatch `json:"batches"`
}

var historyLocks sync.Map

func HistoryPath(cfg config.Config) string {
	dir := filepath.Dir(cfg.Database.Path)
	if dir == "." || dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "rename-history.json")
}

func ListHistory(path string, limit int) ([]HistoryBatch, error) {
	unlock := lockHistory(path)
	defer unlock()
	history, err := readHistory(path)
	if err != nil {
		return nil, err
	}
	batches := append([]HistoryBatch(nil), history.Batches...)
	sort.SliceStable(batches, func(i int, j int) bool { return batches[i].CreatedAt > batches[j].CreatedAt })
	if limit > 0 && len(batches) > limit {
		batches = batches[:limit]
	}
	return batches, nil
}

func UndoHistoryBatch(path string, id string) (HistoryBatch, error) {
	unlock := lockHistory(path)
	defer unlock()
	history, err := readHistory(path)
	if err != nil {
		return HistoryBatch{}, err
	}
	index := -1
	for i, batch := range history.Batches {
		if batch.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return HistoryBatch{}, errors.New("rename history batch not found")
	}
	batch := history.Batches[index]
	if batch.Undone {
		return HistoryBatch{}, errors.New("rename history batch already undone")
	}
	check := checkBatchUndoable(batch)
	if !check.CanUndo {
		return HistoryBatch{}, errors.New("rename history batch is not fully undoable")
	}
	moves := flattenMoves(batch)
	if err := reverseRenameMoves(moves); err != nil {
		return HistoryBatch{}, err
	}
	history.Batches[index].Undone = true
	history.Batches[index].UndoneAt = time.Now().Format(time.RFC3339)
	if err := writeHistory(path, history); err != nil {
		if restoreErr := restoreRenameMoves(moves); restoreErr != nil {
			return HistoryBatch{}, fmt.Errorf("save undo history: %v; restore renamed files: %w", err, restoreErr)
		}
		return HistoryBatch{}, fmt.Errorf("save undo history: %w", err)
	}
	return history.Batches[index], nil
}

func CheckHistoryBatchUndo(path string, id string) (UndoCheckResult, error) {
	unlock := lockHistory(path)
	defer unlock()
	history, err := readHistory(path)
	if err != nil {
		return UndoCheckResult{}, err
	}
	for _, batch := range history.Batches {
		if batch.ID == id {
			return checkBatchUndoable(batch), nil
		}
	}
	return UndoCheckResult{}, errors.New("rename history batch not found")
}

func checkBatchUndoable(batch HistoryBatch) UndoCheckResult {
	result := UndoCheckResult{CanUndo: !batch.Undone, Batch: batch}
	if batch.Undone {
		result.Items = append(result.Items, UndoCheckItem{OK: false, Reason: "批次已撤销"})
		return result
	}
	moves := flattenMoves(batch)
	for i := len(moves) - 1; i >= 0; i-- {
		move := moves[i]
		item := UndoCheckItem{From: move.From, To: move.To, OK: true}
		if _, err := os.Stat(move.To); err != nil {
			item.OK = false
			item.Reason = "当前路径不存在: " + err.Error()
		} else if _, err := os.Stat(move.From); err == nil && !samePath(move.From, move.To) {
			item.OK = false
			item.Reason = "原路径已存在"
		}
		if !item.OK {
			result.CanUndo = false
		}
		result.Items = append(result.Items, item)
	}
	return result
}

func appendHistoryBatchLocked(path string, batch HistoryBatch) error {
	if len(batch.Items) == 0 {
		return nil
	}
	history, err := readHistory(path)
	if err != nil {
		return err
	}
	history.Batches = append(history.Batches, batch)
	return writeHistory(path, history)
}

func readHistory(path string) (historyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return historyFile{}, nil
		}
		return historyFile{}, err
	}
	var history historyFile
	if err := json.Unmarshal(data, &history); err != nil {
		return historyFile{}, err
	}
	return history, nil
}

func writeHistory(path string, history historyFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(dir, ".rename-history-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		_ = temporary.Close()
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	installed = true
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func lockHistory(path string) func() {
	key, err := filepath.Abs(path)
	if err != nil {
		key = filepath.Clean(path)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
	value, _ := historyLocks.LoadOrStore(key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func reverseRenameMoves(moves []RenameMove) error {
	reversed := make([]RenameMove, 0, len(moves))
	for i := len(moves) - 1; i >= 0; i-- {
		move := moves[i]
		if err := os.MkdirAll(filepath.Dir(move.From), 0o755); err != nil {
			return joinMoveRollbackError(err, restoreReversedMoves(reversed))
		}
		if err := os.Rename(move.To, move.From); err != nil {
			return joinMoveRollbackError(err, restoreReversedMoves(reversed))
		}
		reversed = append(reversed, move)
	}
	return nil
}

func restoreRenameMoves(moves []RenameMove) error {
	var errs []error
	for _, move := range moves {
		if err := os.MkdirAll(filepath.Dir(move.To), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(move.From, move.To); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreReversedMoves(moves []RenameMove) error {
	reordered := make([]RenameMove, len(moves))
	for index := range moves {
		reordered[len(moves)-1-index] = moves[index]
	}
	return restoreRenameMoves(reordered)
}

func joinMoveRollbackError(operationErr error, rollbackErr error) error {
	if rollbackErr == nil {
		return operationErr
	}
	return fmt.Errorf("rename operation: %v; rollback: %w", operationErr, rollbackErr)
}

func flattenMoves(batch HistoryBatch) []RenameMove {
	moves := make([]RenameMove, 0)
	for _, item := range batch.Items {
		moves = append(moves, item.Moves...)
	}
	return moves
}
