package probe

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sentinel errors returned by Probe.
var (
	// ErrInvalidScheme means the URL was not http or https.
	ErrInvalidScheme = errors.New("probe: only http and https URLs are supported")
	// ErrInsecureRedirect means a redirect tried to downgrade https to http.
	ErrInsecureRedirect = errors.New("probe: refusing redirect from https to http")
	// ErrTooManyRedirects means the redirect chain exceeded maxRedirects.
	ErrTooManyRedirects = errors.New("probe: too many redirects")
)

// maxRedirects matches aria2's cap; Go's default is 10.
const maxRedirects = 20

// StatusError reports a non-2xx response that Probe could not work around.
type StatusError struct {
	Code int
	URL  string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("probe: %s returned HTTP %d", e.URL, e.Code)
}

// Info is the outcome of probing a resource.
type Info struct {
	// URL is what the caller asked for.
	URL string
	// FinalURL is the URL after following redirects.
	FinalURL string
	// Size is the total length in bytes, or 0 when the server did not say.
	Size int64
	// AcceptsRanges reports whether a Range request was answered with 206.
	AcceptsRanges bool
	// ETag and LastModified are the validators seen while probing.
	ETag         string
	LastModified string
	// Filename is the sanitized base name to save the file under.
	Filename string
	// ContentType is the Content-Type header of the final response.
	ContentType string

	// contentDisposition is kept from HEAD so a GET that omits it still
	// yields the server's preferred name.
	contentDisposition string
}

// NewClient returns the HTTP client used for probing and downloading: it
// follows at most maxRedirects hops, refuses https-to-http downgrades, and
// strips credentials when a redirect crosses origins.
func NewClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return ErrTooManyRedirects
			}
			prev := via[len(via)-1]
			if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
				return ErrInsecureRedirect
			}
			if !sameOrigin(prev.URL, req.URL) {
				req.Header.Del("Authorization")
				req.Header.Del("Cookie")
				if ref := req.Header.Get("Referer"); ref != "" {
					wantRef := prev.URL.Scheme + "://" + prev.URL.Host + "/"
					if !strings.HasPrefix(ref, wantRef) {
						req.Header.Set("Referer", wantRef)
					}
				}
			}
			return nil
		},
	}
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host
}

// Probe discovers how to download rawURL: length, range support, validators,
// redirect target and a safe filename. It issues a HEAD request and falls back
// to a one-byte ranged GET, which is also what decides AcceptsRanges.
func Probe(ctx context.Context, c *http.Client, rawURL string, headers map[string]string) (*Info, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, ErrInvalidScheme
	}

	info := &Info{URL: rawURL, FinalURL: rawURL}

	// HEAD first: cheap, and some servers expose validators only there.
	head, headErr := request(ctx, c, http.MethodHead, rawURL, nil, headers)
	if headErr == nil {
		defer head.Body.Close()
		switch {
		case head.StatusCode >= 200 && head.StatusCode < 300:
			copyMeta(info, head)
			if n, convErr := strconv.ParseInt(head.Header.Get("Content-Length"), 10, 64); convErr == nil && n > 0 {
				info.Size = n
			}
		case head.StatusCode == http.StatusNotFound || head.StatusCode == http.StatusGone:
			return nil, &StatusError{Code: head.StatusCode, URL: rawURL}
		}
	}

	// Range probe is authoritative for range support and final length.
	get, getErr := request(ctx, c, http.MethodGet, rawURL, map[string]string{"Range": "bytes=0-0"}, headers)
	if getErr != nil {
		if headErr != nil {
			return nil, getErr
		}
		// HEAD worked; treat as a non-range server.
		info.AcceptsRanges = false
		if info.Size > 0 {
			info.Filename = filenameFor(info, rawURL, "")
		}
		return info, nil
	}
	defer get.Body.Close()

	info.FinalURL = get.Request.URL.String()
	copyMeta(info, get)

	switch {
	case get.StatusCode == http.StatusPartialContent:
		info.AcceptsRanges = true
		if total, ok := totalFromContentRange(get.Header.Get("Content-Range")); ok {
			info.Size = total
		}
	case get.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// Empty resource, or a range past the end; the total is still useful.
		if total, ok := totalFromContentRange(get.Header.Get("Content-Range")); ok {
			info.Size = total
			info.AcceptsRanges = total > 1
		}
	case get.StatusCode >= 200 && get.StatusCode < 300:
		// The server ignored Range and is sending the whole body.
		info.AcceptsRanges = false
		if n, convErr := strconv.ParseInt(get.Header.Get("Content-Length"), 10, 64); convErr == nil && n > 0 {
			info.Size = n
		} else if info.Size == 0 {
			info.Size = 0 // unknown length: single stream, no preallocation
		}
	case get.StatusCode == http.StatusNotFound || get.StatusCode == http.StatusGone:
		return nil, &StatusError{Code: get.StatusCode, URL: rawURL}
	default:
		if headErr != nil || info.Size == 0 {
			return nil, &StatusError{Code: get.StatusCode, URL: rawURL}
		}
		// HEAD gave us enough; Range is simply unsupported here.
		info.AcceptsRanges = false
	}

	if info.Size == 0 {
		info.AcceptsRanges = false
	}
	info.Filename = filenameFor(info, rawURL, get.Header.Get("Content-Disposition"))
	return info, nil
}

func request(ctx context.Context, c *http.Client, method, rawURL string, extra, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return c.Do(req)
}

func copyMeta(info *Info, resp *http.Response) {
	if et := resp.Header.Get("ETag"); et != "" {
		info.ETag = et
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		info.LastModified = lm
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		info.ContentType = ct
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		info.contentDisposition = cd
	}
}

// totalFromContentRange parses "bytes 0-0/1234", returning 1234. The wildcard
// form "bytes */1234" is accepted too.
func totalFromContentRange(v string) (int64, bool) {
	if !strings.HasPrefix(v, "bytes ") {
		return 0, false
	}
	slash := strings.IndexByte(v, '/')
	if slash < 0 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(v[slash+1:]), 10, 64)
	if err != nil {
		return 0, false
	}
	return total, true
}

// filenameFor picks Content-Disposition first, then the URL path, and always
// returns a sanitized base name. disposition is passed explicitly for the GET
// response; the HEAD header is read from Info when present.
func filenameFor(info *Info, rawURL, disposition string) string {
	if name := parseDisposition(disposition); name != "" {
		return SanitizeFilename(name)
	}
	if name := parseDisposition(info.contentDisposition); name != "" {
		return SanitizeFilename(name)
	}
	if u, err := url.Parse(info.FinalURL); err == nil {
		if seg := u.EscapedPath(); seg != "" {
			base := seg[strings.LastIndex(seg, "/")+1:]
			if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
				return SanitizeFilename(decoded)
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if base := u.Path[strings.LastIndex(u.Path, "/")+1:]; base != "" {
			return SanitizeFilename(base)
		}
	}
	return "download"
}

func parseDisposition(v string) string {
	if v == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(v); err == nil {
		if name, ok := params["filename"]; ok && name != "" {
			return name
		}
	}
	return ""
}

// SanitizeFilename reduces arbitrary input to a safe base name: no path
// separators, no traversal, no control or bidi characters, no Windows reserved
// device names, at most 255 bytes, extension preserved.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f:
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	if isWindowsReserved(name) {
		name = "_" + name
	}
	return truncateUTF8(name, 255)
}

func isWindowsReserved(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 {
		prefix := strings.ToUpper(base[:3])
		if (prefix == "COM" || prefix == "LPT") && base[3] >= '1' && base[3] <= '9' {
			return true
		}
	}
	return false
}

// truncateUTF8 caps s at max bytes without splitting a rune, keeping the
// extension when there is one.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := ""
	if i := strings.LastIndexByte(s, '.'); i > 0 && len(s)-i <= 20 {
		ext = s[i:]
	}
	base := strings.TrimSuffix(s, ext)
	limit := max - len(ext)
	for len(base) > limit {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}
	return base + ext
}
