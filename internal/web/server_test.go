package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// handlerThat captures the paths it sees.
type captureHandler struct {
	paths []string
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.paths = append(h.paths, r.URL.Path)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("api:" + r.URL.Path))
}

func TestHandlerRoutesAPI(t *testing.T) {
	api := &captureHandler{}
	h := Handler(api)

	// API request passes through.
	req := httptest.NewRequest(http.MethodGet, "/api/downloads", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != "api:/api/downloads" {
		t.Fatalf("expected API passthrough, got %q", string(body))
	}
}

func TestHandlerServesIndex(t *testing.T) {
	api := &captureHandler{}
	h := Handler(api)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "<!DOCTYPE html>") {
		t.Fatalf("expected index.html, got %q", string(body[:100]))
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html content type, got %q", w.Header().Get("Content-Type"))
	}
}

func TestHandlerSPAFallback(t *testing.T) {
	api := &captureHandler{}
	h := Handler(api)

	// Unknown route serves index.html (SPA fallback), not 404.
	req := httptest.NewRequest(http.MethodGet, "/downloads/abc123", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "<!DOCTYPE html>") {
		t.Fatalf("expected index.html for SPA fallback, got %q", string(body[:100]))
	}
}

func TestHandlerServesAssets(t *testing.T) {
	api := &captureHandler{}
	h := Handler(api)

	// Discover the actual asset name from the embedded FS instead of
	// hardcoding a hashed filename that changes on every rebuild.
	static, err := fs.Sub(embedFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	entries, err := fs.ReadDir(static, "assets")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no built assets embedded; run `npm run build` in ui/ to cover this test")
	}
	if err != nil {
		t.Fatalf("ReadDir assets: %v", err)
	}
	entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
		// all:dist embeds dotfiles too, and dist/.gitkeep is not an asset.
		return strings.HasPrefix(e.Name(), ".")
	})
	if len(entries) == 0 {
		t.Skip("no built assets embedded; run `npm run build` in ui/ to cover this test")
	}
	assetName := entries[0].Name()

	req := httptest.NewRequest(http.MethodGet, "/assets/"+assetName, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for asset, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty asset body")
	}
}

func TestHandlerAPIPriority(t *testing.T) {
	api := &captureHandler{}
	h := Handler(api)

	// /api/ should go to the API handler, not the SPA fallback.
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != "api:/api/settings" {
		t.Fatalf("expected API passthrough, got %q", string(body))
	}
}
