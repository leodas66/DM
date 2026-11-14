package ui

import (
	"strings"
	"testing"
)

func TestClipboardURLAcceptsOneHTTPLink(t *testing.T) {
	cases := []string{
		"https://example.com/file.iso",
		"http://127.0.0.1:8099/notes.txt",
		"  https://example.com/a/b.zip?x=1&y=2  ",
		"https://example.com:8443/path",
	}
	for _, in := range cases {
		got, ok := clipboardURL(in)
		if !ok {
			t.Errorf("clipboardURL(%q) rejected a valid link", in)
			continue
		}
		if got != strings.TrimSpace(in) {
			t.Errorf("clipboardURL(%q) = %q, want the trimmed value", in, got)
		}
	}
}

func TestClipboardURLRejectsEverythingElse(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"file:///etc/passwd",
		"ftp://example.com/file.iso",
		"javascript:alert(1)",
		"https://",
		"not a url",
		"see https://example.com/file.iso for details",
		"https://example.com/file.iso\nhttps://example.com/other.iso",
		"https://example.com/a b.iso",
	}
	for _, in := range cases {
		if got, ok := clipboardURL(in); ok {
			t.Errorf("clipboardURL(%q) = %q, want rejection", in, got)
		}
	}
}

func TestClipboardURLRejectsAbsurdLength(t *testing.T) {
	long := "https://example.com/" + string(make([]byte, 5000))
	if _, ok := clipboardURL(long); ok {
		t.Error("clipboardURL accepted an oversized value")
	}
}
