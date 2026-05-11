package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/* dist/assets/*
var distFS embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			serveIndex(w, r, sub)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if _, err := fs.Stat(sub, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		serveIndex(w, r, sub)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, filesystem fs.FS) {
	content, err := fs.ReadFile(filesystem, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}
