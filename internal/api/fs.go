package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type directoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type directoryListResponse struct {
	Path    string           `json:"path"`
	Parent  string           `json:"parent"`
	Entries []directoryEntry `json:"entries"`
}

func (s *Server) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusOK, directoryListResponse{Entries: rootDirectories()})
		return
	}

	cleaned := filepath.Clean(path)
	entries, err := os.ReadDir(cleaned)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	dirs := make([]directoryEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, directoryEntry{Name: entry.Name(), Path: filepath.Join(cleaned, entry.Name())})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		parent = ""
	}
	writeJSON(w, http.StatusOK, directoryListResponse{Path: cleaned, Parent: parent, Entries: dirs})
}

func rootDirectories() []directoryEntry {
	if runtime.GOOS == "windows" {
		entries := make([]directoryEntry, 0)
		for letter := 'A'; letter <= 'Z'; letter++ {
			path := string(letter) + ":\\"
			if _, err := os.Stat(path); err == nil {
				entries = append(entries, directoryEntry{Name: path, Path: path})
			}
		}
		return entries
	}
	return []directoryEntry{{Name: "/", Path: "/"}}
}
