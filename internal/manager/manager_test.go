package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/store"
)

// rangeServer is a range-capable file server that can dribble its body out so
// tests can observe a download in flight.
type rangeServer struct {
	mu        sync.Mutex
	body      []byte
	etag      string
	chunk     int
	delay     time.Duration
	gets      int
	ranges    []string
	active    int
	maxActive int
}

func newRangeServer(body []byte) *rangeServer {
	return &rangeServer{body: body, etag: `"v1"`, chunk: 16 << 10}
}

func (s *rangeServer) setBody(body []byte, etag string) {
	s.mu.Lock()
	s.body, s.etag = body, etag
	s.mu.Unlock()
}

func (s *rangeServer) stats() (gets, maxActive int, ranges []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets, s.maxActive, append([]string(nil), s.ranges...)
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	body, etag := s.body, s.etag
	s.mu.Unlock()

	w.Header().Set("ETag", etag)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Accept-Ranges", "bytes")
		return
	}

	s.mu.Lock()
	s.gets++
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	start, end, ranged := parseRangeHeader(r.Header.Get("Range"), len(body))
	if ranged {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	}

	chunk := s.chunk
	if chunk <= 0 {
		chunk = len(body)
	}
	flusher, _ := w.(http.Flusher)
	for off := start; off <= end; off += int64(chunk) {
		hi := off + int64(chunk)
		if hi > end+1 {
			hi = end + 1
		}
		if _, err := w.Write(body[off:hi]); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if s.delay > 0 {
			select {
			case <-time.After(s.delay):
			case <-r.Context().Done():
				return
			}
		}
	}
}

func parseRangeHeader(v string, size int) (int64, int64, bool) {
	if !strings.HasPrefix(v, "bytes=") {
		return 0, int64(size) - 1, false
	}
	parts := strings.SplitN(strings.TrimPrefix(v, "bytes="), "-", 2)
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= int64(size) {
		return 0, int64(size) - 1, false
	}
	end := int64(size) - 1
	if len(parts) == 2 && parts[1] != "" {
		if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil && v < end {
			end = v
		}
	}
	return start, end, true
}

func testBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

type harness struct {
	m    *Manager
	st   *store.Store
	dir  string
	ctx  context.Context
	stop context.CancelFunc
}

func newHarness(t *testing.T, settings Settings) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dm.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	settings.Dir = filepath.Join(dir, "out")
	ctx, cancel := context.WithCancel(context.Background())
	m := New(st, settings, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h := &harness{m: m, st: st, dir: dir, ctx: ctx, stop: cancel}
	t.Cleanup(h.close)
	return h
}

func (h *harness) close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.m.Shutdown(shutdownCtx)
	_ = h.st.Close()
	if h.stop != nil {
		h.stop()
	}
}

// waitFor polls the manager until pred is true or timeout expires.
func waitFor(t *testing.T, m *Manager, id string, timeout time.Duration, what string, pred func(model.Download) bool) model.Download {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		d, err := m.Get(context.Background(), id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s: Get: %v", what, err)
		}
		if err == nil && pred(d) {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out; last state %s (%d/%d), err %q", what, d.Status, downloadedBytes(d), d.Size, d.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func downloadedBytes(d model.Download) int64 {
	var n int64
	for _, s := range d.Segments {
		n += s.Downloaded
	}
	return n
}

func waitCompleted(t *testing.T, m *Manager, id string, timeout time.Duration) model.Download {
	t.Helper()
	return waitFor(t, m, id, timeout, "complete", func(d model.Download) bool {
		return d.Status == model.StatusCompleted || d.Status == model.StatusFailed
	})
}

func wantChecksum(t *testing.T, path string, body []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if sha256.Sum256(got) != sha256.Sum256(body) {
		t.Fatalf("checksum mismatch for %s (%d bytes, want %d)", path, len(got), len(body))
	}
}

func TestAddCompletesAndPersists(t *testing.T) {
	body := testBytes(512 << 10)
	srv := httptest.NewServer(newRangeServer(body))
	defer srv.Close()

	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 2, SaveInterval: 10 * time.Millisecond})
	events, cancel := h.m.Events()
	defer cancel()

	d, err := h.m.Add(context.Background(), AddRequest{URL: srv.URL + "/file.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.Filename != "" {
		t.Fatalf("Filename = %q before probe, want empty", d.Filename)
	}

	done := waitCompleted(t, h.m, d.ID, 10*time.Second)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status = %s, err %q", done.Status, done.Error)
	}
	if done.Filename != "file.bin" || done.Size != int64(len(body)) || !done.AcceptsRanges {
		t.Fatalf("metadata = %+v", done)
	}
	wantChecksum(t, filepath.Join(done.Dir, done.Filename), body)

	// The store saw the completion, not just the in-memory loop.
	persisted, err := h.st.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if persisted.Status != model.StatusCompleted || persisted.Filename != "file.bin" {
		t.Fatalf("persisted = %+v", persisted)
	}

	sawAdded := false
	sawCompleted := false
	for len(events) > 0 {
		ev := <-events
		switch {
		case ev.Type == model.EventAdded:
			sawAdded = true
		case ev.Type == model.EventStatusChanged && ev.Download != nil && ev.Download.Status == model.StatusCompleted:
			sawCompleted = true
		}
	}
	if !sawAdded || !sawCompleted {
		t.Fatalf("events: added=%v completed=%v", sawAdded, sawCompleted)
	}
}

func TestMaxConcurrentIsEnforced(t *testing.T) {
	const n = 6
	const limit = 2
	const payload = 128 << 10
	idle := newRangeServer(testBytes(payload))
	idle.chunk = 16 << 10
	idle.delay = 12 * time.Millisecond
	srv := httptest.NewServer(idle)
	defer srv.Close()

	h := newHarness(t, Settings{MaxConcurrent: limit, Connections: 1})

	// Sample the Manager's own running count. Counting concurrent GETs on the
	// test server is not the same invariant: a finished handler can still be
	// unwinding when the Manager has already started the next download, which
	// shows up as a third open connection without three downloads running.
	stop := make(chan struct{})
	peakCh := make(chan int, 1)
	go func() {
		peak := 0
		for {
			select {
			case <-stop:
				peakCh <- peak
				return
			default:
			}
			list, err := h.m.List(context.Background())
			if err != nil {
				peakCh <- -1
				return
			}
			running := 0
			for _, d := range list {
				if d.Status == model.StatusRunning {
					running++
				}
			}
			if running > peak {
				peak = running
			}
			time.Sleep(time.Millisecond)
		}
	}()

	ids := make([]string, n)
	for i := range ids {
		d, err := h.m.Add(context.Background(), AddRequest{URL: fmt.Sprintf("%s/f%d.bin", srv.URL, i)})
		if err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
		ids[i] = d.ID
	}

	// Let the queue drain while the sampler watches.
	deadline := time.Now().Add(10 * time.Second)
	for _, id := range ids {
		if d := waitCompleted(t, h.m, id, time.Until(deadline)); d.Status != model.StatusCompleted {
			t.Fatalf("%s: status %s, err %q", id, d.Status, d.Error)
		}
	}

	close(stop)
	peak := <-peakCh
	if peak < 0 {
		t.Fatal("sampling manager state failed")
	}
	if peak > limit {
		t.Fatalf("manager ran %d downloads at once, limit %d", peak, limit)
	}
	if peak < limit {
		t.Fatalf("manager ran only %d downloads at once, want the limit to be used", peak)
	}

	// And the work really was parallel: the server saw both at once.
	if _, maxActive, _ := idle.stats(); maxActive < limit {
		t.Fatalf("server saw only %d concurrent GETs, want %d", maxActive, limit)
	}
}

func TestPauseResumesFromPartialFile(t *testing.T) {
	body := testBytes(2 << 20)
	slow := newRangeServer(body)
	slow.chunk = 32 << 10
	slow.delay = 8 * time.Millisecond
	srv := httptest.NewServer(slow)
	defer srv.Close()

	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 1, SaveInterval: 10 * time.Millisecond})

	d, err := h.m.Add(context.Background(), AddRequest{URL: srv.URL + "/slow.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, h.m, d.ID, 10*time.Second, "some progress", func(d model.Download) bool {
		return d.Status == model.StatusRunning && downloadedBytes(d) > 256<<10
	})

	if _, err := h.m.Pause(context.Background(), d.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	paused := waitFor(t, h.m, d.ID, 10*time.Second, "paused", func(d model.Download) bool {
		return d.Status == model.StatusPaused
	})
	if got := downloadedBytes(paused); got == 0 || got >= int64(len(body)) {
		t.Fatalf("paused at %d/%d bytes, want a partial download", got, len(body))
	}
	part := filepath.Join(paused.Dir, paused.Filename) + ".part"
	if _, err := os.Stat(part); err != nil {
		t.Fatalf(".part missing after pause: %v", err)
	}

	getsBefore, _, _ := slow.stats()
	if _, err := h.m.Resume(context.Background(), d.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	done := waitCompleted(t, h.m, d.ID, 20*time.Second)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status after resume = %s, err %q", done.Status, done.Error)
	}
	wantChecksum(t, filepath.Join(done.Dir, done.Filename), body)

	getsAfter, _, ranges := slow.stats()
	if getsAfter <= getsBefore {
		t.Fatalf("no new GET after resume (%d before, %d after)", getsBefore, getsAfter)
	}
	// The second run must ask for a byte range that starts past zero.
	resumed := false
	for _, r := range ranges[getsBefore:] {
		if r != "" && !strings.HasPrefix(r, "bytes=0-") {
			resumed = true
		}
	}
	if !resumed {
		t.Fatalf("resume re-downloaded from zero; ranges after resume: %v", ranges[getsBefore:])
	}
}

func TestCrashResumeProducesCorrectFile(t *testing.T) {
	body := testBytes(3 << 20)
	slow := newRangeServer(body)
	slow.chunk = 16 << 10
	slow.delay = 12 * time.Millisecond
	srv := httptest.NewServer(slow)
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "dm.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	outDir := filepath.Join(t.TempDir(), "out")

	settings := Settings{Dir: outDir, MaxConcurrent: 1, Connections: 1, SaveInterval: 10 * time.Millisecond}
	first := New(st, settings, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := first.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d, err := first.Add(context.Background(), AddRequest{URL: srv.URL + "/crash.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Wait until progress has reached the database, then die without a final
	// save: this is the kill -9 shape.
	deadline := time.Now().Add(10 * time.Second)
	for {
		saved, err := st.Get(context.Background(), d.ID)
		if err != nil {
			t.Fatalf("store.Get: %v", err)
		}
		if downloadedBytes(saved) > 64<<10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no persisted progress; last status %s", saved.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	first.stop()

	reloaded, err := st.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("store.Get after crash: %v", err)
	}
	if reloaded.Status == model.StatusCompleted {
		t.Fatal("download completed before the crash; test proves nothing")
	}
	if downloadedBytes(reloaded) == 0 {
		t.Fatal("no persisted segment progress after crash")
	}

	second := New(st, settings, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := second.Start(ctx2); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = second.Shutdown(sctx)
	}()

	done := waitCompleted(t, second, d.ID, 20*time.Second)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status after restart = %s, err %q", done.Status, done.Error)
	}
	wantChecksum(t, filepath.Join(done.Dir, done.Filename), body)

	// The restart asked for a byte range starting past zero, which can only
	// come from the persisted segment table.
	_, _, ranges := slow.stats()
	resumed := false
	for _, r := range ranges {
		if r != "" && !strings.HasPrefix(r, "bytes=0-") {
			resumed = true
		}
	}
	if !resumed {
		t.Fatalf("restart re-downloaded from zero; ranges: %v", ranges)
	}
}

func TestChangedValidatorRestartsFromZero(t *testing.T) {
	oldBody := testBytes(3 << 20)
	slow := newRangeServer(oldBody)
	slow.chunk = 16 << 10
	slow.delay = 12 * time.Millisecond
	srv := httptest.NewServer(slow)
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "dm.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	settings := Settings{Dir: filepath.Join(t.TempDir(), "out"), MaxConcurrent: 1, Connections: 1, SaveInterval: 10 * time.Millisecond}

	first := New(st, settings, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := first.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d, err := first.Add(context.Background(), AddRequest{URL: srv.URL + "/changing.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		saved, err := st.Get(context.Background(), d.ID)
		if err != nil {
			t.Fatalf("store.Get: %v", err)
		}
		if downloadedBytes(saved) > 64<<10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no persisted progress before crash")
		}
		time.Sleep(5 * time.Millisecond)
	}
	first.stop()

	newBody := testBytes(700 << 10)
	slow.setBody(newBody, `"v2"`)

	second := New(st, settings, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := second.Start(ctx2); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = second.Shutdown(sctx)
	}()

	done := waitCompleted(t, second, d.ID, 20*time.Second)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status = %s, err %q", done.Status, done.Error)
	}
	if done.ETag != `"v2"` || done.Size != int64(len(newBody)) {
		t.Fatalf("metadata not refreshed: etag %q size %d", done.ETag, done.Size)
	}
	wantChecksum(t, filepath.Join(done.Dir, done.Filename), newBody)
}

func TestGlobalSpeedLimitSlowsTransfer(t *testing.T) {
	body := testBytes(512 << 10)
	srv := httptest.NewServer(newRangeServer(body))
	defer srv.Close()

	const rate = 512 << 10 // bytes/second
	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 1, GlobalLimit: rate})

	d, err := h.m.Add(context.Background(), AddRequest{URL: srv.URL + "/limited.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	start := time.Now()
	done := waitCompleted(t, h.m, d.ID, 20*time.Second)
	elapsed := time.Since(start)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status = %s, err %q", done.Status, done.Error)
	}
	wantChecksum(t, filepath.Join(done.Dir, done.Filename), body)

	// The bucket starts with a quarter second of credit, so 512 KiB at
	// 512 KiB/s cannot finish faster than about 0.75 s.
	if elapsed < 500*time.Millisecond {
		t.Fatalf("download took %v at %d B/s; the limiter did not apply", elapsed, rate)
	}
}

func TestRemoveDeletesRecordAndFiles(t *testing.T) {
	body := testBytes(64 << 10)
	srv := httptest.NewServer(newRangeServer(body))
	defer srv.Close()

	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 1})
	d, err := h.m.Add(context.Background(), AddRequest{URL: srv.URL + "/gone.bin"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	done := waitCompleted(t, h.m, d.ID, 10*time.Second)
	path := filepath.Join(done.Dir, done.Filename)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("completed file missing: %v", err)
	}

	if err := h.m.Remove(context.Background(), d.ID, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := h.m.Get(context.Background(), d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after remove = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still present after remove: %v", err)
	}
	if list, err := h.m.List(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("List after remove = %v, %v", list, err)
	}
}

func TestAddRejectsNonHTTPURLs(t *testing.T) {
	h := newHarness(t, Settings{})
	for _, u := range []string{"ftp://example.com/f", "file:///etc/passwd", "not a url", ""} {
		if _, err := h.m.Add(context.Background(), AddRequest{URL: u}); err == nil {
			t.Errorf("Add(%q) succeeded, want error", u)
		}
	}
}

// TestKillNineChild is the helper process for TestKillNineAndResume. It is not
// a test of its own: the parent starts it, waits for persisted progress, then
// SIGKILLs it so the real crash path (WAL recovery, no final save) is exercised.
func TestKillNineChild(t *testing.T) {
	if os.Getenv("DM_CRASH_CHILD") != "1" {
		t.Skip("helper process; started by TestKillNineAndResume")
	}
	st, err := store.Open(os.Getenv("DM_STORE"))
	if err != nil {
		t.Fatalf("child store.Open: %v", err)
	}
	m := New(st, Settings{
		Dir:           os.Getenv("DM_OUT"),
		MaxConcurrent: 1,
		Connections:   2,
		SaveInterval:  10 * time.Millisecond,
	}, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("child Start: %v", err)
	}
	d, err := m.Add(context.Background(), AddRequest{URL: os.Getenv("DM_URL")})
	if err != nil {
		t.Fatalf("child Add: %v", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		saved, err := st.Get(context.Background(), d.ID)
		if err != nil {
			t.Fatalf("child store.Get: %v", err)
		}
		if downloadedBytes(saved) > 64<<10 {
			if err := os.WriteFile(os.Getenv("DM_READY"), []byte(d.ID), 0o644); err != nil {
				t.Fatalf("child ready marker: %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child never persisted progress (status %s)", saved.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Wait to be killed from the outside.
	select {}
}

func TestKillNineAndResume(t *testing.T) {
	if os.Getenv("DM_CRASH_CHILD") == "1" {
		t.Skip("helper process")
	}
	body := testBytes(4 << 20)
	slow := newRangeServer(body)
	slow.chunk = 16 << 10
	slow.delay = 10 * time.Millisecond
	srv := httptest.NewServer(slow)
	defer srv.Close()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "dm.db")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")

	cmd := exec.Command(os.Args[0], "-test.run=^TestKillNineChild$", "-test.timeout=180s")
	cmd.Env = append(os.Environ(),
		"DM_CRASH_CHILD=1",
		"DM_STORE="+storePath,
		"DM_OUT="+outDir,
		"DM_URL="+srv.URL+"/kill.bin",
		"DM_READY="+ready,
	)
	var childOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &childOut, &childOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child never reported persisted progress; output:\n%s", childOut.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_ = cmd.Wait()

	// Rebuild the manager from the database the child left behind.
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("reopen store after kill: %v; child output:\n%s", err, childOut.String())
	}
	defer st.Close()

	unfinished, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(unfinished) != 1 {
		t.Fatalf("found %d downloads after kill, want 1", len(unfinished))
	}
	d := unfinished[0]
	if d.Status == model.StatusCompleted {
		t.Fatal("child completed the download before being killed")
	}
	if got := downloadedBytes(d); got == 0 || got >= int64(len(body)) {
		t.Fatalf("persisted progress = %d/%d bytes, want a partial download", got, len(body))
	}
	part := filepath.Join(outDir, d.Filename) + ".part"
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("partial file missing after kill: %v", err)
	}

	second := New(st, Settings{
		Dir:           outDir,
		MaxConcurrent: 1,
		Connections:   2,
		SaveInterval:  50 * time.Millisecond,
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := second.Start(ctx); err != nil {
		t.Fatalf("Start after kill: %v", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = second.Shutdown(sctx)
	}()

	done := waitCompleted(t, second, d.ID, 30*time.Second)
	if done.Status != model.StatusCompleted {
		t.Fatalf("status after restart = %s, err %q", done.Status, done.Error)
	}
	wantChecksum(t, filepath.Join(outDir, done.Filename), body)
}

func TestScheduledDownloadStaysScheduled(t *testing.T) {
	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 2})

	start := time.Now().Add(10 * time.Second)
	d, err := h.m.Add(context.Background(), AddRequest{
		URL:     "https://example.com/file.bin",
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.Status != model.StatusScheduled {
		t.Fatalf("status = %s, want %s", d.Status, model.StatusScheduled)
	}

	// It should not be in the queue.
	got, err := h.m.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != model.StatusScheduled {
		t.Fatalf("Get status = %s, want %s", got.Status, model.StatusScheduled)
	}

	// Pump manually and verify it's still scheduled (start time hasn't arrived).
	h.m.pump()
	got, err = h.m.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != model.StatusScheduled {
		t.Fatalf("after pump status = %s, want %s", got.Status, model.StatusScheduled)
	}
}

func TestScheduledDownloadPromotedOnTime(t *testing.T) {
	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 2})

	start := time.Now().Add(50 * time.Millisecond)
	d, err := h.m.Add(context.Background(), AddRequest{
		URL:     "https://example.com/file.bin",
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.Status != model.StatusScheduled {
		t.Fatalf("status = %s, want %s", d.Status, model.StatusScheduled)
	}

	// Wait for the pump to promote it to queued (and then probing since a probe
	// will start once it's running). Give it a generous deadline.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := h.m.Get(context.Background(), d.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status == model.StatusQueued || got.Status == model.StatusProbing || got.Status == model.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for promotion; status = %s", got.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestScheduledDownloadStartsImmediately(t *testing.T) {
	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 2})

	// StartAt in the past -> starts immediately.
	d, err := h.m.Add(context.Background(), AddRequest{
		URL:     "https://example.com/file.bin",
		StartAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.Status != model.StatusQueued {
		t.Fatalf("status = %s, want %s", d.Status, model.StatusQueued)
	}

	// Zero StartAt -> starts immediately.
	d2, err := h.m.Add(context.Background(), AddRequest{
		URL: "https://example.com/file2.bin",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d2.Status != model.StatusQueued {
		t.Fatalf("status = %s, want %s", d2.Status, model.StatusQueued)
	}
}

func TestScheduledDownloadCanBeCanceled(t *testing.T) {
	h := newHarness(t, Settings{MaxConcurrent: 1, Connections: 2})

	start := time.Now().Add(10 * time.Second)
	d, err := h.m.Add(context.Background(), AddRequest{
		URL:     "https://example.com/file.bin",
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	d, err = h.m.Cancel(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if d.Status != model.StatusCanceled {
		t.Fatalf("status = %s, want %s", d.Status, model.StatusCanceled)
	}

	// Pump should not resurrect it.
	h.m.pump()
	got, err := h.m.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != model.StatusCanceled {
		t.Fatalf("after pump status = %s, want %s", got.Status, model.StatusCanceled)
	}
}

func TestScheduledDownloadPersistedAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "dm.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	start := time.Now().Add(10 * time.Second)
	settings := Settings{Dir: filepath.Join(dir, "out"), MaxConcurrent: 1, Connections: 2}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := New(st, settings, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	d, err := m.Add(context.Background(), AddRequest{
		URL:     "https://example.com/file.bin",
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.Status != model.StatusScheduled {
		t.Fatalf("status = %s, want %s", d.Status, model.StatusScheduled)
	}

	// Restart the manager; the scheduled download should stay scheduled.
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	if err := m.Shutdown(sctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	m2 := New(st, settings, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	if err := m2.Start(ctx2); err != nil {
		t.Fatalf("Start second: %v", err)
	}
	defer func() {
		sctx2, scancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel2()
		_ = m2.Shutdown(sctx2)
	}()

	got, err := m2.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if got.Status != model.StatusScheduled {
		t.Fatalf("after restart status = %s, want %s", got.Status, model.StatusScheduled)
	}
	if !got.StartAt.Equal(start) {
		t.Fatalf("StartAt after restart = %v, want %v", got.StartAt, start)
	}
}
