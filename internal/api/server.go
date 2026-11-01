// Package api exposes the REST endpoints and the WebSocket event stream on
// 127.0.0.1, guarded by a bearer token and an Origin allowlist.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vishwa/dm/internal/manager"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/store"
)

const (
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	authHeader   = "Authorization"
	tokenParam   = "token"
	originHeader = "Origin"
)

// ErrMissingToken is returned when no bearer token is configured.
var ErrMissingToken = errors.New("api: no bearer token configured")

// ErrInvalidToken is returned when the bearer token does not match.
var ErrInvalidToken = errors.New("api: invalid bearer token")

// addRequest is the JSON body for POST /api/downloads.
type addRequest struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename,omitempty"`
	Dir      string            `json:"dir,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Cookies  []model.Cookie    `json:"cookies,omitempty"`
	Referrer string            `json:"referrer,omitempty"`
}

// settingsRequest is the JSON body for PUT /api/settings. Pointer fields
// distinguish "not provided" from "set to zero".
type settingsRequest struct {
	Dir              string `json:"dir,omitempty"`
	MaxConcurrent    *int   `json:"maxConcurrent,omitempty"`
	Connections      *int   `json:"connections,omitempty"`
	MinSegmentSize   *int64 `json:"minSegmentSize,omitempty"`
	GlobalLimit      *int64 `json:"globalLimit,omitempty"`
	PerDownloadLimit *int64 `json:"perDownloadLimit,omitempty"`
	SaveInterval     string `json:"saveInterval,omitempty"`
}

// settingsResponse is the JSON body for GET/PUT /api/settings.
type settingsResponse struct {
	Dir              string `json:"dir"`
	MaxConcurrent    int    `json:"maxConcurrent"`
	Connections      int    `json:"connections"`
	MinSegmentSize   int64  `json:"minSegmentSize"`
	GlobalLimit      int64  `json:"globalLimit"`
	PerDownloadLimit int64  `json:"perDownloadLimit"`
	SaveInterval     string `json:"saveInterval"`
}

// errorResponse is a JSON error body.
type errorResponse struct {
	Error string `json:"error"`
}

// Server is the HTTP API server. It binds to 127.0.0.1 and exposes REST
// endpoints plus a WebSocket event stream, guarded by a bearer token and an
// Origin allowlist. Only the Manager writes to the store; the API only
// issues commands to the Manager.
type Server struct {
	mgr   *manager.Manager
	token string
	ln    net.Listener
	srv   http.Server

	mu      sync.Mutex
	origins map[string]bool
	clients map[*wsClient]struct{}
}

// wsClient is one connected WebSocket subscriber.
type wsClient struct {
	conn net.Conn
	send chan []byte
}

// NewServer builds an API server over mgr. token must be non-empty. origins
// are the allowed Origin values; when empty, only same-host origins are
// accepted.
func NewServer(mgr *manager.Manager, token string, origins ...string) *Server {
	s := &Server{
		mgr:     mgr,
		token:   token,
		origins: make(map[string]bool),
		clients: make(map[*wsClient]struct{}),
	}
	for _, o := range origins {
		s.origins[o] = true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/downloads", s.withAuth(s.handleAdd))
	mux.HandleFunc("GET /api/downloads", s.withAuth(s.handleList))
	mux.HandleFunc("GET /api/downloads/{id}", s.withAuth(s.handleGet))
	mux.HandleFunc("POST /api/downloads/{id}/pause", s.withAuth(s.handlePause))
	mux.HandleFunc("POST /api/downloads/{id}/resume", s.withAuth(s.handleResume))
	mux.HandleFunc("POST /api/downloads/{id}/cancel", s.withAuth(s.handleCancel))
	mux.HandleFunc("DELETE /api/downloads/{id}", s.withAuth(s.handleDelete))
	mux.HandleFunc("GET /api/settings", s.withAuth(s.handleSettingsGet))
	mux.HandleFunc("PUT /api/settings", s.withAuth(s.handleSettingsPut))
	mux.HandleFunc("GET /api/ws", s.handleWS)

	s.srv.Handler = mux
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.srv.Handler.ServeHTTP(w, r)
}

// withAuth wraps a handler with bearer-token and Origin checks.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(r) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
			return
		}
		if !s.checkOrigin(r) {
			writeJSON(w, http.StatusForbidden, errorResponse{Error: "forbidden origin"})
			return
		}
		next(w, r)
	}
}

func (s *Server) checkAuth(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	auth := r.Header.Get(authHeader)
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	provided := strings.TrimPrefix(auth, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func (s *Server) wsToken(r *http.Request) string {
	if auth := r.Header.Get(authHeader); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get(tokenParam)
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get(originHeader)
	if origin == "" {
		return true
	}
	if s.origins[origin] {
		return true
	}
	// Same-host origin is always allowed.
	if origin == "http://"+r.Host || origin == "https://"+r.Host {
		return true
	}
	return false
}

// ListenAndServe binds to 127.0.0.1 on an ephemeral port, subscribes to the
// Manager's event bus for WebSocket broadcasting, and serves until ctx is
// cancelled. On shutdown it pauses running downloads and persists state.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	events, cancel := s.mgr.Events()
	defer cancel()
	go s.broadcastLoop(ctx, events)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		s.srv.Close()
		<-errCh
		return s.mgr.Shutdown(context.Background())
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return s.mgr.Shutdown(context.Background())
		}
		return err
	}
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close stops the HTTP server without shutting down the Manager.
func (s *Server) Close() error {
	return s.srv.Close()
}

// broadcastLoop reads Manager events and fans them out to WebSocket clients.
func (s *Server) broadcastLoop(ctx context.Context, events <-chan model.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.broadcast(ev)
		}
	}
}

func (s *Server) broadcast(ev model.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("api: failed to marshal event", "error", err)
		return
	}
	s.mu.Lock()
	clients := make([]*wsClient, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Slow subscriber: drop the event rather than stall.
		}
	}
}

func (s *Server) addClient(c *wsClient) {
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) removeClient(c *wsClient) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	close(c.send)
}

// ---- REST handlers ----

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	d, err := s.mgr.Add(r.Context(), manager.AddRequest{
		URL:      req.URL,
		Filename: req.Filename,
		Dir:      req.Dir,
		Headers:  req.Headers,
		Cookies:  req.Cookies,
		Referrer: req.Referrer,
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
		} else {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	list, err := s.mgr.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	if list == nil {
		list = []model.Download{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.mgr.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.mgr.Pause(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.mgr.Resume(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.mgr.Cancel(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleteFile := r.URL.Query().Get("deleteFile") == "true"
	if err := s.mgr.Remove(r.Context(), id, deleteFile); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Settings()
	writeJSON(w, http.StatusOK, settingsResponse{
		Dir:              st.Dir,
		MaxConcurrent:    st.MaxConcurrent,
		Connections:      st.Connections,
		MinSegmentSize:   st.MinSegmentSize,
		GlobalLimit:      st.GlobalLimit,
		PerDownloadLimit: st.PerDownloadLimit,
		SaveInterval:     st.SaveInterval.String(),
	})
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	st, err := parseSettingsRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.mgr.UpdateSettings(st)

	current := s.mgr.Settings()
	writeJSON(w, http.StatusOK, settingsResponse{
		Dir:              current.Dir,
		MaxConcurrent:    current.MaxConcurrent,
		Connections:      current.Connections,
		MinSegmentSize:   current.MinSegmentSize,
		GlobalLimit:      current.GlobalLimit,
		PerDownloadLimit: current.PerDownloadLimit,
		SaveInterval:     current.SaveInterval.String(),
	})
}

// ---- WebSocket ----

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := s.wsToken(r)
	if token != s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.checkOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		conn.Close()
		return
	}
	accept := acceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		conn.Close()
		return
	}

	client := &wsClient{conn: conn, send: make(chan []byte, 256)}
	s.addClient(client)
	defer s.removeClient(client)

	go client.readLoop()

	for msg := range client.send {
		if err := writeTextFrame(conn, msg); err != nil {
			return
		}
	}
}

func (c *wsClient) readLoop() {
	defer c.conn.Close()
	for {
		opcode, payload, err := readFrame(c.conn)
		if err != nil {
			return
		}
		switch opcode {
		case 0x8: // close
			_ = writeCloseFrame(c.conn, payload)
			return
		case 0x9: // ping
			_ = writePongFrame(c.conn, payload)
		}
	}
}

// ---- token management ----

// configDir returns the per-user config directory for dm (baseDir/dm).
func configDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, "dm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// tokenPath returns the path to the bearer token file.
func tokenPath(baseDir string) (string, error) {
	dir, err := configDir(baseDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token"), nil
}

// defaultConfigBase returns the OS user config directory.
func defaultConfigBase() (string, error) {
	return os.UserConfigDir()
}

// generateToken creates a random 256-bit bearer token as lowercase hex.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// LoadOrCreateToken loads the bearer token from the config dir, generating
// and storing one (mode 0600) if none exists.
func LoadOrCreateToken() (string, error) {
	base, err := defaultConfigBase()
	if err != nil {
		return "", err
	}
	return loadOrCreateTokenIn(base)
}

// loadOrCreateTokenIn loads or creates the token under baseDir/dm/token.
func loadOrCreateTokenIn(baseDir string) (string, error) {
	path, err := tokenPath(baseDir)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	tok, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("api: failed to write response", "error", err)
	}
}

func parseSettingsRequest(req settingsRequest) (manager.Settings, error) {
	st := manager.Settings{
		Dir: req.Dir,
	}
	if req.MaxConcurrent != nil {
		st.MaxConcurrent = *req.MaxConcurrent
	}
	if req.Connections != nil {
		st.Connections = *req.Connections
	}
	if req.MinSegmentSize != nil {
		st.MinSegmentSize = *req.MinSegmentSize
	}
	if req.GlobalLimit != nil {
		st.GlobalLimit = *req.GlobalLimit
	}
	if req.PerDownloadLimit != nil {
		st.PerDownloadLimit = *req.PerDownloadLimit
	}
	if req.SaveInterval != "" {
		d, err := time.ParseDuration(req.SaveInterval)
		if err != nil {
			return manager.Settings{}, fmt.Errorf("invalid saveInterval: %w", err)
		}
		st.SaveInterval = d
	}
	return st, nil
}

// ---- WebSocket framing (RFC 6455) ----

func acceptKey(challengeKey string) string {
	h := sha1.New()
	h.Write([]byte(challengeKey + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func readFrame(r io.Reader) (opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	opcode = header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := int(header[1] & 0x7f)

	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}

	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}

	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
	}
	return opcode, payload, nil
}

func writeTextFrame(w io.Writer, payload []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(0x80 | 0x01) // FIN + text opcode

	length := len(payload)
	if length < 126 {
		buf.WriteByte(byte(length))
	} else if length <= 65535 {
		buf.WriteByte(126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(length))
		buf.Write(ext[:])
	} else {
		buf.WriteByte(127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(length))
		buf.Write(ext[:])
	}
	buf.Write(payload)
	_, err := w.Write(buf.Bytes())
	return err
}

func writeCloseFrame(w io.Writer, payload []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(0x88) // FIN + close opcode
	buf.WriteByte(byte(len(payload)))
	buf.Write(payload)
	_, err := w.Write(buf.Bytes())
	return err
}

func writePongFrame(w io.Writer, payload []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(0x8a) // FIN + pong opcode
	buf.WriteByte(byte(len(payload)))
	buf.Write(payload)
	_, err := w.Write(buf.Bytes())
	return err
}
