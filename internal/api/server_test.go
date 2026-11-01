package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/store"
)

// harness holds a running API server, its manager, and cleanup funcs.
type harness struct {
	srv    *Server
	addr   string
	mgr    *manager.Manager
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T, token string, origins ...string) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	mgr := manager.New(st, manager.Settings{Dir: dir}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	if err := mgr.Start(ctx); err != nil {
		cancel()
		t.Fatalf("manager start: %v", err)
	}
	s := NewServer(mgr, token, origins...)
	done := make(chan error, 1)
	go func() {
		done <- s.ListenAndServe(ctx)
	}()
	// Wait for the listener to bind.
	deadline := time.Now().Add(5 * time.Second)
	for s.Addr() == "" {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("server did not start")
		}
		time.Sleep(time.Millisecond)
	}
	return &harness{srv: s, addr: s.Addr(), mgr: mgr, cancel: cancel, done: done}
}

func (h *harness) close() {
	h.cancel()
	<-h.done
}

// restReq performs an authenticated or unauthenticated REST request.
func (h *harness) restReq(method, path string, token string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, "http://"+h.addr+path, r)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Origin", "http://"+h.addr)
	return http.DefaultClient.Do(req)
}

func (h *harness) addDownload(t *testing.T, url string, token string) model.Download {
	t.Helper()
	resp, err := h.restReq(http.MethodPost, "/api/downloads", token, addRequest{URL: url})
	if err != nil {
		t.Fatalf("add request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add failed: %s %s", resp.Status, string(b))
	}
	var d model.Download
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("decode add: %v", err)
	}
	return d
}

func (h *harness) listDownloads(t *testing.T, token string) []model.Download {
	t.Helper()
	resp, err := h.restReq(http.MethodGet, "/api/downloads", token, nil)
	if err != nil {
		t.Fatalf("list request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list failed: %s %s", resp.Status, string(b))
	}
	var list []model.Download
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}

// ---- WebSocket test helpers ----

type wsConn struct {
	conn net.Conn
}

func dialWS(t *testing.T, h *harness, token string, origin string) (*wsConn, func()) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	key := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	req := "GET /api/ws?token=" + token + " HTTP/1.1\r\n" +
		"Host: " + h.addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("ws write handshake: %v", err)
	}
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		t.Fatalf("ws read handshake: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "101 Switching Protocols") {
		conn.Close()
		t.Fatalf("ws bad handshake: %s", resp)
	}
	return &wsConn{conn: conn}, func() { conn.Close() }
}

func (c *wsConn) readFrameDeadline() (opcode byte, payload []byte, err error) {
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return readFrame(c.conn)
}

func (c *wsConn) readEventDeadline() (model.Event, error) {
	opcode, payload, err := c.readFrameDeadline()
	if err != nil {
		return model.Event{}, err
	}
	if opcode != 0x1 {
		return model.Event{}, fmt.Errorf("expected text frame, got opcode %#x", opcode)
	}
	var ev model.Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return model.Event{}, err
	}
	return ev, nil
}

// ---- Tests ----

func TestRESTRequiresAuth(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	// No token.
	resp, err := h.restReq(http.MethodGet, "/api/downloads", "", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %s", resp.Status)
	}

	// Wrong token.
	resp, err = h.restReq(http.MethodGet, "/api/downloads", "wrong-token", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %s", resp.Status)
	}

	// Correct token.
	resp, err = h.restReq(http.MethodGet, "/api/downloads", "secret-token", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %s", resp.Status)
	}
}

func TestRESTAddListGet(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	// Empty list.
	list := h.listDownloads(t, "secret-token")
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d items", len(list))
	}

	// Add a download.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"abc"`)
		io.WriteString(w, "hello world!")
	}))
	defer srv.Close()

	d := h.addDownload(t, srv.URL, "secret-token")
	if d.ID == "" {
		t.Fatal("expected download ID")
	}
	if d.URL != srv.URL {
		t.Fatalf("expected URL %s, got %s", srv.URL, d.URL)
	}
	if d.Status != model.StatusQueued {
		t.Fatalf("expected queued, got %s", d.Status)
	}

	// List contains it.
	list = h.listDownloads(t, "secret-token")
	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}
	if list[0].ID != d.ID {
		t.Fatalf("expected ID %s, got %s", d.ID, list[0].ID)
	}

	// Get one.
	got, err := h.mgr.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != d.ID {
		t.Fatalf("expected ID %s, got %s", d.ID, got.ID)
	}

	// Get missing.
	_, err = h.mgr.Get(context.Background(), "nonexistent")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRESTPauseResumeCancel(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		io.WriteString(w, "hello world!")
	}))
	defer srv.Close()

	d := h.addDownload(t, srv.URL, "secret-token")

	// Pause.
	resp, err := h.restReq(http.MethodPost, "/api/downloads/"+d.ID+"/pause", "secret-token", nil)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("pause failed: %s %s", resp.Status, string(b))
	}

	got, _ := h.mgr.Get(context.Background(), d.ID)
	for i := 0; i < 100; i++ {
		if got.Status == model.StatusPaused {
			break
		}
		time.Sleep(10 * time.Millisecond)
		got, _ = h.mgr.Get(context.Background(), d.ID)
	}
	if got.Status != model.StatusPaused {
		t.Fatalf("expected paused, got %s", got.Status)
	}

	// Resume.
	resp, err = h.restReq(http.MethodPost, "/api/downloads/"+d.ID+"/resume", "secret-token", nil)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("resume failed: %s %s", resp.Status, string(b))
	}

	got, _ = h.mgr.Get(context.Background(), d.ID)
	if got.Status != model.StatusQueued && got.Status != model.StatusProbing {
		t.Fatalf("expected queued or probing, got %s", got.Status)
	}

	// Cancel.
	resp, err = h.restReq(http.MethodPost, "/api/downloads/"+d.ID+"/cancel", "secret-token", nil)
	if err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("cancel failed: %s %s", resp.Status, string(b))
	}

	// Wait for the cancel to propagate through the worker.
	got, _ = h.mgr.Get(context.Background(), d.ID)
	for i := 0; i < 100; i++ {
		if got.Status == model.StatusCanceled {
			break
		}
		time.Sleep(10 * time.Millisecond)
		got, _ = h.mgr.Get(context.Background(), d.ID)
	}
	if got.Status != model.StatusCanceled {
		t.Fatalf("expected canceled, got %s", got.Status)
	}

	// Pause on missing download.
	resp, err = h.restReq(http.MethodPost, "/api/downloads/nonexistent/pause", "secret-token", nil)
	if err != nil {
		t.Fatalf("pause missing request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing download, got %s", resp.Status)
	}
}

func TestRESTDelete(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		io.WriteString(w, "hello world!")
	}))
	defer srv.Close()

	d := h.addDownload(t, srv.URL, "secret-token")

	// Delete without deleteFile.
	resp, err := h.restReq(http.MethodDelete, "/api/downloads/"+d.ID, "secret-token", nil)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %s", resp.Status)
	}

	// Confirm it's gone.
	_, err = h.mgr.Get(context.Background(), d.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// Delete missing.
	resp, err = h.restReq(http.MethodDelete, "/api/downloads/nonexistent", "secret-token", nil)
	if err != nil {
		t.Fatalf("delete missing request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing delete, got %s", resp.Status)
	}
}

func TestRESTSettings(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	// GET settings.
	resp, err := h.restReq(http.MethodGet, "/api/settings", "secret-token", nil)
	if err != nil {
		t.Fatalf("settings get request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %s", resp.Status)
	}
	var getResp settingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if getResp.MaxConcurrent != 3 {
		t.Fatalf("expected default maxConcurrent 3, got %d", getResp.MaxConcurrent)
	}

	// PUT settings.
	req := settingsRequest{
		MaxConcurrent: ptr(5),
		GlobalLimit:   ptr(int64(100000)),
		SaveInterval:  "2s",
	}
	resp, err = h.restReq(http.MethodPut, "/api/settings", "secret-token", req)
	if err != nil {
		t.Fatalf("settings put request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("settings put failed: %s %s", resp.Status, string(b))
	}
	var putResp settingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode put settings: %v", err)
	}
	if putResp.MaxConcurrent != 5 {
		t.Fatalf("expected maxConcurrent 5, got %d", putResp.MaxConcurrent)
	}
	if putResp.GlobalLimit != 100000 {
		t.Fatalf("expected globalLimit 100000, got %d", putResp.GlobalLimit)
	}
	if putResp.SaveInterval != "2s" {
		t.Fatalf("expected saveInterval 2s, got %s", putResp.SaveInterval)
	}
}

func TestOriginRejected(t *testing.T) {
	h := newHarness(t, "secret-token", "http://allowed.example.com")
	defer h.close()

	// Allowed origin works.
	resp, err := h.restReq(http.MethodGet, "/api/downloads", "secret-token", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for allowed origin, got %s", resp.Status)
	}

	// Disallowed origin is rejected.
	req, err := http.NewRequest(http.MethodGet, "http://"+h.addr+"/api/downloads", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, got %s", resp.Status)
	}
}

func TestWSRequiresAuth(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	// No token → rejected.
	conn, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	req := "GET /api/ws HTTP/1.1\r\n" +
		"Host: " + h.addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 2048)
	n, _ := conn.Read(buf)
	conn.Close()
	resp := string(buf[:n])
	if strings.Contains(resp, "101 Switching Protocols") {
		t.Fatal("expected WS auth to be rejected without token")
	}

	// Wrong token → rejected.
	conn, err = net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	req = "GET /api/ws?token=wrong HTTP/1.1\r\n" +
		"Host: " + h.addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("write: %v", err)
	}
	n, _ = conn.Read(buf)
	conn.Close()
	resp = string(buf[:n])
	if strings.Contains(resp, "101 Switching Protocols") {
		t.Fatal("expected WS auth to be rejected with wrong token")
	}
}

func TestWSReceivesEvents(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	ws, release := dialWS(t, h, "secret-token", "http://"+h.addr)
	defer release()

	// Add a download → should receive EventAdded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		io.WriteString(w, "hello world!")
	}))
	defer srv.Close()

	d := h.addDownload(t, srv.URL, "secret-token")

	ev, err := ws.readEventDeadline()
	if err != nil {
		t.Fatalf("read WS event: %v", err)
	}
	if ev.Type != model.EventAdded {
		t.Fatalf("expected added event, got %s", ev.Type)
	}
	if ev.Download == nil || ev.Download.ID != d.ID {
		t.Fatalf("expected download %s in event, got %v", d.ID, ev.Download)
	}

	// Pause → should receive EventStatusChanged.
	resp, err := h.restReq(http.MethodPost, "/api/downloads/"+d.ID+"/pause", "secret-token", nil)
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	resp.Body.Close()

	ev, err = ws.readEventDeadline()
	if err != nil {
		t.Fatalf("read WS event: %v", err)
	}
	if ev.Type != model.EventStatusChanged {
		t.Fatalf("expected status event, got %s", ev.Type)
	}
}

func TestWSRejectsBadOrigin(t *testing.T) {
	h := newHarness(t, "secret-token", "http://allowed.example.com")
	defer h.close()

	conn, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	key := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	req := "GET /api/ws?token=secret-token HTTP/1.1\r\n" +
		"Host: " + h.addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: http://evil.example.com\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 2048)
	n, _ := conn.Read(buf)
	conn.Close()
	resp := string(buf[:n])
	if strings.Contains(resp, "101 Switching Protocols") {
		t.Fatal("expected WS to reject bad origin")
	}
}

func TestLoadOrCreateToken(t *testing.T) {
	dir := t.TempDir()

	tok, err := loadOrCreateTokenIn(dir)
	if err != nil {
		t.Fatalf("loadOrCreateTokenIn: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	if len(tok) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(tok))
	}

	// File should exist with 0600.
	path := filepath.Join(dir, "dm", "token")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}

	// Loading again returns the same token.
	tok2, err := loadOrCreateTokenIn(dir)
	if err != nil {
		t.Fatalf("second loadOrCreateTokenIn: %v", err)
	}
	if tok2 != tok {
		t.Fatal("expected same token on reload")
	}
}

func TestAcceptKey(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := acceptKey(key); got != want {
		t.Fatalf("acceptKey(%q) = %q, want %q", key, got, want)
	}
}

func TestWriteTextFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello")
	if err := writeTextFrame(&buf, payload); err != nil {
		t.Fatalf("writeTextFrame: %v", err)
	}
	opcode, got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("expected opcode 0x1, got %#x", opcode)
	}
	if string(got) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(got))
	}
}

func ptr[T any](v T) *T { return &v }

// ---- Category API tests ----

func TestCategoriesCRUD(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	// Empty list.
	list, err := h.mgr.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty category list, got %d", len(list))
	}

	// Add a category.
	resp, err := h.restReq(http.MethodPost, "/api/categories", "secret-token", categoryRequest{
		Name:       "Videos",
		DefaultDir: "/home/user/downloads/videos",
	})
	if err != nil {
		t.Fatalf("add category request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add category failed: %s %s", resp.Status, string(b))
	}
	var cat model.Category
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		t.Fatalf("decode category: %v", err)
	}
	if cat.ID == "" {
		t.Fatal("expected category ID")
	}
	if cat.Name != "Videos" {
		t.Fatalf("expected name Videos, got %s", cat.Name)
	}
	if cat.DefaultDir != "/home/user/downloads/videos" {
		t.Fatalf("expected defaultDir, got %s", cat.DefaultDir)
	}

	// List contains it.
	list, err = h.mgr.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(list) != 1 || list[0].ID != cat.ID {
		t.Fatalf("expected 1 category with id %s, got %v", cat.ID, list)
	}

	// Get one.
	got, err := h.mgr.GetCategory(context.Background(), cat.ID)
	if err != nil {
		t.Fatalf("get category: %v", err)
	}
	if got.ID != cat.ID || got.Name != "Videos" {
		t.Fatalf("expected category %s, got %+v", cat.ID, got)
	}

	// Get missing.
	_, err = h.mgr.GetCategory(context.Background(), "nonexistent")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Delete.
	resp, err = h.restReq(http.MethodDelete, "/api/categories/"+cat.ID, "secret-token", nil)
	if err != nil {
		t.Fatalf("delete category request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %s", resp.Status)
	}

	// Confirm it's gone.
	_, err = h.mgr.GetCategory(context.Background(), cat.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	// Delete missing.
	resp, err = h.restReq(http.MethodDelete, "/api/categories/nonexistent", "secret-token", nil)
	if err != nil {
		t.Fatalf("delete missing category request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing delete, got %s", resp.Status)
	}
}

func TestCategoriesAddRequiresName(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	resp, err := h.restReq(http.MethodPost, "/api/categories", "secret-token", categoryRequest{
		Name: "",
	})
	if err != nil {
		t.Fatalf("add category request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %s", resp.Status)
	}
}

func TestCategoryDefaultDirAppliedToDownload(t *testing.T) {
	h := newHarness(t, "secret-token")
	defer h.close()

	dir := t.TempDir()

	// Add a category with a default dir.
	resp, err := h.restReq(http.MethodPost, "/api/categories", "secret-token", categoryRequest{
		Name:       "Documents",
		DefaultDir: dir,
	})
	if err != nil {
		t.Fatalf("add category: %v", err)
	}
	defer resp.Body.Close()
	var cat model.Category
	if err := json.NewDecoder(resp.Body).Decode(&cat); err != nil {
		t.Fatalf("decode category: %v", err)
	}

	// Add a download with the category but no dir -> should use category default.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Accept-Ranges", "bytes")
		io.WriteString(w, "hello world!")
	}))
	defer srv.Close()

	d := h.addDownloadWithCategory(t, srv.URL, cat.ID, "secret-token")
	if d.CategoryID != cat.ID {
		t.Fatalf("expected CategoryID %s, got %s", cat.ID, d.CategoryID)
	}
	if d.Dir != dir {
		t.Fatalf("expected dir %s, got %s", dir, d.Dir)
	}
}

// addDownloadWithCategory adds a download via the manager directly with a category.
func (h *harness) addDownloadWithCategory(t *testing.T, url string, categoryID string, token string) model.Download {
	t.Helper()
	d, err := h.mgr.Add(context.Background(), manager.AddRequest{
		URL:        url,
		CategoryID: categoryID,
	})
	if err != nil {
		t.Fatalf("add download: %v", err)
	}
	return d
}
