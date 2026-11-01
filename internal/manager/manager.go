// Package manager owns all download state: the queue, the state machine,
// concurrency and speed limits, persistence and the event stream. Only the
// Manager writes to the store, and only the Manager's loop touches the item
// map. Workers report progress over channels; they never share state with it.
package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vishwa/dm/internal/download"
	"github.com/vishwa/dm/internal/model"
	"github.com/vishwa/dm/internal/probe"
	"github.com/vishwa/dm/internal/store"
)

// ErrClosed means the manager has shut down.
var ErrClosed = errors.New("manager: closed")

// Settings are the manager-wide knobs. Zero values mean defaults where noted.
type Settings struct {
	Dir              string
	MaxConcurrent    int // default 3
	Connections      int // default 8
	MinSegmentSize   int64
	GlobalLimit      int64 // bytes/sec, 0 = unlimited
	PerDownloadLimit int64 // bytes/sec, 0 = unlimited
	SaveInterval     time.Duration
}

func (s Settings) withDefaults() Settings {
	if s.MaxConcurrent <= 0 {
		s.MaxConcurrent = 3
	}
	if s.Connections <= 0 {
		s.Connections = 8
	}
	if s.MinSegmentSize <= 0 {
		s.MinSegmentSize = 1 << 20
	}
	if s.SaveInterval <= 0 {
		s.SaveInterval = time.Second
	}
	return s
}

// AddRequest is one accepted download. Filename may be empty, in which case
// probing decides it.
type AddRequest struct {
	URL      string
	Filename string
	Dir      string
	Headers  map[string]string
	Cookies  []model.Cookie
	Referrer string
}

// Manager is the single owner of download state.
type Manager struct {
	store    *store.Store
	settings Settings
	client   *http.Client
	bus      *Bus

	cmds      chan command
	msgs      chan workerMsg
	loopDone  chan struct{}
	hardStop  chan struct{}
	closing   chan struct{}
	closeOnce sync.Once
	stopOnce  sync.Once

	// Owned by the loop goroutine after Start returns.
	items  map[string]*item
	order  []string
	queue  []string
	active int
	wg     sync.WaitGroup
	global *Bucket
}

type item struct {
	d        model.Download
	creds    model.Credentials
	cancel   context.CancelFunc
	perDL    *Bucket
	action   string // "pause" or "cancel" while a stop is in flight
	lastSave time.Time
}

// New builds a manager over st. It does no I/O until Start.
func New(st *store.Store, settings Settings, client *http.Client) *Manager {
	if client == nil {
		client = probe.NewClient()
	}
	return &Manager{
		store:    st,
		settings: settings.withDefaults(),
		client:   client,
		bus:      newBus(),
		cmds:     make(chan command),
		msgs:     make(chan workerMsg, 256),
		loopDone: make(chan struct{}),
		hardStop: make(chan struct{}),
		closing:  make(chan struct{}),
		items:    make(map[string]*item),
		global:   NewBucket(settings.GlobalLimit),
	}
}

// Start reloads unfinished downloads from the store and starts the loop.
// Queued, probing and running downloads are re-queued; paused ones stay
// paused. The context bounds the manager's lifetime.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.reload(ctx); err != nil {
		return err
	}
	go m.loop(ctx)
	return nil
}

func (m *Manager) reload(ctx context.Context) error {
	unfinished, err := m.store.Unfinished(ctx)
	if err != nil {
		return err
	}
	for _, d := range unfinished {
		it := &item{d: d}
		if d.Status == model.StatusPaused {
			m.items[d.ID] = it
			m.order = append(m.order, d.ID)
			continue
		}
		it.d.Status = model.StatusQueued
		it.d.Error = ""
		m.items[d.ID] = it
		m.order = append(m.order, d.ID)
		m.queue = append(m.queue, d.ID)
	}
	return nil
}

// Events returns a subscription to the manager's event stream. The returned
// cancel function must be called to release it.
func (m *Manager) Events() (<-chan model.Event, func()) {
	return m.bus.subscribe()
}

type cmdKind int

const (
	cmdAdd cmdKind = iota
	cmdPause
	cmdResume
	cmdCancel
	cmdRemove
	cmdList
	cmdGet
	cmdShutdown
)

type command struct {
	kind       cmdKind
	id         string
	add        AddRequest
	deleteFile bool
	reply      chan result
}

type result struct {
	download model.Download
	list     []model.Download
	err      error
}

func (m *Manager) call(ctx context.Context, c command) result {
	c.reply = make(chan result, 1)
	select {
	case m.cmds <- c:
	case <-ctx.Done():
		return result{err: ctx.Err()}
	case <-m.loopDone:
		return result{err: ErrClosed}
	}
	select {
	case r := <-c.reply:
		return r
	case <-ctx.Done():
		return result{err: ctx.Err()}
	case <-m.loopDone:
		return result{err: ErrClosed}
	}
}

// Add accepts a download and queues it.
func (m *Manager) Add(ctx context.Context, req AddRequest) (model.Download, error) {
	r := m.call(ctx, command{kind: cmdAdd, add: req})
	return r.download, r.err
}

// Pause stops a download and keeps its partial data.
func (m *Manager) Pause(ctx context.Context, id string) (model.Download, error) {
	r := m.call(ctx, command{kind: cmdPause, id: id})
	return r.download, r.err
}

// Resume re-queues a paused or failed download.
func (m *Manager) Resume(ctx context.Context, id string) (model.Download, error) {
	r := m.call(ctx, command{kind: cmdResume, id: id})
	return r.download, r.err
}

// Cancel stops a download and marks it canceled, keeping partial data.
func (m *Manager) Cancel(ctx context.Context, id string) (model.Download, error) {
	r := m.call(ctx, command{kind: cmdCancel, id: id})
	return r.download, r.err
}

// Remove deletes the download record and, optionally, its files.
func (m *Manager) Remove(ctx context.Context, id string, deleteFile bool) error {
	return m.call(ctx, command{kind: cmdRemove, id: id, deleteFile: deleteFile}).err
}

// List returns every download, oldest first.
func (m *Manager) List(ctx context.Context) ([]model.Download, error) {
	r := m.call(ctx, command{kind: cmdList})
	return r.list, r.err
}

// Get returns one download.
func (m *Manager) Get(ctx context.Context, id string) (model.Download, error) {
	r := m.call(ctx, command{kind: cmdGet, id: id})
	return r.download, r.err
}

// Shutdown pauses running downloads, persists them and stops the loop.
func (m *Manager) Shutdown(ctx context.Context) error {
	err := m.call(ctx, command{kind: cmdShutdown}).err
	if errors.Is(err, ErrClosed) {
		return nil
	}
	return err
}

// stop is the abrupt variant used by crash tests: workers are cancelled and
// the loop exits without a final save.
func (m *Manager) stop() {
	m.stopOnce.Do(func() { close(m.hardStop) })
	<-m.loopDone
}

func (m *Manager) loop(ctx context.Context) {
	defer close(m.loopDone)
	for {
		m.pump()
		select {
		case <-ctx.Done():
			m.shutdown()
			return
		case <-m.hardStop:
			m.abort()
			return
		case c := <-m.cmds:
			if m.handle(ctx, c) {
				return
			}
		case msg := <-m.msgs:
			m.onMessage(msg)
		}
	}
}

func (m *Manager) handle(ctx context.Context, c command) (stop bool) {
	switch c.kind {
	case cmdAdd:
		c.reply <- m.add(ctx, c.add)
	case cmdPause:
		c.reply <- m.pause(c.id)
	case cmdResume:
		c.reply <- m.resume(ctx, c.id)
	case cmdCancel:
		c.reply <- m.cancel(c.id)
	case cmdRemove:
		c.reply <- m.remove(ctx, c.id, c.deleteFile)
	case cmdList:
		list := make([]model.Download, 0, len(m.order))
		for _, id := range m.order {
			list = append(list, m.items[id].d)
		}
		c.reply <- result{list: list}
	case cmdGet:
		if it, ok := m.items[c.id]; ok {
			c.reply <- result{download: it.d}
		} else {
			c.reply <- result{err: store.ErrNotFound}
		}
	case cmdShutdown:
		m.shutdown()
		c.reply <- result{}
		return true
	}
	return false
}

func (m *Manager) pump() {
	for m.active < m.settings.MaxConcurrent && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		it, ok := m.items[id]
		if !ok || it.d.Status != model.StatusQueued {
			continue
		}
		m.startDownload(it)
	}
}

func (m *Manager) startDownload(it *item) {
	ctx, cancel := context.WithCancel(context.Background())
	it.cancel = cancel
	it.action = ""
	it.perDL = NewBucket(m.settings.PerDownloadLimit)
	it.d.Status = model.StatusProbing
	it.d.Error = ""
	m.active++
	m.wg.Add(1)
	m.save(it)
	go m.work(ctx, it)
	m.emit(model.EventStatusChanged, it.d)
}

func (m *Manager) work(ctx context.Context, it *item) {
	defer m.wg.Done()
	// Snapshot: the loop may update it.d for the UI while this goroutine runs.
	d := it.d
	id := d.ID

	probeHeaders := make(map[string]string, len(it.creds.Headers)+2)
	for k, v := range it.creds.Headers {
		probeHeaders[k] = v
	}
	if len(it.creds.Cookies) > 0 {
		parts := make([]string, 0, len(it.creds.Cookies))
		for _, c := range it.creds.Cookies {
			parts = append(parts, c.Name+"="+c.Value)
		}
		probeHeaders["Cookie"] = strings.Join(parts, "; ")
	}
	if it.creds.Referrer != "" {
		probeHeaders["Referer"] = it.creds.Referrer
	}

	info, err := probe.Probe(ctx, m.client, d.URL, probeHeaders)
	if err != nil {
		m.post(workerMsg{id: id, kind: msgDone, err: err})
		return
	}

	m.post(workerMsg{id: id, kind: msgProbed, info: info})

	segs := d.Segments
	if validatorsChanged(d, info) {
		segs = nil
	}
	filename := d.Filename
	if filename == "" {
		filename = info.Filename
	}
	dest := filepath.Join(d.Dir, filename)

	cfg := download.Config{
		URL:           d.URL,
		FinalURL:      info.FinalURL,
		Size:          info.Size,
		AcceptsRanges: info.AcceptsRanges,
		ETag:          info.ETag,
		LastModified:  info.LastModified,
		Headers:       it.creds.Headers,
		Cookies:       it.creds.Cookies,
		Referrer:      it.creds.Referrer,
		Resume:        segs,
	}

	events := make(chan download.Event, 8)
	go func() {
		for ev := range events {
			m.post(workerMsg{id: id, kind: msgProgress, segments: ev.Segments, total: ev.Total, downloaded: ev.Downloaded})
		}
	}()

	err = download.Run(ctx, cfg, dest, download.Options{
		Segments:       m.settings.Connections,
		MinSegmentSize: m.settings.MinSegmentSize,
		Client:         m.client,
		Limiter:        multiLimiter{m.global, it.perDL},
	}, events)
	close(events)

	m.post(workerMsg{id: id, kind: msgDone, err: err})
}

// validatorsChanged reports whether the remote resource differs from what was
// persisted, in which case partial data must be discarded.
func validatorsChanged(old model.Download, info *probe.Info) bool {
	if old.Size > 0 && info.Size > 0 && old.Size != info.Size {
		return true
	}
	if old.ETag != "" && info.ETag != "" {
		return old.ETag != info.ETag
	}
	if old.LastModified != "" && info.LastModified != "" {
		return old.LastModified != info.LastModified
	}
	return false
}

type msgKind int

const (
	msgProbed msgKind = iota
	msgProgress
	msgDone
)

type workerMsg struct {
	id         string
	kind       msgKind
	info       *probe.Info
	segments   []model.Segment
	total      int64
	downloaded int64
	err        error
}

func (m *Manager) post(msg workerMsg) {
	select {
	case m.msgs <- msg:
	case <-m.closing:
	case <-m.loopDone:
	}
}

func (m *Manager) onMessage(msg workerMsg) {
	if msg.kind == msgDone {
		m.active--
	}
	it, ok := m.items[msg.id]
	if !ok {
		return
	}
	switch msg.kind {
	case msgProbed:
		info := msg.info
		changed := validatorsChanged(it.d, info)
		if it.d.Filename == "" {
			it.d.Filename = info.Filename
		}
		it.d.FinalURL = info.FinalURL
		it.d.Size = info.Size
		it.d.AcceptsRanges = info.AcceptsRanges
		it.d.ETag = info.ETag
		it.d.LastModified = info.LastModified
		if changed {
			// The worker already restarted the download from zero; dropping
			// the table here keeps the persisted state honest. The .part
			// file is left to download.Run, which truncates it — deleting it
			// from this goroutine would race with the worker recreating it.
			it.d.Segments = nil
		}
		it.d.Status = model.StatusRunning
		m.save(it)
		m.emit(model.EventStatusChanged, it.d)
	case msgProgress:
		it.d.Segments = msg.segments
		if time.Since(it.lastSave) >= m.settings.SaveInterval {
			m.save(it)
		}
		m.emit(model.EventProgress, it.d)
	case msgDone:
		it.cancel = nil
		switch {
		case it.action == "pause":
			it.d.Status = model.StatusPaused
			it.action = ""
		case it.action == "cancel":
			it.d.Status = model.StatusCanceled
			it.action = ""
		case msg.err == nil:
			it.d.Status = model.StatusCompleted
			it.d.Error = ""
		case errors.Is(msg.err, context.Canceled):
			it.d.Status = model.StatusPaused
		default:
			it.d.Status = model.StatusFailed
			it.d.Error = msg.err.Error()
		}
		m.save(it)
		m.emit(model.EventStatusChanged, it.d)
	}
}

func (m *Manager) add(ctx context.Context, req AddRequest) result {
	u := strings.TrimSpace(req.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return result{err: fmt.Errorf("manager: only http and https URLs are supported")}
	}
	dir := req.Dir
	if dir == "" {
		dir = m.settings.Dir
	}
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return result{err: err}
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return result{err: err}
	}
	name := ""
	if req.Filename != "" {
		name = probe.SanitizeFilename(req.Filename)
	}

	d := model.Download{
		ID:        newID(),
		URL:       u,
		Filename:  name,
		Dir:       absDir,
		Status:    model.StatusQueued,
		CreatedAt: time.Now().UTC(),
	}
	it := &item{d: d, creds: model.Credentials{
		Headers:  req.Headers,
		Cookies:  req.Cookies,
		Referrer: req.Referrer,
	}}
	if err := m.store.Save(ctx, d); err != nil {
		return result{err: err}
	}
	m.items[d.ID] = it
	m.order = append(m.order, d.ID)
	m.queue = append(m.queue, d.ID)
	m.emit(model.EventAdded, d)
	return result{download: d}
}

func (m *Manager) pause(id string) result {
	it, ok := m.items[id]
	if !ok {
		return result{err: store.ErrNotFound}
	}
	switch it.d.Status {
	case model.StatusRunning, model.StatusProbing:
		it.action = "pause"
		if it.cancel != nil {
			it.cancel()
		}
	case model.StatusQueued:
		m.dropFromQueue(id)
		it.d.Status = model.StatusPaused
		m.save(it)
		m.emit(model.EventStatusChanged, it.d)
	default:
		return result{download: it.d}
	}
	return result{download: it.d}
}

func (m *Manager) cancel(id string) result {
	it, ok := m.items[id]
	if !ok {
		return result{err: store.ErrNotFound}
	}
	m.dropFromQueue(id)
	switch it.d.Status {
	case model.StatusRunning, model.StatusProbing:
		it.action = "cancel"
		if it.cancel != nil {
			it.cancel()
		}
	case model.StatusCompleted, model.StatusFailed, model.StatusCanceled:
	default:
		it.d.Status = model.StatusCanceled
		m.save(it)
		m.emit(model.EventStatusChanged, it.d)
	}
	return result{download: it.d}
}

func (m *Manager) resume(ctx context.Context, id string) result {
	it, ok := m.items[id]
	if !ok {
		return result{err: store.ErrNotFound}
	}
	switch it.d.Status {
	case model.StatusPaused, model.StatusFailed, model.StatusCanceled:
		it.d.Status = model.StatusQueued
		it.d.Error = ""
		m.save(it)
		m.queue = append(m.queue, id)
		m.emit(model.EventStatusChanged, it.d)
	}
	return result{download: it.d}
}

func (m *Manager) remove(ctx context.Context, id string, deleteFile bool) result {
	it, ok := m.items[id]
	if !ok {
		return result{err: store.ErrNotFound}
	}
	if it.d.Status == model.StatusRunning || it.d.Status == model.StatusProbing {
		it.action = "cancel"
		if it.cancel != nil {
			it.cancel()
		}
	}
	m.dropFromQueue(id)

	dir := it.d.Dir
	name := it.d.Filename
	if err := m.store.Delete(ctx, id); err != nil {
		return result{err: err}
	}
	delete(m.items, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	if deleteFile && dir != "" && name != "" {
		dest := filepath.Join(dir, name)
		_ = os.Remove(dest)
		_ = os.Remove(dest + ".part")
	}
	m.emitRemoved(id)
	return result{}
}

func (m *Manager) dropFromQueue(id string) {
	for i, qid := range m.queue {
		if qid == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

// save persists the item. The Manager is the only caller.
func (m *Manager) save(it *item) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.store.Save(ctx, it.d); err == nil {
		it.lastSave = time.Now()
	}
}

func (m *Manager) emit(t model.EventType, d model.Download) {
	m.bus.publish(model.Event{Type: t, Download: &d, DownloadID: d.ID, At: time.Now().UTC()})
}

func (m *Manager) emitRemoved(id string) {
	m.bus.publish(model.Event{Type: model.EventRemoved, DownloadID: id, At: time.Now().UTC()})
}

func (m *Manager) shutdown() {
	m.closeOnce.Do(func() { close(m.closing) })
	for _, it := range m.items {
		switch it.d.Status {
		case model.StatusRunning, model.StatusProbing:
			it.d.Status = model.StatusPaused
			m.save(it)
		}
	}
	m.stopWorkers()
}

// abort is the abrupt variant: workers are cancelled but nothing is written,
// which is what a kill -9 leaves behind.
func (m *Manager) abort() {
	m.closeOnce.Do(func() { close(m.closing) })
	m.stopWorkers()
}

func (m *Manager) stopWorkers() {
	for _, it := range m.items {
		if it.cancel != nil {
			it.cancel()
		}
	}
	m.wg.Wait()
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
