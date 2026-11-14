package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/vishwa/dm/internal/model"
)

func makeDownload(id string, status model.Status, created time.Time) model.Download {
	return model.Download{
		ID:        id,
		URL:       "https://example.com/" + id + ".bin",
		FinalURL:  "https://cdn.example.com/" + id + ".bin",
		Filename:  id + ".bin",
		Dir:       "/tmp",
		Size:      2048,
		Status:    status,
		CreatedAt: created,
		Segments: []model.Segment{
			{Index: 0, Start: 0, End: 2047, Downloaded: 1024, Status: model.SegmentRunning},
		},
	}
}

func TestFilterMatches(t *testing.T) {
	base := time.Now()
	cases := []struct {
		filter filter
		d      model.Download
		want   bool
	}{
		{filter{kind: filterAll}, makeDownload("a", model.StatusCanceled, base), true},
		{filter{kind: filterActive}, makeDownload("a", model.StatusRunning, base), true},
		{filter{kind: filterActive}, makeDownload("a", model.StatusProbing, base), true},
		{filter{kind: filterActive}, makeDownload("a", model.StatusPaused, base), false},
		{filter{kind: filterQueued}, makeDownload("a", model.StatusQueued, base), true},
		{filter{kind: filterQueued}, makeDownload("a", model.StatusScheduled, base), true},
		{filter{kind: filterPaused}, makeDownload("a", model.StatusPaused, base), true},
		{filter{kind: filterCompleted}, makeDownload("a", model.StatusCompleted, base), true},
		{filter{kind: filterFailed}, makeDownload("a", model.StatusFailed, base), true},
		{filter{kind: filterCategory, categoryID: "c1"}, model.Download{CategoryID: "c1"}, true},
		{filter{kind: filterCategory, categoryID: "c1"}, model.Download{CategoryID: "c2"}, false},
	}
	for _, c := range cases {
		if got := c.filter.matches(c.d); got != c.want {
			t.Errorf("%s.matches(%s) = %v, want %v", c.filter.kind, c.d.Status, got, c.want)
		}
	}
}

func TestStoreReplaceAllOrdersNewestFirst(t *testing.T) {
	s := newViewStore()
	now := time.Now()
	s.replaceAll([]model.Download{
		makeDownload("old", model.StatusCompleted, now.Add(-time.Hour)),
		makeDownload("new", model.StatusRunning, now),
		makeDownload("mid", model.StatusPaused, now.Add(-time.Minute)),
	})
	got := s.ids(filter{kind: filterAll})
	want := []string{"new", "mid", "old"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestStoreApplyEvents(t *testing.T) {
	s := newViewStore()
	now := time.Now()
	d := makeDownload("a", model.StatusQueued, now)
	s.apply(model.Event{Type: model.EventAdded, DownloadID: d.ID, Download: &d})

	if s.len() != 1 {
		t.Fatalf("len = %d after added, want 1", s.len())
	}
	d.Status = model.StatusRunning
	s.apply(model.Event{Type: model.EventStatusChanged, DownloadID: d.ID, Download: &d})
	if got := s.ids(filter{kind: filterActive}); len(got) != 1 || got[0] != "a" {
		t.Errorf("active ids = %v, want [a]", got)
	}

	s.apply(model.Event{Type: model.EventRemoved, DownloadID: d.ID})
	if s.len() != 0 {
		t.Errorf("len = %d after removed, want 0", s.len())
	}
}

func TestStoreIgnoresEventWithoutDownload(t *testing.T) {
	s := newViewStore()
	if s.apply(model.Event{Type: model.EventProgress, DownloadID: "x"}) {
		t.Error("apply reported a change for a nil download")
	}
	if s.len() != 0 {
		t.Errorf("len = %d, want 0", s.len())
	}
}

func TestStoreCounts(t *testing.T) {
	s := newViewStore()
	now := time.Now()
	a := makeDownload("a", model.StatusRunning, now)
	b := makeDownload("b", model.StatusQueued, now)
	c := makeDownload("c", model.StatusScheduled, now)
	c.CategoryID = "cat"
	s.replaceAll([]model.Download{a, b, c})

	counts := s.counts()
	if counts[filterAll] != 3 || counts[filterActive] != 1 || counts[filterQueued] != 2 {
		t.Errorf("counts = %v", counts)
	}
	if got := s.countCategory("cat"); got != 1 {
		t.Errorf("category count = %d, want 1", got)
	}
	if got := s.countCategory("missing"); got != 0 {
		t.Errorf("missing category count = %d, want 0", got)
	}
}

func TestStoreRateSamples(t *testing.T) {
	s := newViewStore()
	now := time.Now()
	s.now = func() time.Time { return now }

	d := makeDownload("a", model.StatusRunning, now)
	d.Segments[0].Downloaded = 0
	s.replaceAll([]model.Download{d})

	// Advance 1s and add 1 MiB: the first interval seeds the rate.
	now = now.Add(time.Second)
	d.Segments[0].Downloaded = 1024 * 1024
	s.apply(model.Event{Type: model.EventProgress, DownloadID: d.ID, Download: &d})
	if got := s.rate(d.ID); got != 1024*1024 {
		t.Fatalf("rate = %v, want 1 MiB/s", got)
	}

	// Another second with no progress decays the rate toward zero.
	now = now.Add(time.Second)
	s.apply(model.Event{Type: model.EventProgress, DownloadID: d.ID, Download: &d})
	if got := s.rate(d.ID); got >= 1024*1024 {
		t.Errorf("rate = %v, want it to decay after a stalled interval", got)
	}

	// A paused download reports no rate at all.
	d.Status = model.StatusPaused
	s.apply(model.Event{Type: model.EventStatusChanged, DownloadID: d.ID, Download: &d})
	if got := s.rate(d.ID); got != 0 {
		t.Errorf("paused rate = %v, want 0", got)
	}
}

func TestDescribeRunningKnownSize(t *testing.T) {
	d := makeDownload("a", model.StatusRunning, time.Now())
	text := describe(d, 1024*1024)
	if !text.Determinate {
		t.Fatal("known size should be determinate")
	}
	if text.Fraction != 0.5 {
		t.Errorf("fraction = %v, want 0.5", text.Fraction)
	}
	if text.Status != "50%" {
		t.Errorf("status = %q, want 50%%", text.Status)
	}
	if !strings.Contains(text.Context, "1.0 KiB of 2.0 KiB") {
		t.Errorf("context = %q, want byte counts", text.Context)
	}
	if !strings.Contains(text.Context, "1.0 MiB/s") {
		t.Errorf("context = %q, want rate", text.Context)
	}
	if strings.Contains(text.Status, "/s") || strings.Contains(text.Status, "ETA") {
		t.Errorf("status = %q, want no invented ETA", text.Status)
	}
	if text.Spinning {
		t.Error("known-size transfer should not spin")
	}
}

func TestDescribeUnknownSize(t *testing.T) {
	d := makeDownload("a", model.StatusRunning, time.Now())
	d.Size = 0
	d.Segments = nil
	text := describe(d, 512*1024)
	if text.Determinate {
		t.Error("unknown size must not be determinate")
	}
	if text.Status != "Downloading" {
		t.Errorf("status = %q, want Downloading", text.Status)
	}
	if strings.Contains(text.Status, "%") || strings.Contains(text.Context, "left") {
		t.Errorf("unknown size fabricated progress: %q / %q", text.Status, text.Context)
	}
	if !text.Spinning {
		t.Error("unknown size should show activity")
	}
	if !strings.Contains(text.Context, "512.0 KiB/s") {
		t.Errorf("context = %q, want observed rate", text.Context)
	}
}

func TestDescribeStates(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		mutate     func(*model.Download)
		wantStatus string
		wantCtx    string
	}{
		{
			name:       "probing",
			mutate:     func(d *model.Download) { d.Status = model.StatusProbing },
			wantStatus: "Checking server",
			wantCtx:    "cdn.example.com",
		},
		{
			name:       "paused",
			mutate:     func(d *model.Download) { d.Status = model.StatusPaused },
			wantStatus: "Paused",
			wantCtx:    "cdn.example.com · 1.0 KiB of 2.0 KiB",
		},
		{
			name:       "completed",
			mutate:     func(d *model.Download) { d.Status = model.StatusCompleted },
			wantStatus: "Completed",
			wantCtx:    "cdn.example.com · 2.0 KiB",
		},
		{
			name:       "failed",
			mutate:     func(d *model.Download) { d.Status = model.StatusFailed; d.Error = "connection reset" },
			wantStatus: "Failed",
			wantCtx:    "cdn.example.com · Connection reset by the server",
		},
		{
			name:       "canceled",
			mutate:     func(d *model.Download) { d.Status = model.StatusCanceled },
			wantStatus: "Canceled",
			wantCtx:    "cdn.example.com · 1.0 KiB of 2.0 KiB",
		},
	}
	for _, c := range cases {
		d := makeDownload("a", model.StatusRunning, now)
		c.mutate(&d)
		text := describe(d, 0)
		if text.Status != c.wantStatus {
			t.Errorf("%s: status = %q, want %q", c.name, text.Status, c.wantStatus)
		}
		if text.Context != c.wantCtx {
			t.Errorf("%s: context = %q, want %q", c.name, text.Context, c.wantCtx)
		}
	}
}

func TestDescribeFailedKeepsRawErrorForTooltip(t *testing.T) {
	d := makeDownload("a", model.StatusFailed, time.Now())
	d.Error = "probe: http://127.0.0.1:8099/does-not-exist.bin returned HTTP 404"
	text := describe(d, 0)
	if !strings.Contains(text.Context, "Server returned HTTP 404") {
		t.Errorf("context = %q, want a human-readable reason", text.Context)
	}
	if text.Detail != d.Error {
		t.Errorf("detail = %q, want the raw error", text.Detail)
	}
	if text.StatusIcon != iconFailed {
		t.Errorf("icon = %q, want %q", text.StatusIcon, iconFailed)
	}
}

func TestDescribeStatusIcons(t *testing.T) {
	cases := map[model.Status]string{
		model.StatusRunning:   "",
		model.StatusProbing:   "",
		model.StatusQueued:    iconQueued,
		model.StatusScheduled: iconScheduled,
		model.StatusPaused:    iconPaused,
		model.StatusCompleted: iconCompleted,
		model.StatusFailed:    iconFailed,
		model.StatusCanceled:  iconCanceled,
	}
	for status, want := range cases {
		d := makeDownload("a", status, time.Now())
		if got := describe(d, 0).StatusIcon; got != want {
			t.Errorf("%s icon = %q, want %q", status, got, want)
		}
	}
}

func TestDescribeScheduled(t *testing.T) {
	d := makeDownload("a", model.StatusScheduled, time.Now())
	d.StartAt = time.Date(2026, 11, 14, 15, 4, 0, 0, time.Local)
	text := describe(d, 0)
	if text.Status != "Scheduled" {
		t.Errorf("status = %q, want Scheduled", text.Status)
	}
	if !strings.Contains(text.Context, "Starts at 15:04") {
		t.Errorf("context = %q, want start time", text.Context)
	}
}

func TestDescribeFallsBackToURL(t *testing.T) {
	d := model.Download{URL: "https://example.com/file", Status: model.StatusQueued}
	if got := describe(d, 0).Title; got != "https://example.com/file" {
		t.Errorf("title = %q, want the URL", got)
	}
}
