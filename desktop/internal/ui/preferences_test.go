package ui

import (
	"math"
	"testing"

	"github.com/vishwa/dm/desktop/internal/dmapi"
)

func TestMergeSettingsPreservesUnshownFields(t *testing.T) {
	base := dmapi.Settings{
		Dir:              "/old",
		MaxConcurrent:    3,
		Connections:      8,
		MinSegmentSize:   1 << 20,
		GlobalLimit:      0,
		PerDownloadLimit: 0,
		SaveInterval:     "1s",
	}
	got, err := mergeSettings(base, editableSettings{
		Dir:                 "  /downloads  ",
		MaxConcurrent:       4,
		Connections:         6,
		GlobalMiBLimit:      2,
		PerDownloadMiBLimit: 0.5,
	})
	if err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}
	if got.Dir != "/downloads" {
		t.Errorf("Dir = %q, want the trimmed value", got.Dir)
	}
	if got.MaxConcurrent != 4 || got.Connections != 6 {
		t.Errorf("counts = %d/%d, want 4/6", got.MaxConcurrent, got.Connections)
	}
	if got.GlobalLimit != 2*1024*1024 {
		t.Errorf("GlobalLimit = %d, want 2 MiB", got.GlobalLimit)
	}
	if got.PerDownloadLimit != 512*1024 {
		t.Errorf("PerDownloadLimit = %d, want 512 KiB", got.PerDownloadLimit)
	}
	// The daemon replaces the whole record on PUT, so these two must survive
	// a form that does not edit them.
	if got.MinSegmentSize != base.MinSegmentSize || got.SaveInterval != base.SaveInterval {
		t.Errorf("unshown fields = %d/%q, want %d/%q",
			got.MinSegmentSize, got.SaveInterval, base.MinSegmentSize, base.SaveInterval)
	}
}

func TestMergeSettingsValidation(t *testing.T) {
	valid := editableSettings{
		Dir:           "/downloads",
		MaxConcurrent: 3,
		Connections:   8,
	}
	cases := []struct {
		name   string
		mutate func(*editableSettings)
	}{
		{"empty dir", func(e *editableSettings) { e.Dir = "   " }},
		{"zero concurrent", func(e *editableSettings) { e.MaxConcurrent = 0 }},
		{"too many concurrent", func(e *editableSettings) { e.MaxConcurrent = maxConcurrentLimit + 1 }},
		{"zero connections", func(e *editableSettings) { e.Connections = 0 }},
		{"too many connections", func(e *editableSettings) { e.Connections = maxConnectionsLimit + 1 }},
		{"negative global limit", func(e *editableSettings) { e.GlobalMiBLimit = -1 }},
		{"negative per-download limit", func(e *editableSettings) { e.PerDownloadMiBLimit = -1 }},
		{"absurd global limit", func(e *editableSettings) { e.GlobalMiBLimit = maxMiBPerSecond + 1 }},
		{"absurd per-download limit", func(e *editableSettings) { e.PerDownloadMiBLimit = maxMiBPerSecond + 1 }},
		{"NaN limit", func(e *editableSettings) { e.GlobalMiBLimit = math.NaN() }},
		{"infinite limit", func(e *editableSettings) { e.PerDownloadMiBLimit = math.Inf(1) }},
	}
	for _, c := range cases {
		edit := valid
		c.mutate(&edit)
		if _, err := mergeSettings(dmapi.Settings{}, edit); err == nil {
			t.Errorf("%s: mergeSettings accepted invalid input", c.name)
		}
	}
}

func TestMiBLimitRoundTrip(t *testing.T) {
	cases := []struct {
		bytes int64
		want  float64
	}{
		{0, 0},
		{-1, 0},
		{1 << 20, 1},
		{1536 * 1024, 1.5},
		{2 * 1024 * 1024, 2},
	}
	for _, c := range cases {
		got := mibLimit(c.bytes)
		if got != c.want {
			t.Errorf("mibLimit(%d) = %v, want %v", c.bytes, got, c.want)
		}
		if back := bytesFromMiBLimit(got); back != c.bytes && c.bytes > 0 {
			t.Errorf("round trip %d -> %v -> %d", c.bytes, got, back)
		}
	}
	if got := bytesFromMiBLimit(0.5); got != 512*1024 {
		t.Errorf("bytesFromMiBLimit(0.5) = %d, want 512 KiB", got)
	}
	if got := bytesFromMiBLimit(-3); got != 0 {
		t.Errorf("bytesFromMiBLimit(-3) = %d, want 0", got)
	}
}
