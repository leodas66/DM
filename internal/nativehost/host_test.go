package nativehost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/runtime"
)

// testHost points the host at an httptest server without touching the real
// endpoint file or token.
func testHost(t *testing.T, h http.HandlerFunc) (*Host, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Host{
		Version: "test",
		Endpoint: func() (runtime.Endpoint, error) {
			return runtime.Endpoint{Addr: strings.TrimPrefix(srv.URL, "http://"), PID: 1}, nil
		},
		Token:      func() (string, error) { return "t0ken", nil },
		HTTPClient: srv.Client(),
	}, srv
}

func TestAddForwardsCredentialsAndPayload(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/downloads" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer t0ken" {
			t.Errorf("Authorization = %q", got)
		}
		var body struct {
			URL      string            `json:"url"`
			Filename string            `json:"filename"`
			Referrer string            `json:"referrer"`
			Headers  map[string]string `json:"headers"`
			Cookies  []model.Cookie    `json:"cookies"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if body.URL != "https://example.com/file.iso" || body.Filename != "file.iso" {
			t.Errorf("body = %+v", body)
		}
		if body.Referrer != "https://example.com/page" {
			t.Errorf("referrer = %q", body.Referrer)
		}
		if body.Headers["User-Agent"] != "dm/0.2.0" {
			t.Errorf("headers = %v", body.Headers)
		}
		if len(body.Cookies) != 1 || body.Cookies[0].Name != "session" || body.Cookies[0].Value != "abc" {
			t.Errorf("cookies = %+v", body.Cookies)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "dl1", "filename": "file.iso"})
	})

	resp := host.Handle(context.Background(), Request{
		Command:  "add",
		URL:      "https://example.com/file.iso",
		Filename: "  file.iso  ",
		Referrer: "https://example.com/page",
		Headers:  map[string]string{"User-Agent": "dm/0.2.0"},
		Cookies:  []model.Cookie{{Name: "session", Value: "abc"}},
	})
	if !resp.OK || resp.ID != "dl1" || resp.Filename != "file.iso" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddStripsDangerousHeaders(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Headers map[string]string `json:"headers"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body.Headers["X-Bad"]; ok {
			t.Errorf("header with CRLF was forwarded: %v", body.Headers)
		}
		if body.Headers["Accept"] != "text/html" {
			t.Errorf("headers = %v", body.Headers)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "dl1"})
	})

	resp := host.Handle(context.Background(), Request{
		Command: "add",
		URL:     "https://example.com/file.iso",
		Headers: map[string]string{
			"X-Bad":  "value\r\nInjected: 1",
			"Accept": "text/html",
			"":       "nameless",
		},
	})
	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddRejectsNonHTTPURLs(t *testing.T) {
	var calls int32
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	})

	for _, u := range []string{
		"file:///etc/passwd",
		"ftp://example.com/x",
		"javascript:alert(1)",
		"",
		"https://",
	} {
		resp := host.Handle(context.Background(), Request{Command: "add", URL: u})
		if resp.OK || resp.Error == "" {
			t.Errorf("Add(%q) = %+v, want an error", u, resp)
		}
	}
	if calls != 0 {
		t.Errorf("invalid URLs reached the daemon %d times", calls)
	}
}

func TestAddReportsDaemonUnavailable(t *testing.T) {
	host := &Host{
		Version:    "test",
		Endpoint:   func() (runtime.Endpoint, error) { return runtime.Endpoint{}, errors.New("no endpoint") },
		Token:      func() (string, error) { return "t", nil },
		HTTPClient: http.DefaultClient,
	}
	resp := host.Handle(context.Background(), Request{Command: "add", URL: "https://example.com/f"})
	if resp.OK || !strings.Contains(resp.Error, "not running") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddReportsMissingToken(t *testing.T) {
	host := &Host{
		Version:    "test",
		Endpoint:   func() (runtime.Endpoint, error) { return runtime.Endpoint{Addr: "127.0.0.1:1"}, nil },
		Token:      func() (string, error) { return "", errors.New("missing") },
		HTTPClient: http.DefaultClient,
	}
	resp := host.Handle(context.Background(), Request{Command: "add", URL: "https://example.com/f"})
	if resp.OK || !strings.Contains(resp.Error, "token") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddSurfacesDaemonError(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "only http and https URLs are supported"})
	})
	resp := host.Handle(context.Background(), Request{Command: "add", URL: "https://example.com/f"})
	if resp.OK || !strings.Contains(resp.Error, "only http and https") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddFallsBackToStatusWhenBodyIsNotJSON(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("nope"))
	})
	resp := host.Handle(context.Background(), Request{Command: "add", URL: "https://example.com/f"})
	if resp.OK || !strings.Contains(resp.Error, "HTTP 401") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAddRejectsUnreadableResponse(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("not json"))
	})
	resp := host.Handle(context.Background(), Request{Command: "add", URL: "https://example.com/f"})
	if resp.OK || resp.Error == "" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestPing(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("ping must not call the daemon: %s %s", r.Method, r.URL.Path)
	})
	resp := host.Handle(context.Background(), Request{Command: "ping"})
	if !resp.OK || resp.Version != "test" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestUnknownCommand(t *testing.T) {
	host, _ := testHost(t, func(w http.ResponseWriter, r *http.Request) {})
	resp := host.Handle(context.Background(), Request{Command: "delete-everything"})
	if resp.OK || !strings.Contains(resp.Error, "unknown command") {
		t.Fatalf("response = %+v", resp)
	}
}
