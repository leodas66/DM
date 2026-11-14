package ui

import (
	"strings"
	"testing"
)

func TestFileIcon(t *testing.T) {
	cases := map[string]string{
		"archive.zip":       "package-x-generic-symbolic",
		"movie.MKV":         "video-x-generic-symbolic",
		"song.flac":         "audio-x-generic-symbolic",
		"photo.jpeg":        "image-x-generic-symbolic",
		"tool.deb":          "application-x-executable-symbolic",
		"report.pdf":        "text-x-generic-symbolic",
		"README":            "text-x-generic-symbolic",
		"trailing.":         "text-x-generic-symbolic",
		"no-extension-here": "text-x-generic-symbolic",
	}
	for name, want := range cases {
		if got := fileIcon(name); got != want {
			t.Errorf("fileIcon(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExplainError(t *testing.T) {
	cases := []struct {
		name       string
		err        string
		wantShort  string
		wantDetail string
	}{
		{
			name:       "empty",
			err:        "",
			wantShort:  "",
			wantDetail: "",
		},
		{
			name:       "probe 404",
			err:        "probe: http://127.0.0.1:8099/does-not-exist.bin returned HTTP 404",
			wantShort:  "Server returned HTTP 404",
			wantDetail: "probe: http://127.0.0.1:8099/does-not-exist.bin returned HTTP 404",
		},
		{
			name:       "disk full",
			err:        "write at 333299712: write /out/live.iso.part: disk quota exceeded",
			wantShort:  "Not enough disk space",
			wantDetail: "write at 333299712: write /out/live.iso.part: disk quota exceeded",
		},
		{
			name:       "refused",
			err:        "dial tcp 127.0.0.1:8099: connect: connection refused",
			wantShort:  "Server refused the connection",
			wantDetail: "dial tcp 127.0.0.1:8099: connect: connection refused",
		},
		{
			name:       "dns",
			err:        "dial tcp: lookup nope.invalid: no such host",
			wantShort:  "Host not found",
			wantDetail: "dial tcp: lookup nope.invalid: no such host",
		},
		{
			name:       "timeout",
			err:        "context deadline exceeded",
			wantShort:  "Timed out",
			wantDetail: "context deadline exceeded",
		},
		{
			name:       "only first line in the row",
			err:        "connection reset by peer\nlong stack detail follows",
			wantShort:  "Connection reset by the server",
			wantDetail: "connection reset by peer\nlong stack detail follows",
		},
		{
			name:       "unknown error is clamped",
			err:        strings.Repeat("x", 200),
			wantShort:  strings.Repeat("x", 79) + "\u2026",
			wantDetail: strings.Repeat("x", 200),
		},
	}
	for _, c := range cases {
		short, detail := explainError(c.err)
		if short != c.wantShort {
			t.Errorf("%s: short = %q, want %q", c.name, short, c.wantShort)
		}
		if detail != c.wantDetail {
			t.Errorf("%s: detail = %q, want %q", c.name, detail, c.wantDetail)
		}
	}
}
