package nativehost

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
	"os/exec"
	"strings"
	"time"

	"github.com/vishwa/dm/internal/api"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/runtime"
	"github.com/vishwa/dm/internal/uihandoff"
)

// Request is one message from the browser extension. "open" asks the desktop
// app to show a review dialog; "add" remains available to trusted CLI clients
// that intentionally want to queue a download immediately.
type Request struct {
	// Command is "open" (show the desktop review dialog), "add" (queue directly), or "ping".
	Command string `json:"command"`

	URL        string            `json:"url,omitempty"`
	Filename   string            `json:"filename,omitempty"`
	Dir        string            `json:"dir,omitempty"`
	CategoryID string            `json:"categoryId,omitempty"`
	Referrer   string            `json:"referrer,omitempty"`
	MIME       string            `json:"mime,omitempty"`
	TotalBytes int64             `json:"totalBytes,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Cookies    []model.Cookie    `json:"cookies,omitempty"`
}

// Response is one message back to the extension.
type Response struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
	Version  string `json:"version,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Host forwards requests to the daemon. Endpoint, Token and HTTPClient are
// injectable so the protocol and the request shape can be tested without a
// live daemon.
type Host struct {
	Version    string
	Endpoint   func() (runtime.Endpoint, error)
	Token      func() (string, error)
	HTTPClient *http.Client
	OpenUI     func(context.Context, uihandoff.Request) error
}

// New returns a Host wired to the real daemon discovery and token file.
func New(version string) *Host {
	return &Host{
		Version: version,
		Endpoint: func() (runtime.Endpoint, error) {
			return runtime.Read()
		},
		Token: func() (string, error) {
			path, err := api.TokenFile()
			if err != nil {
				return "", err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			token := strings.TrimSpace(string(raw))
			if token == "" {
				return "", fmt.Errorf("token file %s is empty", path)
			}
			return token, nil
		},
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		OpenUI:     openDesktopReview,
	}
}

// Handle answers one request. It never panics and always returns a response,
// so a malformed message cannot leave the browser waiting.
func (h *Host) Handle(ctx context.Context, req Request) Response {
	switch req.Command {
	case "", "add":
		return h.add(ctx, req)
	case "open":
		return h.open(ctx, req)
	case "ping":
		return h.ping(ctx)
	default:
		return failure("unknown command " + strconvQuote(req.Command))
	}
}

func (h *Host) open(ctx context.Context, req Request) Response {
	if err := validate(req); err != nil {
		return failure(err.Error())
	}
	if _, err := h.Endpoint(); err != nil {
		return failure("The dm daemon is not running. Start it with: systemctl --user start dm")
	}
	if _, err := h.Token(); err != nil {
		return failure("The dm bearer token is missing. Start the daemon once so it can create it.")
	}
	if h.OpenUI == nil {
		return failure("The DM desktop app cannot be opened.")
	}

	browserRequest := uihandoff.Request{
		URL:        strings.TrimSpace(req.URL),
		Filename:   strings.TrimSpace(req.Filename),
		Referrer:   strings.TrimSpace(req.Referrer),
		MIME:       strings.TrimSpace(req.MIME),
		TotalBytes: req.TotalBytes,
		Headers:    cleanHeaders(req.Headers),
		Cookies:    cleanCookies(req.Cookies),
	}
	if err := uihandoff.Validate(browserRequest); err != nil {
		return failure(err.Error())
	}
	if err := h.OpenUI(ctx, browserRequest); err != nil {
		return failure("Could not open the DM download review dialog: " + err.Error())
	}
	return Response{OK: true, Version: h.Version}
}

// openDesktopReview sends the request to the running desktop app. If the UI
// isn't running, start the packaged executable and retry the local socket. The
// payload is sent over a 0600 Unix socket, never in command-line arguments.
func openDesktopReview(ctx context.Context, req uihandoff.Request) error {
	err := uihandoff.Send(ctx, req)
	if err == nil {
		return nil
	}
	var rejected *uihandoff.ResponseError
	if errors.As(err, &rejected) {
		return err
	}

	executable, err := exec.LookPath("dm-desktop")
	if err != nil {
		return fmt.Errorf("dm-desktop was not found in PATH: %w", err)
	}
	cmd := exec.Command(executable)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start dm-desktop: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		sendCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		err := uihandoff.Send(sendCtx, req)
		cancel()
		if err == nil {
			return nil
		}
		if errors.As(err, &rejected) {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("desktop app did not become ready: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func (h *Host) ping(ctx context.Context) Response {
	if _, err := h.Endpoint(); err != nil {
		return failure("The dm daemon is not running. Start it with: systemctl --user start dm")
	}
	if _, err := h.Token(); err != nil {
		return failure("The dm bearer token is missing. Start the daemon once so it can create it.")
	}
	return Response{OK: true, Version: h.Version}
}

func (h *Host) add(ctx context.Context, req Request) Response {
	if err := validate(req); err != nil {
		return failure(err.Error())
	}

	endpoint, err := h.Endpoint()
	if err != nil {
		return failure("The dm daemon is not running. Start it with: systemctl --user start dm")
	}
	token, err := h.Token()
	if err != nil {
		return failure("The dm bearer token is missing. Start the daemon once so it can create it.")
	}

	body, err := json.Marshal(struct {
		URL        string            `json:"url"`
		Filename   string            `json:"filename,omitempty"`
		Dir        string            `json:"dir,omitempty"`
		CategoryID string            `json:"categoryId,omitempty"`
		Referrer   string            `json:"referrer,omitempty"`
		Headers    map[string]string `json:"headers,omitempty"`
		Cookies    []model.Cookie    `json:"cookies,omitempty"`
	}{
		URL:        req.URL,
		Filename:   strings.TrimSpace(req.Filename),
		Dir:        strings.TrimSpace(req.Dir),
		CategoryID: strings.TrimSpace(req.CategoryID),
		Referrer:   strings.TrimSpace(req.Referrer),
		Headers:    cleanHeaders(req.Headers),
		Cookies:    cleanCookies(req.Cookies),
	})
	if err != nil {
		return failure("Could not prepare the request.")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+endpoint.Addr+"/api/downloads", bytes.NewReader(body))
	if err != nil {
		return failure("Could not prepare the request.")
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := h.HTTPClient.Do(httpReq)
	if err != nil {
		return failure("Could not reach the dm daemon.")
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return failure("The dm daemon rejected the download: " + daemonError(raw, resp.StatusCode))
	}

	var created struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return failure("The dm daemon sent an unreadable response.")
	}
	return Response{OK: true, ID: created.ID, Filename: created.Filename}
}

// daemonError extracts the API's error message, falling back to the status.
func daemonError(raw []byte, status int) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.Error != "" {
		return body.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}

// validate accepts only what the daemon can fetch, and only remote http(s):
// a browser extension must not be able to make the daemon read local files.
func validate(req Request) error {
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return fmt.Errorf("The download address is not a valid URL.")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Only http:// and https:// downloads are supported.")
	}
	if u.Host == "" {
		return fmt.Errorf("The download address is missing a host.")
	}
	return nil
}

// cleanHeaders drops names and values that cannot be sent on a request line.
func cleanHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || strings.ContainsAny(name+value, "\r\n") {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanCookies(in []model.Cookie) []model.Cookie {
	out := make([]model.Cookie, 0, len(in))
	for _, c := range in {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		out = append(out, model.Cookie{Name: name, Value: c.Value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func failure(message string) Response {
	return Response{OK: false, Error: message}
}

// strconvQuote keeps quotes out of hand-built messages without importing
// strconv for one call.
func strconvQuote(s string) string {
	return fmt.Sprintf("%q", s)
}
