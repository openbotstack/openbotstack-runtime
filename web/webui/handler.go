// Package webui provides embedded frontend assets.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed user/dist/*
var userDistFS embed.FS

//go:embed admin/dist/*
var adminDistFS embed.FS

// UserHandler returns an HTTP handler for the user plane (/ui/).
func UserHandler() http.Handler {
	return spaHandler(userDistFS, "user/dist")
}

// AdminHandler returns an HTTP handler for the admin plane (/admin/).
func AdminHandler() http.Handler {
	return spaHandler(adminDistFS, "admin/dist")
}

func spaHandler(embedFS embed.FS, subDir string) http.Handler {
	sub, err := fs.Sub(embedFS, subDir)
	if err != nil {
		panic("failed to create sub filesystem: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// Check if the file actually exists in the embedded FS
		if f, err := sub.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html for all non-file routes
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
