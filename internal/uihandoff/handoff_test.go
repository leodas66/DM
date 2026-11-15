package uihandoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

func TestSendDeliversRequestAndWaitsForUIAcknowledgement(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	server, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Close()

	received := make(chan Request, 1)
	go func() {
		if err := server.Serve(ctx, func(req Request) error {
			received <- req
			return nil
		}); err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}()

	req := Request{
		URL:        "https://cdn.example/file.iso?token=one",
		Filename:   "file.iso",
		Referrer:   "https://example.com/page",
		MIME:       "application/octet-stream",
		TotalBytes: 1024,
		Headers:    map[string]string{"User-Agent": "Browser/1"},
		Cookies:    []model.Cookie{{Name: "session", Value: "secret"}},
	}
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer sendCancel()
	if err := Send(sendCtx, req); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case got := <-received:
		if got.URL != req.URL || got.Filename != req.Filename || got.Referrer != req.Referrer ||
			got.MIME != req.MIME || got.TotalBytes != req.TotalBytes ||
			got.Headers["User-Agent"] != "Browser/1" ||
			len(got.Cookies) != 1 || got.Cookies[0].Value != "secret" {
			t.Fatalf("received request = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("desktop handler did not receive the request")
	}
}

func TestSendReturnsDesktopRejection(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	server, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Close()
	go server.Serve(ctx, func(Request) error {
		return errors.New("desktop is not connected to the daemon")
	})

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer sendCancel()
	err = Send(sendCtx, Request{URL: "https://example.com/file"})
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr.Message != "desktop is not connected to the daemon" {
		t.Fatalf("Send() error = %v, want a ResponseError", err)
	}
}

func TestListenRemovesOwnedStaleSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "dm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, socketName)
	old, err := netListenUnixForTest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	server, err := Listen()
	if err != nil {
		t.Fatalf("Listen() did not replace stale socket: %v", err)
	}
	defer server.Close()
}

func TestValidateOnlyAcceptsSafeHTTPRequests(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want bool
	}{
		{name: "https", req: Request{URL: "https://example.com/file"}, want: true},
		{name: "http", req: Request{URL: "http://example.com/file"}, want: true},
		{name: "file url", req: Request{URL: "file:///etc/passwd"}},
		{name: "missing host", req: Request{URL: "https:///file"}},
		{name: "CRLF header", req: Request{URL: "https://example.com/file", Headers: map[string]string{"X-Test": "ok\r\nInjected: yes"}}},
		{name: "negative size", req: Request{URL: "https://example.com/file", TotalBytes: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.req)
			if (err == nil) != tt.want {
				t.Fatalf("Validate() error = %v, want valid=%v", err, tt.want)
			}
		})
	}
}
