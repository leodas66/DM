// Package uihandoff carries browser download requests from the Native
// Messaging host to the running GTK desktop app. The request is transient:
// it crosses a per-user Unix socket and is never written to disk.
package uihandoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/runtime"
)

const (
	socketName  = "desktop.sock"
	maxMessage  = 1 << 20
	sendTimeout = 10 * time.Second
)

// Request contains the browser context needed to review and, after the user
// confirms, start a download. Cookies and headers remain memory-only.
type Request struct {
	URL        string            `json:"url"`
	Filename   string            `json:"filename,omitempty"`
	Referrer   string            `json:"referrer,omitempty"`
	MIME       string            `json:"mime,omitempty"`
	TotalBytes int64             `json:"totalBytes,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Cookies    []model.Cookie    `json:"cookies,omitempty"`
}

// Response acknowledges that the GTK app displayed the review dialog.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseError means the desktop app received the request but could not
// display it. Callers should not launch another desktop instance.
type ResponseError struct{ Message string }

func (e *ResponseError) Error() string { return e.Message }

// Server owns the local socket used by one desktop process.
type Server struct {
	listener *net.UnixListener
	path     string
	info     os.FileInfo
	once     sync.Once
}

// Listen creates a 0600 Unix socket in the user's 0700 DM runtime directory.
// A stale socket from a crashed desktop process is removed safely.
func Listen() (*Server, error) {
	dir, err := runtime.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create DM runtime directory: %w", err)
	}
	path := filepath.Join(dir, socketName)
	if len(path) >= 104 {
		return nil, fmt.Errorf("DM desktop socket path is too long: %s", path)
	}

	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to replace non-socket desktop path: %s", path)
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Getuid()) {
			return nil, errors.New("desktop socket is not owned by the current user")
		}
		conn, dialErr := net.DialTimeout("unix", path, 150*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, errors.New("another DM desktop handoff listener is already active")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale desktop socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect desktop socket: %w", err)
	}

	addr := &net.UnixAddr{Name: path, Net: "unix"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for browser downloads: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, fmt.Errorf("secure desktop socket: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		listener.Close()
		os.Remove(path)
		return nil, fmt.Errorf("inspect new desktop socket: %w", err)
	}
	return &Server{listener: listener, path: path, info: info}, nil
}

// Serve handles requests until ctx is cancelled or the listener is closed.
// Each request is decoded and passed to handle; the reply is sent only after
// the GTK main loop confirms that the review dialog has been presented.
func (s *Server) Serve(ctx context.Context, handle func(Request) error) error {
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept desktop handoff: %w", err)
		}
		go s.serveConn(ctx, conn, handle)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn, handle func(Request) error) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sendTimeout))

	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxMessage)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "The browser request was unreadable."})
		return
	}
	if err := Validate(req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
		return
	}
	if ctx.Err() != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "DM is shutting down."})
		return
	}
	if err := handle(req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(Response{OK: true})
}

// Close removes only the socket this server created.
func (s *Server) Close() error {
	var closeErr error
	s.once.Do(func() {
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = err
		}
		if current, err := os.Lstat(s.path); err == nil && os.SameFile(s.info, current) {
			if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) && closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

// Send sends one request to the local desktop and waits for the dialog-open
// acknowledgement. It never contacts the daemon and never writes the request
// to disk.
func Send(ctx context.Context, req Request) error {
	if err := Validate(req); err != nil {
		return err
	}
	dir, err := runtime.Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, socketName)
	dialer := net.Dialer{Timeout: sendTimeout}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("DM desktop is not accepting browser requests: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(sendTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send browser request to DM: %w", err)
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&response); err != nil {
		return fmt.Errorf("read DM desktop response: %w", err)
	}
	if !response.OK {
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = "DM could not open the download review dialog."
		}
		return &ResponseError{Message: message}
	}
	return nil
}

// Validate rejects malformed or oversized requests before they cross the
// process boundary. Only remote HTTP(S) links are accepted.
func Validate(req Request) error {
	raw := strings.TrimSpace(req.URL)
	if raw == "" || len(raw) > 8192 {
		return errors.New("The download address is empty or too long.")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("Only complete http:// and https:// download links are supported.")
	}
	if len(req.Filename) > 1024 || len(req.Referrer) > 8192 || len(req.MIME) > 256 {
		return errors.New("The browser supplied oversized download metadata.")
	}
	if req.TotalBytes < 0 {
		return errors.New("The browser supplied an invalid file size.")
	}
	if len(req.Headers) > 64 || len(req.Cookies) > 256 {
		return errors.New("The browser supplied too much request metadata.")
	}
	for name, value := range req.Headers {
		if strings.TrimSpace(name) == "" || len(name) > 256 || len(value) > 8192 || strings.ContainsAny(name+value, "\r\n") {
			return errors.New("The browser supplied an invalid request header.")
		}
	}
	for _, cookie := range req.Cookies {
		if strings.TrimSpace(cookie.Name) == "" || len(cookie.Name) > 256 || len(cookie.Value) > 8192 || strings.ContainsAny(cookie.Name+cookie.Value, "\r\n") {
			return errors.New("The browser supplied an invalid cookie.")
		}
	}
	return nil
}
