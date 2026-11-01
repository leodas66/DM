// Command mockserver serves a directory over HTTP for manual testing: it
// supports Range requests, can pretend it does not, and can sever connections
// mid-body to exercise retries.
//
//	go run ./scripts/mockserver -dir /tmp/fixtures -addr 127.0.0.1:8099
//	go run ./scripts/mockserver -dir /tmp/fixtures -no-range
//	go run ./scripts/mockserver -dir /tmp/fixtures -flaky-every 3
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

func main() {
	dir := flag.String("dir", ".", "directory to serve")
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	noRange := flag.Bool("no-range", false, "ignore Range headers and always send 200")
	flakyEvery := flag.Int("flaky-every", 0, "sever every Nth GET after 64 KiB")
	flag.Parse()

	srv := &server{dir: *dir, noRange: *noRange, flakyEvery: *flakyEvery}
	log.Printf("serving %s on http://%s (no-range=%v flaky-every=%d)", *dir, *addr, *noRange, *flakyEvery)
	log.Fatal(http.ListenAndServe(*addr, srv))
}

type server struct {
	dir        string
	noRange    bool
	flakyEvery int

	mu   sync.Mutex
	gets int
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean("/" + r.URL.Path)
	path := filepath.Join(s.dir, clean)
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	defer f.Close()

	etag := fmt.Sprintf(`"%x-%x"`, fi.ModTime().UnixNano(), fi.Size())
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	if !s.noRange {
		w.Header().Set("Accept-Ranges", "bytes")
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		return
	}

	start, end, ranged := parseRange(r.Header.Get("Range"), fi.Size())
	if s.noRange || !ranged {
		start, end = 0, fi.Size()-1
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fi.Size()))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	}

	length := end - start + 1

	s.mu.Lock()
	s.gets++
	n := s.gets
	s.mu.Unlock()

	if s.flakyEvery > 0 && n%s.flakyEvery == 0 && length > 64<<10 {
		s.sever(w, f, start, 64<<10)
		return
	}
	_, _ = f.Seek(start, 0)
	_, _ = io.CopyN(w, f, length)
}

// sever writes the first cut bytes of the body, then drops the connection so
// the client sees a truncated read rather than a clean end.
func (s *server) sever(w http.ResponseWriter, f *os.File, start, cut int64) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = f.Seek(start, 0)
	_, _ = io.CopyN(buf, f, cut)
	_ = buf.Flush()
}

func parseRange(v string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(v, "bytes=") || strings.Contains(v, ",") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(v, "bytes="), "-", 2)
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
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
