package download

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

// rangeServer serves body with Range support, optionally severing the first
// few GET responses mid-body.
type rangeServer struct {
	body       []byte
	noRange    bool
	failFirst  int
	cut        int
	mu         sync.Mutex
	gets       int
	ranges     []string
	status     int // when non-zero, answer the first GET with this status
	retryAfter string
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", `"v1"`)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		if !s.noRange {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		return
	}

	s.mu.Lock()
	s.gets++
	attempt := s.gets
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mu.Unlock()

	if s.status != 0 && attempt == 1 {
		w.Header().Set("Retry-After", s.retryAfter)
		w.WriteHeader(s.status)
		return
	}

	start, end, hasRange := parseTestRange(r.Header.Get("Range"), int64(len(s.body)))
	if !hasRange || s.noRange {
		start, end = 0, int64(len(s.body))-1
		w.Header().Set("Content-Length", strconv.FormatInt(end+1, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.body)
		return
	}

	chunk := s.body[start : end+1]
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.body)))
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(chunk)), 10))
	w.WriteHeader(http.StatusPartialContent)

	if attempt <= s.failFirst && s.cut < len(chunk) {
		_, _ = w.Write(chunk[:s.cut])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	_, _ = w.Write(chunk)
}

func parseTestRange(v string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(v, "bytes=") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(v, "bytes="), "-", 2)
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end := size - 1
	if len(parts) == 2 && parts[1] != "" {
		if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil && v < end {
			end = v
		}
	}
	return start, end, true
}

func testBody(n int) []byte {
	b := make([]byte, n)
	r := rand.New(rand.NewSource(42))
	_, _ = r.Read(b)
	return b
}

func writeFileAndHash(t *testing.T, body []byte) (string, [32]byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.bin")
	return path, sha256.Sum256(body)
}

func drain(t *testing.T, ch chan Event) *[]Event {
	t.Helper()
	var mu sync.Mutex
	var got []Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		close(ch)
		<-done
	})
	return &got
}

func TestRunSegmentedDownload(t *testing.T) {
	body := testBody(4 << 20)
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()

	dest, want := writeFileAndHash(t, body)
	events := make(chan Event, 256)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true, ETag: `"v1"`}
	err := Run(context.Background(), cfg, dest, Options{Segments: 4, MinSegmentSize: 1 << 20}, events)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if sha256.Sum256(got) != want {
		t.Fatal("checksum mismatch")
	}
	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".part still present after success: %v", err)
	}
}

func TestRunSingleStreamWithoutRangeSupport(t *testing.T) {
	body := testBody(3 << 20)
	srv := httptest.NewServer(&rangeServer{body: body, noRange: true})
	defer srv.Close()

	dest, want := writeFileAndHash(t, body)
	events := make(chan Event, 256)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: false}
	if err := Run(context.Background(), cfg, dest, Options{}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if sha256.Sum256(got) != want {
		t.Fatal("checksum mismatch")
	}
}

func TestRunUnknownLength(t *testing.T) {
	body := testBody(1 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dest, want := writeFileAndHash(t, body)
	events := make(chan Event, 256)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: 0, AcceptsRanges: false}
	if err := Run(context.Background(), cfg, dest, Options{}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if sha256.Sum256(got) != want {
		t.Fatal("checksum mismatch")
	}
}

func TestRunRetriesSeveredResponses(t *testing.T) {
	body := testBody(2 << 20)
	srv := httptest.NewServer(&rangeServer{body: body, failFirst: 3, cut: 64 << 10})
	defer srv.Close()

	dest, want := writeFileAndHash(t, body)
	events := make(chan Event, 256)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	opts := Options{Segments: 2, MinSegmentSize: 1 << 20, BackoffBase: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond}
	if err := Run(context.Background(), cfg, dest, opts, events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if sha256.Sum256(got) != want {
		t.Fatal("checksum mismatch after retries")
	}
}

func TestRunHonorsRetryAfter(t *testing.T) {
	body := testBody(256 << 10)
	srv := httptest.NewServer(&rangeServer{body: body, status: http.StatusTooManyRequests, retryAfter: "0"})
	defer srv.Close()

	dest, want := writeFileAndHash(t, body)
	events := make(chan Event, 64)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 1}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if sha256.Sum256(got) != want {
		t.Fatal("checksum mismatch")
	}
}

func TestRunFailsFastOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.bin")
	events := make(chan Event, 64)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: 100, AcceptsRanges: true}
	start := time.Now()
	err := Run(context.Background(), cfg, dest, Options{MaxTries: 3, BackoffBase: time.Second}, events)
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("404 was retried for %v, want immediate failure", time.Since(start))
	}
}

func TestRunDetectsRemoteSizeChange(t *testing.T) {
	body := testBody(256 << 10)
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.bin")
	events := make(chan Event, 64)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)) + 4096, AcceptsRanges: true}
	err := Run(context.Background(), cfg, dest, Options{Segments: 1}, events)
	if !errors.Is(err, ErrSizeChanged) {
		t.Fatalf("err = %v, want ErrSizeChanged", err)
	}
}

func TestRunStopsOnCancelAndKeepsPartFile(t *testing.T) {
	body := testBody(4 << 20)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end, _ := parseTestRange(r.Header.Get("Range"), int64(len(body)))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[start : start+16<<10])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	dest := filepath.Join(t.TempDir(), "x.bin")
	events := make(chan Event, 64)
	drain(t, events)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	err := Run(ctx, cfg, dest, Options{Segments: 2, MinSegmentSize: 1 << 20}, events)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	fi, statErr := os.Stat(dest + ".part")
	if statErr != nil {
		t.Fatalf(".part missing after cancel: %v", statErr)
	}
	if fi.Size() != int64(len(body)) {
		t.Errorf(".part size = %d, want preallocated %d", fi.Size(), len(body))
	}
}

func TestPlanSegmentsCoversFileWithoutGaps(t *testing.T) {
	cases := []struct {
		size, min int64
		segments  int
		want      int
	}{
		{0, 1 << 20, 8, 0},
		{512 << 10, 1 << 20, 8, 1},
		{3<<20 + 512<<10, 1 << 20, 8, 3},
		{4 << 20, 1 << 20, 4, 4},
		{8 << 20, 1 << 20, 8, 8},
	}
	for _, c := range cases {
		got := planSegments(c.size, Options{Segments: c.segments, MinSegmentSize: c.min})
		if len(got) != c.want {
			t.Errorf("planSegments(%d, %d segs) = %d segments, want %d", c.size, c.segments, len(got), c.want)
			continue
		}
		var next int64
		for i, s := range got {
			if s.Index != i {
				t.Errorf("segment %d has index %d", i, s.Index)
			}
			if s.Start != next {
				t.Errorf("segment %d starts at %d, want %d", i, s.Start, next)
			}
			if s.End < s.Start {
				t.Errorf("segment %d has End %d < Start %d", i, s.End, s.Start)
			}
			next = s.End + 1
		}
		if c.want > 0 && next != c.size {
			t.Errorf("segments cover up to %d, want %d", next, c.size)
		}
	}
}

func TestBackoffStaysWithinBounds(t *testing.T) {
	base, max := time.Second, 10*time.Second
	for attempt := 1; attempt <= 8; attempt++ {
		for i := 0; i < 20; i++ {
			d := backoffDelay(rand.New(rand.NewSource(int64(i))), attempt, base, max)
			if d < 0 || d > max {
				t.Fatalf("attempt %d: delay %v outside [0, %v]", attempt, d, max)
			}
		}
	}
}

func TestRetryAfterParsing(t *testing.T) {
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set("Retry-After", "2")
	if d, ok := retryAfter(resp); !ok || d != 2*time.Second {
		t.Fatalf("Retry-After: 2 -> %v %v", d, ok)
	}
	resp.Header.Set("Retry-After", "999999")
	if d, ok := retryAfter(resp); !ok || d > 60*time.Second {
		t.Fatalf("Retry-After cap -> %v %v", d, ok)
	}
	resp.Header.Set("Retry-After", "Wed, 21 Oct 2015 07:28:00 GMT")
	if _, ok := retryAfter(resp); ok {
		t.Fatal("past HTTP-date should not produce a delay")
	}
}

func TestRunRejectsUnsupportedRangeMidDownload(t *testing.T) {
	body := testBody(2 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("Accept-Ranges", "bytes")
			return
		}
		// Claim range support during probe, then ignore ranges.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.bin")
	events := make(chan Event, 64)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	err := Run(context.Background(), cfg, dest, Options{Segments: 4}, events)
	if err == nil {
		t.Fatal("expected error when a ranged request gets 200")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}

func TestEventStreamReportsProgressAndCompletion(t *testing.T) {
	body := testBody(1 << 20)
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "x.bin")
	events := make(chan Event, 512)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 2}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Run has returned, so every event is already buffered; drain without a
	// goroutine to avoid inspecting a slice the consumer is still writing.
	var got []Event
	for {
		select {
		case ev := <-events:
			got = append(got, ev)
		default:
			if len(got) == 0 {
				t.Fatal("no events")
			}
			last := got[len(got)-1]
			sawProgress := false
			for _, ev := range got {
				if ev.Type == EventProgress && ev.Downloaded > 0 {
					sawProgress = true
				}
			}
			if !sawProgress {
				t.Error("no progress events with bytes")
			}
			if last.Type != EventDone || last.Downloaded != last.Total {
				t.Errorf("last event = %+v, want done with Downloaded == Total", last)
			}
			return
		}
	}
}

func TestPartFileIsRenamedNotCopied(t *testing.T) {
	body := testBody(64 << 10)
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "final.bin")
	events := make(chan Event, 32)
	drain(t, events)

	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 1}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "final.bin" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("dir contains %v, want only final.bin", names)
	}
}

// partialFile writes body[:n] at its real offsets into dest+".part" and
// preallocates the file to len(body), the shape a paused run leaves behind.
func partialFile(t *testing.T, dest string, body []byte, n int64) {
	t.Helper()
	f, err := os.OpenFile(dest+".part", os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("create .part: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteAt(body[:n], 0); err != nil {
		t.Fatalf("write .part: %v", err)
	}
	if err := f.Truncate(int64(len(body))); err != nil {
		t.Fatalf("truncate .part: %v", err)
	}
}

func (s *rangeServer) seenRanges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func TestRunResumesFromSavedSegments(t *testing.T) {
	body := testBody(4 << 20)
	rs := &rangeServer{body: body}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	const already = 1 << 20
	partialFile(t, dest, body, already)

	events := make(chan Event, 64)
	cfg := Config{
		URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true, ETag: `"v1"`,
		Resume: []model.Segment{{
			Index: 0, Start: 0, End: int64(len(body)) - 1,
			Downloaded: already, Status: model.SegmentRunning,
		}},
	}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 4}, events); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(body) {
		t.Fatal("checksum mismatch after resume")
	}
	ranges := rs.seenRanges()
	if len(ranges) != 1 {
		t.Fatalf("ranges = %v, want one resumed request", ranges)
	}
	if want := fmt.Sprintf("bytes=%d-", already); !strings.HasPrefix(ranges[0], want) {
		t.Fatalf("resumed with %q, want the range to start at %q", ranges[0], want)
	}
}

func TestRunSkipsCompletedSegments(t *testing.T) {
	body := testBody(2 << 20)
	rs := &rangeServer{body: body}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	// The whole file is already on disk, split into two finished segments.
	partialFile(t, dest, body, int64(len(body)))
	half := int64(len(body)) / 2
	cfg := Config{
		URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true, ETag: `"v1"`,
		Resume: []model.Segment{
			{Index: 0, Start: 0, End: half - 1, Downloaded: half, Status: model.SegmentDone},
			{Index: 1, Start: half, End: int64(len(body)) - 1, Downloaded: int64(len(body)) - half, Status: model.SegmentDone},
		},
	}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 4}, eventsSink()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ranges := rs.seenRanges(); len(ranges) != 0 {
		t.Fatalf("fetched %v for a complete file, want no requests", ranges)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(body) {
		t.Fatal("checksum mismatch")
	}
}

func TestRunRestartsWhenResumeIsInvalid(t *testing.T) {
	body := testBody(4 << 20)
	rs := &rangeServer{body: body}
	srv := httptest.NewServer(rs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	// A stale .part with junk in it must not survive an invalid resume table.
	if err := os.WriteFile(dest+".part", make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true, ETag: `"v1"`,
		// The second segment starts past the end of the first: a gap.
		Resume: []model.Segment{
			{Index: 0, Start: 0, End: (1 << 20) - 1},
			{Index: 1, Start: 2 << 20, End: int64(len(body)) - 1},
		},
	}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 4}, eventsSink()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(body) {
		t.Fatal("checksum mismatch: invalid resume table was trusted")
	}
	ranges := rs.seenRanges()
	found := false
	for _, r := range ranges {
		if strings.HasPrefix(r, "bytes=0-") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ranges = %v, want a full download from zero (bytes=0-)", ranges)
	}
}

// countingLimiter records what the downloader asks it to allow.
type countingLimiter struct {
	mu    sync.Mutex
	calls int
	bytes int
}

func (l *countingLimiter) WaitN(_ context.Context, n int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.bytes += n
	return nil
}

func TestRunConsultsLimiterForEveryRead(t *testing.T) {
	body := testBody(256 << 10)
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()

	lim := &countingLimiter{}
	dest := filepath.Join(t.TempDir(), "out.bin")
	cfg := Config{URL: srv.URL, Size: int64(len(body)), AcceptsRanges: true}
	if err := Run(context.Background(), cfg, dest, Options{Segments: 1, Limiter: lim}, eventsSink()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	lim.mu.Lock()
	defer lim.mu.Unlock()
	if lim.calls == 0 {
		t.Fatal("limiter was never called")
	}
	if lim.bytes != len(body) {
		t.Fatalf("limiter saw %d bytes, want %d (one per downloaded byte)", lim.bytes, len(body))
	}
}

// eventsSink returns a large buffered channel nobody reads, so emit never
// blocks the download.
func eventsSink() chan Event { return make(chan Event, 4096) }
