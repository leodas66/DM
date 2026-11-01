package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rangeHandler serves body with Range support and reports whether a HEAD
// request is allowed.
type rangeHandler struct {
	body     []byte
	headCode int // 0 means 200
	noRange  bool
	redirect string
	requests int
}

func (h *rangeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.requests++
	if h.redirect != "" {
		http.Redirect(w, r, h.redirect, http.StatusFound)
		return
	}
	if r.Method == http.MethodHead {
		if h.headCode != 0 {
			w.WriteHeader(h.headCode)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(h.body)))
		if !h.noRange {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		return
	}

	start, end, hasRange := parseRange(r.Header.Get("Range"), int64(len(h.body)))
	if hasRange && !h.noRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(h.body)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(h.body[start : end+1])
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(h.body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.body)
}

func parseRange(s string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(s, "bytes=") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(s, "bytes="), "-", 2)
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end := size - 1
	if len(parts) == 2 && parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func TestProbeReadsMetadataFromRangeResponse(t *testing.T) {
	body := make([]byte, 4096)
	h := &rangeHandler{body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''rates%20%E2%82%AC.txt`)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.Size != 4096 {
		t.Errorf("Size = %d, want 4096", got.Size)
	}
	if !got.AcceptsRanges {
		t.Error("AcceptsRanges = false, want true")
	}
	if got.Filename != "rates €.txt" {
		t.Errorf("Filename = %q, want %q", got.Filename, "rates €.txt")
	}
	if got.ETag != `"v1"` {
		t.Errorf("ETag = %q", got.ETag)
	}
	if got.LastModified == "" {
		t.Error("LastModified empty")
	}
}

func TestProbeFallsBackWhenHeadRejected(t *testing.T) {
	h := &rangeHandler{body: make([]byte, 5000), headCode: http.StatusMethodNotAllowed}
	srv := httptest.NewServer(h)
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL+"/f", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.Size != 5000 || !got.AcceptsRanges {
		t.Errorf("got Size=%d AcceptsRanges=%v, want 5000/true", got.Size, got.AcceptsRanges)
	}
}

func TestProbeDetectsMissingRangeSupport(t *testing.T) {
	body := make([]byte, 1000)
	body[0] = 1
	h := &rangeHandler{body: body, noRange: true}
	srv := httptest.NewServer(h)
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL+"/f", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.AcceptsRanges {
		t.Error("AcceptsRanges = true, want false")
	}
	if got.Size != 1000 {
		t.Errorf("Size = %d, want 1000 (from Content-Length)", got.Size)
	}
}

func TestProbeUnknownLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("streaming"))
		}
	}))
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.Size != 0 {
		t.Errorf("Size = %d, want 0 for unknown length", got.Size)
	}
	if got.AcceptsRanges {
		t.Error("AcceptsRanges = true, want false")
	}
}

func TestProbeFollowsRedirectsAndRecordsFinalURL(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, srv.URL+"/mid", http.StatusFound)
		case "/mid":
			http.Redirect(w, r, srv.URL+"/files/report%20final.pdf", http.StatusMovedPermanently)
		default:
			h := &rangeHandler{body: make([]byte, 10)}
			h.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL+"/start", nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !strings.HasSuffix(got.FinalURL, "/files/report%20final.pdf") {
		t.Errorf("FinalURL = %q", got.FinalURL)
	}
	if got.Filename != "report final.pdf" {
		t.Errorf("Filename = %q, want %q", got.Filename, "report final.pdf")
	}
}

func TestProbeRefusesHTTPSDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer plain.Close()

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
	}))
	defer tlsSrv.Close()

	c := NewClient()
	c.Transport = tlsSrv.Client().Transport

	_, err := Probe(context.Background(), c, tlsSrv.URL+"/start", nil)
	if !errors.Is(err, ErrInsecureRedirect) {
		t.Fatalf("err = %v, want ErrInsecureRedirect", err)
	}
}

func TestProbeRejectsNonHTTPSchemes(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/f", "file:///etc/passwd", "javascript:alert(1)"} {
		_, err := Probe(context.Background(), NewClient(), raw, nil)
		if !errors.Is(err, ErrInvalidScheme) {
			t.Errorf("Probe(%q) err = %v, want ErrInvalidScheme", raw, err)
		}
	}
}

func TestProbeReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Probe(context.Background(), NewClient(), srv.URL+"/missing", nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want StatusError 404", err)
	}
}

func TestProbeRespectsContext(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Probe(ctx, NewClient(), srv.URL+"/slow", nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline", err)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\file.txt`, "file.txt"},
		{"..", "download"},
		{"", "download"},
		{"a/../../b", "b"},
		{"trailing.  ", "trailing"},
		{"con.txt", "_con.txt"},
		{"CON", "_CON"},
		{"bad\x00name\x1b.txt", "badname.txt"},
		{"  spaced.txt", "spaced.txt"},
	}
	for _, c := range cases {
		if got := SanitizeFilename(c.in); got != c.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeFilenamePreservesExtensionWhenTruncating(t *testing.T) {
	long := strings.Repeat("é", 200) + ".mkv"
	got := SanitizeFilename(long)
	if len(got) > 255 {
		t.Fatalf("len = %d, want <= 255 bytes", len(got))
	}
	if !strings.HasSuffix(got, ".mkv") {
		t.Fatalf("lost extension: %q", got)
	}
}
