// Package dmapi is the GTK client's view of the dm daemon. It speaks the same
// authenticated loopback REST API and WebSocket event stream as any other
// client; it never imports the Manager or opens SQLite.
package dmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/runtime"
)

// ErrDaemonNotRunning means no daemon has published an endpoint, or the
// published metadata could not be read. The client should offer a retry.
var ErrDaemonNotRunning = errors.New("dm daemon is not running")

// ErrNoToken means the daemon endpoint exists but no bearer token has been
// written yet, so the client cannot authenticate.
var ErrNoToken = errors.New("no daemon token found; start the daemon once")

// Client talks to one daemon instance. The address is fixed at construction;
// after a daemon restart the caller must Connect again, because the ephemeral
// port changes.
type Client struct {
	addr  string
	token string
	base  string
	hc    *http.Client
}

// Connect discovers the running daemon's address and loads its bearer token.
func Connect() (*Client, error) {
	ep, err := runtime.Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrDaemonNotRunning
		}
		return nil, fmt.Errorf("read daemon endpoint: %w", err)
	}
	token, err := Token()
	if err != nil {
		return nil, err
	}
	return New(ep.Addr, token), nil
}

// New builds a client for a known address and token.
func New(addr, token string) *Client {
	return &Client{
		addr:  addr,
		token: token,
		base:  "http://" + addr,
		hc:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Addr is the loopback host:port this client talks to.
func (c *Client) Addr() string { return c.addr }

// Token reads the bearer token from the per-user config directory.
func Token() (string, error) {
	path, err := tokenPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNoToken
		}
		return "", fmt.Errorf("read token: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", ErrNoToken
	}
	return tok, nil
}

func tokenPath() (string, error) {
	if dir := os.Getenv("DM_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "token"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(base, "dm", "token"), nil
}

// APIError is a non-2xx response from the daemon. Message is the daemon's
// human-readable reason when it sent one.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("daemon returned HTTP %d", e.Status)
	}
	return e.Message
}

// Settings mirrors GET /api/settings. Only the fields the desktop client uses
// are modelled; unknown JSON fields are ignored.
type Settings struct {
	Dir string `json:"dir"`
}

// AddRequest is a new download. Secrets (headers, cookies, referrer) are not
// exposed by this client: the desktop UI never captures them.
type AddRequest struct {
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
	Dir      string `json:"dir,omitempty"`
}

// Downloaded returns the bytes written across all segments. Single-stream
// downloads with an unknown size report no segments, so the result stays 0.
func Downloaded(d model.Download) int64 {
	var n int64
	for _, seg := range d.Segments {
		n += seg.Downloaded
	}
	return n
}

// List returns every download the daemon knows about.
func (c *Client) List(ctx context.Context) ([]model.Download, error) {
	var out []model.Download
	if err := c.do(ctx, http.MethodGet, "/api/downloads", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Download{}
	}
	return out, nil
}

// Categories returns the user's categories.
func (c *Client) Categories(ctx context.Context) ([]model.Category, error) {
	var out []model.Category
	if err := c.do(ctx, http.MethodGet, "/api/categories", nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Category{}
	}
	return out, nil
}

// Settings returns the daemon's current settings.
func (c *Client) Settings(ctx context.Context) (Settings, error) {
	var out Settings
	if err := c.do(ctx, http.MethodGet, "/api/settings", nil, &out); err != nil {
		return Settings{}, err
	}
	return out, nil
}

// Add submits a new download and returns the accepted record.
func (c *Client) Add(ctx context.Context, req AddRequest) (model.Download, error) {
	var out model.Download
	if err := c.do(ctx, http.MethodPost, "/api/downloads", req, &out); err != nil {
		return model.Download{}, err
	}
	return out, nil
}

// Pause asks the daemon to stop an active download, keeping partial data.
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/downloads/"+url.PathEscape(id)+"/pause", nil, nil)
}

// Resume restarts a paused download.
func (c *Client) Resume(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/downloads/"+url.PathEscape(id)+"/resume", nil, nil)
}

// Cancel stops a download and marks it canceled. It does not remove it.
func (c *Client) Cancel(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/downloads/"+url.PathEscape(id)+"/cancel", nil, nil)
}

// Remove deletes the download record, and its file when deleteFile is true.
func (c *Client) Remove(ctx context.Context, id string, deleteFile bool) error {
	path := "/api/downloads/" + url.PathEscape(id)
	if deleteFile {
		path += "?deleteFile=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// do performs one authenticated request and decodes the JSON response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("daemon unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(resp)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		return &APIError{Status: resp.StatusCode, Message: payload.Error}
	}
	return &APIError{Status: resp.StatusCode}
}
