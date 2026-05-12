package renamer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

type historyFile struct {
	Batches []HistoryBatch `json:"batches"`
}

func HistoryPath(cfg config.Config) string {
	dir := filepath.Dir(cfg.Database.Path)
	if dir == "." || dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "rename-history.json")
}

func ListHistory(path string, limit int) ([]HistoryBatch, error) {
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
	moves := flattenMoves(batch)
	for i := len(moves) - 1; i >= 0; i-- {
		move := moves[i]
		if _, err := os.Stat(move.To); err != nil {
			return HistoryBatch{}, err
		}
		if _, err := os.Stat(move.From); err == nil && !samePath(move.From, move.To) {
			return HistoryBatch{}, errors.New("undo target already exists: " + move.From)
		}
	}
	for i := len(moves) - 1; i >= 0; i-- {
		move := moves[i]
		if err := os.MkdirAll(filepath.Dir(move.From), 0o755); err != nil {
			return HistoryBatch{}, err
		}
		if err := os.Rename(move.To, move.From); err != nil {
			return HistoryBatch{}, err
		}
	}
	history.Batches[index].Undone = true
	history.Batches[index].UndoneAt = time.Now().Format(time.RFC3339)
	if err := writeHistory(path, history); err != nil {
		return HistoryBatch{}, err
	}
	return history.Batches[index], nil
}

func appendHistoryBatch(path string, batch HistoryBatch) error {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func flattenMoves(batch HistoryBatch) []RenameMove {
	moves := make([]RenameMove, 0)
	for _, item := range batch.Items {
		moves = append(moves, item.Moves...)
	}
	return moves
}
