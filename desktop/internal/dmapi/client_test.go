package dmapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishwa/dm/internal/model"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(strings.TrimPrefix(srv.URL, "http://"), "test-token")
}

func requireAuth(t *testing.T, r *http.Request) bool {
	t.Helper()
	if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
		return false
	}
	return true
}

func TestList(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, r) {
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/downloads" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]model.Download{{ID: "a", Filename: "a.bin"}})
	})

	list, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Filename != "a.bin" {
		t.Fatalf("List = %+v", list)
	}
}

func TestListEmptyIsNotNil(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "null")
	})
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Fatal("List returned nil slice for null response")
	}
}

func TestAddSendsJSON(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(t, r) {
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/downloads" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var req AddRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.URL != "https://example.com/a.bin" || req.Filename != "a.bin" || req.Dir != "/tmp" {
			t.Errorf("body = %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(model.Download{ID: "a", Filename: "a.bin", Status: model.StatusQueued})
	})

	d, err := c.Add(context.Background(), AddRequest{
		URL:      "https://example.com/a.bin",
		Filename: "a.bin",
		Dir:      "/tmp",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.ID != "a" || d.Status != model.StatusQueued {
		t.Fatalf("Add = %+v", d)
	}
}

func TestAPIErrorCarriesDaemonMessage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"only http and https URLs are supported"}`)
	})

	_, err := c.Add(context.Background(), AddRequest{URL: "ftp://example.com/a"})
	if err == nil {
		t.Fatal("Add succeeded, want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apiErr.Status)
	}
	if apiErr.Error() != "only http and https URLs are supported" {
		t.Errorf("message = %q", apiErr.Error())
	}
}

func TestControlPaths(t *testing.T) {
	var got []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(http.StatusNoContent)
	})
	ctx := context.Background()
	if err := c.Pause(ctx, "abc"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := c.Resume(ctx, "abc"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := c.Cancel(ctx, "abc"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := c.Remove(ctx, "abc", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := c.Remove(ctx, "abc", true); err != nil {
		t.Fatalf("Remove with file: %v", err)
	}

	want := []string{
		"POST /api/downloads/abc/pause",
		"POST /api/downloads/abc/resume",
		"POST /api/downloads/abc/cancel",
		"DELETE /api/downloads/abc",
		"DELETE /api/downloads/abc?deleteFile=true",
	}
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSettingsAndCategories(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/settings":
			io.WriteString(w, `{"dir":"/home/me/Downloads","maxConcurrent":3}`)
		case "/api/categories":
			io.WriteString(w, `[{"id":"c1","name":"Video","defaultDir":"/videos"}]`)
		default:
			http.NotFound(w, r)
		}
	})

	st, err := c.Settings(context.Background())
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if st.Dir != "/home/me/Downloads" {
		t.Errorf("settings dir = %q", st.Dir)
	}

	cats, err := c.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats) != 1 || cats[0].Name != "Video" {
		t.Errorf("categories = %+v", cats)
	}
}

func TestUnreachableDaemon(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	c := New(addr, "tok")
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("List succeeded against a closed daemon")
	}
}

func TestTokenFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("abc123\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("DM_CONFIG_DIR", dir)

	tok, err := Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "abc123" {
		t.Errorf("Token = %q, want abc123", tok)
	}

	if err := os.Remove(filepath.Join(dir, "token")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Token(); !errors.Is(err, ErrNoToken) {
		t.Errorf("Token after removal = %v, want ErrNoToken", err)
	}
}

func TestDownloadedSumsSegments(t *testing.T) {
	d := model.Download{Segments: []model.Segment{
		{Downloaded: 10}, {Downloaded: 32},
	}}
	if got := Downloaded(d); got != 42 {
		t.Errorf("Downloaded = %d, want 42", got)
	}
}
