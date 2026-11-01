package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/probe"
)

var (
	// ErrSizeChanged means the remote length no longer matches what was probed.
	// The partial file must be discarded and the download restarted.
	ErrSizeChanged = errors.New("download: remote size changed")
	// ErrRangeUnsupported means the server answered a Range request with 200.
	// The probe said ranges work, so this is fatal until the download is
	// re-probed.
	ErrRangeUnsupported = errors.New("download: server stopped honouring ranges")
)

// Config is the download plan produced by probing.
type Config struct {
	URL           string
	FinalURL      string
	Size          int64 // 0 means unknown
	AcceptsRanges bool
	ETag          string
	LastModified  string

	Headers  map[string]string
	Cookies  []model.Cookie
	Referrer string

	// Resume carries segment progress from an earlier run. It is used only
	// when it exactly covers Size (contiguous, in range, matching indexes);
	// otherwise the download restarts from zero. Ignored when Size is 0.
	Resume []model.Segment
}

// Options tunes concurrency, retries and reporting. Zero values mean defaults.
type Options struct {
	Segments         int           // default 8
	MinSegmentSize   int64         // default 1 MiB
	MaxTries         int           // per segment, default 5
	BackoffBase      time.Duration // default 1s
	BackoffMax       time.Duration // default 60s
	ProgressInterval time.Duration // default 200ms
	Client           *http.Client
	// Limiter, when set, is consulted with the byte count of every read.
	// It may block to enforce a bandwidth limit.
	Limiter Limiter
}

// Limiter throttles network reads. Implementations must be safe for
// concurrent use by several segment workers.
type Limiter interface {
	WaitN(ctx context.Context, n int) error
}

func (o Options) withDefaults() Options {
	if o.Segments <= 0 {
		o.Segments = 8
	}
	if o.MinSegmentSize <= 0 {
		o.MinSegmentSize = 1 << 20
	}
	if o.MaxTries <= 0 {
		o.MaxTries = 5
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 60 * time.Second
	}
	if o.ProgressInterval <= 0 {
		o.ProgressInterval = 200 * time.Millisecond
	}
	if o.Client == nil {
		o.Client = probe.NewClient()
	}
	return o
}

// EventType discriminates progress from completion.
type EventType string

const (
	// EventProgress carries a snapshot of the segment table.
	EventProgress EventType = "progress"
	// EventDone is the last event: every byte is written and the file renamed.
	EventDone EventType = "done"
)

// Event is what Run sends to its caller. Segments is a copy; the caller may
// keep it.
type Event struct {
	Type       EventType
	Total      int64
	Downloaded int64
	Segments   []model.Segment
}

// Run downloads cfg into dest. Bytes go to dest+".part" (preallocated when
// Size is known) and the file is renamed to dest only after every byte is
// written and fsynced. On error the .part file is left in place for a future
// resume. events may be nil; sends are non-blocking, so a slow consumer loses
// progress ticks but never stalls the download.
func Run(ctx context.Context, cfg Config, dest string, opts Options, events chan<- Event) error {
	opts = opts.withDefaults()
	if cfg.FinalURL == "" {
		cfg.FinalURL = cfg.URL
	}

	part := dest + ".part"
	segs, resuming := segmentsFor(cfg, opts)

	flags := os.O_CREATE | os.O_RDWR
	if !resuming {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	if cfg.Size > 0 {
		fi, statErr := f.Stat()
		if statErr != nil || fi.Size() != cfg.Size {
			if err := f.Truncate(cfg.Size); err != nil {
				f.Close()
				return err
			}
		}
	}

	d := &dl{
		cfg:     cfg,
		opts:    opts,
		file:    f,
		events:  events,
		updates: make(chan segmentUpdate, 1024),
		segs:    segs,
	}

	if err := d.run(ctx); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(part, dest); err != nil {
		return err
	}
	d.emit(EventDone)
	return nil
}

// segmentsFor picks the segment table for this run: the single-stream table
// when ranges are unusable, the saved table when it is valid, otherwise a
// fresh split. It reports whether the saved table was used, which decides
// whether the .part file may be truncated.
func segmentsFor(cfg Config, opts Options) ([]model.Segment, bool) {
	if !cfg.AcceptsRanges || cfg.Size <= 0 {
		return []model.Segment{{Index: 0, Start: 0, End: cfg.Size - 1, Status: model.SegmentPending}}, false
	}
	if validResume(cfg.Resume, cfg.Size) {
		segs := make([]model.Segment, len(cfg.Resume))
		copy(segs, cfg.Resume)
		for i := range segs {
			if segs[i].Status == model.SegmentDone && segs[i].Downloaded == 0 {
				segs[i].Downloaded = segs[i].End - segs[i].Start + 1
			}
		}
		return segs, true
	}
	return planSegments(cfg.Size, opts), false
}

// validResume reports whether segs tile [0,size) exactly with sane progress.
func validResume(segs []model.Segment, size int64) bool {
	if len(segs) == 0 || size <= 0 {
		return false
	}
	var next int64
	for i, s := range segs {
		if s.Index != i || s.Start != next || s.End < s.Start {
			return false
		}
		length := s.End - s.Start + 1
		if s.Downloaded < 0 || s.Downloaded > length {
			return false
		}
		next = s.End + 1
	}
	return next == size
}

// planSegments splits size into at most opts.Segments contiguous ranges, each
// at least opts.MinSegmentSize unless the file is smaller than one.
func planSegments(size int64, opts Options) []model.Segment {
	if size <= 0 {
		return nil
	}
	n := int64(opts.Segments)
	if byMin := size / opts.MinSegmentSize; byMin < n {
		n = byMin
	}
	if n < 1 {
		n = 1
	}
	base := size / n
	rem := size % n
	segs := make([]model.Segment, n)
	var off int64
	for i := int64(0); i < n; i++ {
		length := base
		if i < rem {
			length++
		}
		segs[i] = model.Segment{Index: int(i), Start: off, End: off + length - 1, Status: model.SegmentPending}
		off += length
	}
	return segs
}

type segmentUpdate struct {
	index      int
	downloaded int64
	done       bool
}

type dl struct {
	cfg    Config
	opts   Options
	file   *os.File
	events chan<- Event

	updates chan segmentUpdate
	segs    []model.Segment // owned by the coordinator goroutine

	sawProgress bool
}

func (d *dl) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(d.segs))
	var wg sync.WaitGroup
	for _, seg := range d.segs {
		wg.Add(1)
		go func(seg model.Segment) {
			defer wg.Done()
			if err := d.worker(ctx, seg); err != nil {
				errCh <- err
				cancel()
			}
		}(seg)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	ticker := time.NewTicker(d.opts.ProgressInterval)
	defer ticker.Stop()

	for {
		select {
		case u := <-d.updates:
			d.apply(u)
			if !d.sawProgress && u.downloaded > 0 {
				d.sawProgress = true
				d.emit(EventProgress)
			}
		case <-ticker.C:
			d.emit(EventProgress)
		case <-done:
			d.drainUpdates()
			if err := firstErr(errCh); err != nil {
				return err
			}
			return nil
		case <-ctx.Done():
			<-done
			d.drainUpdates()
			if err := firstErr(errCh); err != nil {
				return err
			}
			return ctx.Err()
		}
	}
}

func firstErr(ch chan error) error {
	select {
	case err := <-ch:
		return err
	default:
		return nil
	}
}

func (d *dl) drainUpdates() {
	for {
		select {
		case u := <-d.updates:
			d.apply(u)
		default:
			return
		}
	}
}

func (d *dl) apply(u segmentUpdate) {
	if u.index < 0 || u.index >= len(d.segs) {
		return
	}
	s := &d.segs[u.index]
	s.Downloaded = u.downloaded
	switch {
	case u.done:
		s.Status = model.SegmentDone
		if s.End >= s.Start {
			s.Downloaded = s.End - s.Start + 1
		}
	case s.Status == model.SegmentPending:
		s.Status = model.SegmentRunning
	}
}

func (d *dl) emit(t EventType) {
	if d.events == nil {
		return
	}
	snapshot := make([]model.Segment, len(d.segs))
	copy(snapshot, d.segs)
	var downloaded int64
	for _, s := range snapshot {
		downloaded += s.Downloaded
	}
	select {
	case d.events <- Event{Type: t, Total: d.cfg.Size, Downloaded: downloaded, Segments: snapshot}:
	default:
	}
}

func (d *dl) report(index int, downloaded int64) {
	select {
	case d.updates <- segmentUpdate{index: index, downloaded: downloaded}:
	default:
	}
}

// worker transfers one segment, retrying on retryable failures. It owns all of
// its own counters; the only state it touches is the file handle (WriteAt) and
// the update channel.
func (d *dl) worker(ctx context.Context, seg model.Segment) error {
	downloaded := seg.Downloaded
	if seg.End >= seg.Start && downloaded >= seg.End-seg.Start+1 {
		select {
		case d.updates <- segmentUpdate{index: seg.Index, downloaded: downloaded, done: true}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	for attempt := 1; ; {
		written, err := d.fetch(ctx, seg, downloaded)
		downloaded += written
		if err == nil {
			select {
			case d.updates <- segmentUpdate{index: seg.Index, downloaded: downloaded, done: true}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isFatal(err) {
			return err
		}
		if attempt >= d.opts.MaxTries {
			return fmt.Errorf("segment %d: giving up after %d attempts: %w", seg.Index, attempt, err)
		}
		delay := backoffDelay(rand.New(rand.NewSource(time.Now().UnixNano()+int64(seg.Index))), attempt, d.opts.BackoffBase, d.opts.BackoffMax)
		var retryable *retryableError
		if errors.As(err, &retryable) && retryable.hasRetryAfter {
			delay = retryable.retryAfter
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		attempt++
	}
}

func (d *dl) fetch(ctx context.Context, seg model.Segment, downloaded int64) (int64, error) {
	from := seg.Start + downloaded
	end := seg.End

	ranged := d.cfg.AcceptsRanges && d.cfg.Size > 0
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.FinalURL, nil)
	if err != nil {
		return 0, err
	}
	d.applyHeaders(req)
	if ranged {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, end))
		if downloaded > 0 {
			if v := d.validator(); v != "" {
				req.Header.Set("If-Range", v)
			}
		}
	}

	resp, err := d.opts.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		if !ranged {
			return 0, fmt.Errorf("%w: unsolicited 206", ErrRangeUnsupported)
		}
		start, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			return 0, fmt.Errorf("segment %d: malformed Content-Range %q", seg.Index, resp.Header.Get("Content-Range"))
		}
		if start != from {
			return 0, fmt.Errorf("segment %d: asked for offset %d, got %d", seg.Index, from, start)
		}
		if d.cfg.Size > 0 && total != d.cfg.Size {
			return 0, fmt.Errorf("%w: probed %d bytes, got %d", ErrSizeChanged, d.cfg.Size, total)
		}
	case http.StatusOK:
		if ranged {
			// The server ignored Range after accepting it during probe.
			return 0, fmt.Errorf("%w: bare 200 for bytes=%d-", ErrRangeUnsupported, from)
		}
		if d.cfg.Size > 0 {
			if cl, convErr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); convErr == nil && cl > 0 && cl != d.cfg.Size {
				return 0, fmt.Errorf("%w: probed %d bytes, got %d", ErrSizeChanged, d.cfg.Size, cl)
			}
		}
	case http.StatusRequestedRangeNotSatisfiable:
		return 0, fmt.Errorf("%w: 416 for bytes=%d-%d", ErrSizeChanged, from, end)
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		re := &retryableError{err: fmt.Errorf("HTTP %d for bytes=%d-", resp.StatusCode, from)}
		if delay, ok := retryAfter(resp); ok {
			re.retryAfter, re.hasRetryAfter = delay, true
		}
		return 0, re
	case http.StatusNotFound, http.StatusGone:
		return 0, &fatalError{err: &probe.StatusError{Code: resp.StatusCode, URL: d.cfg.FinalURL}}
	default:
		return 0, &fatalError{err: &probe.StatusError{Code: resp.StatusCode, URL: d.cfg.FinalURL}}
	}

	return d.copyBody(ctx, resp.Body, seg, from)
}

// copyBody streams the response into the file at the right offsets. It returns
// the number of bytes written by this attempt, which the caller adds to the
// segment's running total.
func (d *dl) copyBody(ctx context.Context, body io.Reader, seg model.Segment, from int64) (int64, error) {
	buf := make([]byte, 64<<10)
	off := from
	var written int64
	lastReport := time.Now()

	for {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		n, err := body.Read(buf)
		if n > 0 {
			if d.opts.Limiter != nil {
				if werr := d.opts.Limiter.WaitN(ctx, n); werr != nil {
					return written, werr
				}
			}
			if _, werr := d.file.WriteAt(buf[:n], off); werr != nil {
				return written, &fatalError{err: fmt.Errorf("write at %d: %w", off, werr)}
			}
			off += int64(n)
			written += int64(n)
			if time.Since(lastReport) >= d.opts.ProgressInterval {
				d.report(seg.Index, from-seg.Start+written)
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			if seg.End >= seg.Start {
				want := seg.End - from + 1
				if written < want {
					return written, io.ErrUnexpectedEOF
				}
			}
			d.report(seg.Index, from-seg.Start+written)
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

func (d *dl) applyHeaders(req *http.Request) {
	if _, ok := d.cfg.Headers["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "dm/0.1")
	}
	for k, v := range d.cfg.Headers {
		req.Header.Set(k, v)
	}
	if len(d.cfg.Cookies) > 0 {
		parts := make([]string, 0, len(d.cfg.Cookies))
		for _, c := range d.cfg.Cookies {
			parts = append(parts, c.Name+"="+c.Value)
		}
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	if d.cfg.Referrer != "" {
		req.Header.Set("Referer", d.cfg.Referrer)
	}
}

// validator returns the strongest validator usable for If-Range: a strong ETag
// first, then Last-Modified. Weak ETags are skipped because servers must not
// honour If-Range with them.
func (d *dl) validator() string {
	if et := d.cfg.ETag; et != "" && !strings.HasPrefix(et, "W/") {
		return et
	}
	return d.cfg.LastModified
}

type fatalError struct{ err error }

func (e *fatalError) Error() string { return e.err.Error() }
func (e *fatalError) Unwrap() error { return e.err }

type retryableError struct {
	err           error
	retryAfter    time.Duration
	hasRetryAfter bool
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func isFatal(err error) bool {
	var fe *fatalError
	if errors.As(err, &fe) {
		return true
	}
	return errors.Is(err, ErrSizeChanged) || errors.Is(err, ErrRangeUnsupported)
}

// backoffDelay is exponential backoff with equal jitter: the delay doubles per
// attempt, is capped at max, then is randomised between half and all of it.
func backoffDelay(rnd *rand.Rand, attempt int, base, max time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		if d >= max/2 {
			d = max
			break
		}
		d *= 2
	}
	if d > max {
		d = max
	}
	half := d / 2
	if half <= 0 {
		return 0
	}
	return half + time.Duration(rnd.Int63n(int64(half)+1))
}

// retryAfter parses the Retry-After header as delta-seconds or an HTTP-date,
// clamping to [0, 60s]. A date in the past or an unparsable value yields false.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > 60 {
			secs = 60
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(v); err == nil {
		d := time.Until(when)
		if d <= 0 {
			return 0, false
		}
		if d > 60*time.Second {
			d = 60 * time.Second
		}
		return d, true
	}
	return 0, false
}

// parseContentRange parses "bytes 100-199/1234" into its start offset and total
// size. The wildcard total "bytes 100-199/*" yields total 0.
func parseContentRange(v string) (start, total int64, ok bool) {
	if !strings.HasPrefix(v, "bytes ") {
		return 0, 0, false
	}
	rest := strings.TrimPrefix(v, "bytes ")
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return 0, 0, false
	}
	span := rest[:slash]
	dash := strings.IndexByte(span, '-')
	if dash <= 0 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(span[:dash]), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	totalStr := strings.TrimSpace(rest[slash+1:])
	if totalStr == "*" {
		return start, 0, true
	}
	total, err = strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, total, true
}
