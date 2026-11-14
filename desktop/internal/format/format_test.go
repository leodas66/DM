package format

import "testing"

func TestBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{-5, "0 B"},
		{0, "0 B"},
		{999, "999 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{5*1024*1024 + 200*1024, "5.2 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}
	for _, c := range cases {
		if got := Bytes(c.in); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRate(t *testing.T) {
	if got := Rate(0.4); got != "0 B/s" {
		t.Errorf("Rate(0.4) = %q, want 0 B/s", got)
	}
	if got := Rate(1536); got != "1.5 KiB/s" {
		t.Errorf("Rate(1536) = %q, want 1.5 KiB/s", got)
	}
}

func TestPercent(t *testing.T) {
	if _, ok := Percent(10, 0); ok {
		t.Error("Percent with unknown total reported a value")
	}
	if pct, ok := Percent(1, 3); !ok || pct != 33 {
		t.Errorf("Percent(1,3) = %d,%v want 33,true", pct, ok)
	}
	if pct, _ := Percent(5, 3); pct != 100 {
		t.Errorf("Percent over total = %d, want clamped 100", pct)
	}
}

func TestETA(t *testing.T) {
	if _, ok := ETA(1000, 0); ok {
		t.Error("ETA with no rate reported a value")
	}
	if _, ok := ETA(0, 1000); ok {
		t.Error("ETA with nothing remaining reported a value")
	}
	if got, ok := ETA(2048*100, 1024); !ok || got != "3 min 20 s" {
		t.Errorf("ETA = %q,%v want 3 min 20 s,true", got, ok)
	}
}

func TestDuration(t *testing.T) {
	cases := map[int64]string{
		0:    "0 s",
		59:   "59 s",
		60:   "1 min",
		605:  "10 min 5 s",
		3600: "1 h",
		7815: "2 h 10 min",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHost(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.com/files/a.bin?x=1": "cdn.example.com",
		"http://example.com:8080/a.bin":           "example.com:8080",
		"not a url":                               "not a url",
	}
	for in, want := range cases {
		if got := Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContext(t *testing.T) {
	if got := Context("example.com", "1.0 MiB / 2.0 MiB"); got != "example.com · 1.0 MiB / 2.0 MiB" {
		t.Errorf("Context = %q", got)
	}
	if got := Context("", "Paused"); got != "Paused" {
		t.Errorf("Context with empty host = %q", got)
	}
}
