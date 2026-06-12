// Package webui provides embedded frontend assets.
package webui

import (
	"embed"
	"io/fs"
	"log/slog"
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
		slog.Error("failed to create sub filesystem", "dir", subDir, "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not available", http.StatusServiceUnavailable)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Cache strategy:
		// - index.html (and the SPA root): never cached, so a new deploy's
		//   hashed asset URLs are always discovered.
		// - hashed assets under /assets/: immutable, browser-cached forever
		//   (their filename changes when content changes).
		isIndex := path == "index.html" || path == ""
		if isIndex {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		} else if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		if f, err := sub.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html for all non-file routes
		r.URL.Path = "/"
		// Fallback also serves index.html → must not be cached.
		if !isIndex {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		fileServer.ServeHTTP(w, r)
	})
}
