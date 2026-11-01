package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestDownloadJSONHidesSecrets guards the rule that credentials never leave
// the process: a Download marshalled for the API must not carry headers,
// referrer, or their values.
func TestDownloadJSONHidesSecrets(t *testing.T) {
	d := Download{
		ID:        "0123456789abcdef0123456789abcdef",
		URL:       "https://example.com/f.bin",
		Filename:  "f.bin",
		Dir:       "/tmp",
		Status:    StatusRunning,
		Headers:   map[string]string{"Authorization": "Bearer s3cret"},
		Referrer:  "https://example.com/private-page",
		CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, leak := range []string{"s3cret", "private-page", "Authorization", "headers", "referrer"} {
		if strings.Contains(s, leak) {
			t.Fatalf("marshalled Download leaks %q: %s", leak, s)
		}
	}
}

// TestDownloadJSONRoundTrip checks the wire format survives a trip through
// the UI, including segments.
func TestDownloadJSONRoundTrip(t *testing.T) {
	in := Download{
		ID:            "0123456789abcdef0123456789abcdef",
		URL:           "https://example.com/f.bin",
		FinalURL:      "https://cdn.example.com/f.bin",
		Filename:      "f.bin",
		Dir:           "/tmp",
		Size:          1 << 20,
		AcceptsRanges: true,
		ETag:          `"abc"`,
		Status:        StatusPaused,
		Segments: []Segment{
			{Index: 0, Start: 0, End: 524287, Downloaded: 100, Status: SegmentRunning},
			{Index: 1, Start: 524288, End: 1048575, Status: SegmentPending},
		},
		CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Download
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.ID != in.ID || out.FinalURL != in.FinalURL || out.Status != in.Status ||
		out.Size != in.Size || !out.AcceptsRanges || out.ETag != in.ETag ||
		!out.CreatedAt.Equal(in.CreatedAt) {
		t.Fatalf("round trip mismatch:\n in: %+v\nout: %+v", in, out)
	}
	if len(out.Segments) != 2 || out.Segments[0].Downloaded != 100 ||
		out.Segments[1].Status != SegmentPending {
		t.Fatalf("segments round trip mismatch: %+v", out.Segments)
	}
	// Secrets must survive being zeroed on the way back in, not be revived.
	if out.Headers != nil || out.Referrer != "" {
		t.Fatalf("secrets restored from JSON: %+v", out)
	}
}

// TestStatusValues pins the wire strings. Renaming one silently breaks the UI
// and any stored database rows.
func TestStatusValues(t *testing.T) {
	want := map[Status]string{
		StatusQueued:    "queued",
		StatusProbing:   "probing",
		StatusRunning:   "running",
		StatusPaused:    "paused",
		StatusCompleted: "completed",
		StatusFailed:    "failed",
		StatusCanceled:  "canceled",
	}
	seen := map[Status]bool{}
	for s, str := range want {
		if string(s) != str {
			t.Errorf("status %q renders as %q", str, s)
		}
		if seen[s] {
			t.Errorf("duplicate status value %q", s)
		}
		seen[s] = true
	}
	if len(seen) != 7 {
		t.Errorf("expected 7 statuses, got %d", len(seen))
	}
}

// TestEventRemovedShape checks the one event whose Download is nil.
func TestEventRemovedShape(t *testing.T) {
	ev := Event{Type: EventRemoved, DownloadID: "abc", At: time.Now().UTC()}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"download"`) {
		t.Fatalf("removed event must omit download: %s", b)
	}

	var out Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != EventRemoved || out.DownloadID != "abc" || out.Download != nil {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}
