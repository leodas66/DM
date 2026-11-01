// Package web embeds the built Svelte UI and serves it at /. It wraps the
// API handler so that a single listener serves both static assets and the
// REST/WebSocket API.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"time"
)

//go:embed dist/*
var embedFS embed.FS

var (
	indexHTML []byte
	indexMod  time.Time
)

func init() {
	var err error
	indexHTML, err = fs.ReadFile(embedFS, "dist/index.html")
	if err != nil {
		panic("web: failed to read embedded index.html: " + err.Error())
	}
	info, err := fs.Stat(embedFS, "dist/index.html")
	if err == nil {
		indexMod = info.ModTime()
	}
}

// Handler wraps an API handler with static file serving for the built UI.
// Routes /api/* to the API handler, /assets/* to the embedded static files,
// and everything else to index.html (single-page app fallback).
func Handler(api http.Handler) http.Handler {
	static, err := fs.Sub(embedFS, "dist")
	if err != nil {
		panic("web: failed to create sub-filesystem: " + err.Error())
	}

	mux := http.NewServeMux()

	// API routes pass through to the API handler (paths like /api/downloads).
	mux.Handle("/api/", api)

	// Static assets.
	mux.Handle("/assets/", http.FileServer(http.FS(static)))

	// SPA fallback: serve index.html for all other routes.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try serving an actual file with an extension.
		if ext := pathExt(r.URL.Path); ext != "" {
			if data, err := fs.ReadFile(static, r.URL.Path); err == nil {
				http.ServeContent(w, r, r.URL.Path, indexMod, bytes.NewReader(data))
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", indexMod, bytes.NewReader(indexHTML))
	})

	return mux
}

func pathExt(p string) string {
	// Only treat it as a file request if the last segment has a dot and
	// isn't a directory-like path.
	last := path.Base(p)
	if last == "." || last == ".." {
		return ""
	}
	dot := path.Ext(last)
	return dot
}
