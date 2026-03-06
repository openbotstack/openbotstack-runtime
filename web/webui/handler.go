// Package webui provides embedded frontend assets.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// Handler returns an HTTP handler for the embedded frontend.
func Handler() http.Handler {
	// Strip "dist" prefix
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("failed to create sub filesystem: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Remove /ui prefix if present
		path = strings.TrimPrefix(path, "/ui")
		if path == "" {
			path = "/"
		}

		// Check if path looks like a file (has extension)
		if strings.Contains(path, ".") {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
