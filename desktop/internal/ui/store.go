package ui

import (
	"sort"
	"time"

	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/internal/model"
)

// filterKind identifies one navigation destination in the sidebar.
type filterKind string

const (
	filterAll       filterKind = "all"
	filterActive    filterKind = "active"
	filterQueued    filterKind = "queued"
	filterPaused    filterKind = "paused"
	filterCompleted filterKind = "completed"
	filterFailed    filterKind = "failed"
	filterCategory  filterKind = "category"
)

// filter is the current sidebar selection.
type filter struct {
	kind       filterKind
	categoryID string
	label      string
}

// matches reports whether d belongs in the view selected by f.
func (f filter) matches(d model.Download) bool {
	switch f.kind {
	case filterAll:
		return true
	case filterActive:
		return d.Status == model.StatusRunning || d.Status == model.StatusProbing
	case filterQueued:
		// Scheduled downloads are still waiting to start, so they belong
		// with queued work rather than in their own destination.
		return d.Status == model.StatusQueued || d.Status == model.StatusScheduled
	case filterPaused:
		return d.Status == model.StatusPaused
	case filterCompleted:
		return d.Status == model.StatusCompleted
	case filterFailed:
		return d.Status == model.StatusFailed
	case filterCategory:
		return d.CategoryID == f.categoryID
	}
	return false
}

// rateSample is the per-download speed sampling state.
type rateSample struct {
	at    time.Time
	bytes int64
	rate  float64
}

// viewStore is the desktop client's copy of daemon state. It is not the source
// of truth: the daemon owns it, and this copy is replaced or patched from the
// REST snapshot and the WebSocket event stream. It is not safe for concurrent
// use; every call happens on the GTK main thread.
type viewStore struct {
	items map[string]model.Download
	rates map[string]*rateSample
	now   func() time.Time
}

func newViewStore() *viewStore {
	return &viewStore{
		items: make(map[string]model.Download),
		rates: make(map[string]*rateSample),
		now:   time.Now,
	}
}

// replaceAll swaps in a fresh REST snapshot, dropping stale entries and their
// speed samples.
func (s *viewStore) replaceAll(list []model.Download) {
	s.items = make(map[string]model.Download, len(list))
	s.rates = make(map[string]*rateSample, len(list))
	for _, d := range list {
		s.items[d.ID] = d
		s.observe(d)
	}
}

// apply folds one WebSocket event into the store. It returns false when the
// event carried no download (for example an unknown event type).
func (s *viewStore) apply(ev model.Event) bool {
	if ev.Type == model.EventRemoved || ev.DownloadID == "" {
		delete(s.items, ev.DownloadID)
		delete(s.rates, ev.DownloadID)
		return true
	}
	if ev.Download == nil {
		return false
	}
	d := *ev.Download
	s.items[d.ID] = d
	s.observe(d)
	return true
}

// observe updates the speed sample for d. Rates are only meaningful while the
// daemon is transferring; anything else reports zero rather than a stale or
// invented speed.
func (s *viewStore) observe(d model.Download) {
	now := s.now()
	bytes := dmapi.Downloaded(d)

	st, ok := s.rates[d.ID]
	if !ok {
		s.rates[d.ID] = &rateSample{at: now, bytes: bytes}
		return
	}
	if dt := now.Sub(st.at).Seconds(); dt >= 0.05 && bytes >= st.bytes {
		inst := float64(bytes-st.bytes) / dt
		if st.rate == 0 {
			st.rate = inst
		} else {
			st.rate = 0.7*st.rate + 0.3*inst
		}
		st.at = now
		st.bytes = bytes
	}
	if !activeStatus(d.Status) {
		st.rate = 0
	}
}

func activeStatus(s model.Status) bool {
	return s == model.StatusRunning || s == model.StatusProbing
}

// get returns the stored copy of one download.
func (s *viewStore) get(id string) (model.Download, bool) {
	d, ok := s.items[id]
	return d, ok
}

// len reports how many downloads the daemon has, regardless of the filter.
func (s *viewStore) len() int { return len(s.items) }

// rate returns the smoothed bytes/second for one download, or 0.
func (s *viewStore) rate(id string) float64 {
	if st, ok := s.rates[id]; ok {
		return st.rate
	}
	return 0
}

// ids returns the IDs visible under f, newest first.
func (s *viewStore) ids(f filter) []string {
	out := make([]string, 0, len(s.items))
	for id, d := range s.items {
		if f.matches(d) {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := s.items[out[i]], s.items[out[j]]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return out
}

// counts returns the number of downloads in each lifecycle filter. Category
// counts are queried separately because a download belongs to one category.
func (s *viewStore) counts() map[filterKind]int {
	c := map[filterKind]int{
		filterAll:       len(s.items),
		filterActive:    0,
		filterQueued:    0,
		filterPaused:    0,
		filterCompleted: 0,
		filterFailed:    0,
	}
	for _, d := range s.items {
		for _, kind := range []filterKind{filterActive, filterQueued, filterPaused, filterCompleted, filterFailed} {
			if (filter{kind: kind}).matches(d) {
				c[kind]++
			}
		}
	}
	return c
}

// countCategory counts downloads in one category.
func (s *viewStore) countCategory(id string) int {
	n := 0
	for _, d := range s.items {
		if d.CategoryID == id {
			n++
		}
	}
	return n
}
