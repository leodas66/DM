package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vishwa/dm/desktop/internal/format"
	"github.com/vishwa/dm/internal/model"
)

// rowText is everything one download row displays. Deriving it separately from
// the GTK widget keeps the formatting rules testable and consistent.
type rowText struct {
	// Title is the primary label: the filename, or the URL before probing
	// has named the file.
	Title string

	// Context is the secondary line: source host plus honest transfer detail.
	Context string

	// Status is the short state label shown on the right of the title.
	Status string

	// StatusIcon is the symbolic icon shown before Status. It is empty when
	// the state is already conveyed by a spinner or a percentage.
	StatusIcon string

	// Detail is the full text behind the row for a tooltip: the raw error
	// for a failed download, otherwise the source URL.
	Detail string

	// Fraction is the determinate progress in [0,1]. Only meaningful when
	// Determinate is true.
	Fraction float64

	// Determinate is false when the total size is unknown; the UI must then
	// show activity without inventing a percentage.
	Determinate bool

	// Spinning requests the standard GTK activity indicator.
	Spinning bool
}

// statusIcon names are standard freedesktop symbolic icons, so they resolve in
// every GTK icon theme rather than only in a GNOME one.
const (
	iconPaused    = "media-playback-pause-symbolic"
	iconQueued    = "appointment-soon-symbolic"
	iconScheduled = "alarm-symbolic"
	iconCompleted = "object-select-symbolic"
	iconFailed    = "dialog-error-symbolic"
	iconCanceled  = "process-stop-symbolic"
)

// describe builds the row text for one download at a sampled transfer rate.
func describe(d model.Download, bytesPerSecond float64) rowText {
	t := rowText{
		Title:   d.Filename,
		Context: hostOf(d),
		Status:  string(d.Status),
		Detail:  sourceOf(d),
	}
	if t.Title == "" {
		t.Title = d.URL
	}
	if t.Title == "" {
		t.Title = "Download"
	}

	downloaded := int64(0)
	for _, seg := range d.Segments {
		downloaded += seg.Downloaded
	}

	switch d.Status {
	case model.StatusScheduled:
		t.Status = "Scheduled"
		t.StatusIcon = iconScheduled
		if !d.StartAt.IsZero() {
			t.Context = format.Context(hostOf(d), "Starts at "+d.StartAt.Local().Format("15:04"))
		}

	case model.StatusQueued:
		t.Status = "Queued"
		t.StatusIcon = iconQueued
		if d.Size > 0 {
			t.Context = format.Context(hostOf(d), format.Bytes(d.Size))
		}

	case model.StatusProbing:
		t.Status = "Checking server"
		t.Spinning = true

	case model.StatusRunning:
		if d.Size > 0 {
			t.Determinate = true
			t.Fraction = float64(downloaded) / float64(d.Size)
			if t.Fraction > 1 {
				t.Fraction = 1
			}
			if pct, ok := format.Percent(downloaded, d.Size); ok {
				t.Status = fmt.Sprintf("%d%%", pct)
			}
			t.Context = format.Context(hostOf(d), transferDetail(downloaded, d.Size, bytesPerSecond))
		} else {
			// Unknown size: report what is known, never a percentage or ETA.
			t.Status = "Downloading"
			t.Spinning = true
			t.Context = format.Context(hostOf(d), unboundedDetail(downloaded, bytesPerSecond))
		}

	case model.StatusPaused:
		t.Status = "Paused"
		t.StatusIcon = iconPaused
		if d.Size > 0 {
			// The retained progress is real, not invented, so the bar keeps
			// showing how much of the file is already on disk.
			t.Determinate = true
			t.Fraction = float64(downloaded) / float64(d.Size)
			if t.Fraction > 1 {
				t.Fraction = 1
			}
		}
		t.Context = format.Context(hostOf(d), pausedDetail(downloaded, d.Size))

	case model.StatusCompleted:
		t.Status = "Completed"
		t.StatusIcon = iconCompleted
		if d.Size > 0 {
			t.Context = format.Context(hostOf(d), format.Bytes(d.Size))
		}

	case model.StatusFailed:
		t.Status = "Failed"
		t.StatusIcon = iconFailed
		short, detail := explainError(d.Error)
		t.Context = format.Context(hostOf(d), short)
		if detail != "" {
			t.Detail = detail
		}

	case model.StatusCanceled:
		t.Status = "Canceled"
		t.StatusIcon = iconCanceled
		t.Context = format.Context(hostOf(d), pausedDetail(downloaded, d.Size))
	}

	return t
}

// explainError turns a daemon error string into one short line for the row and
// the full text for a tooltip. Errors arrive from several layers (probe,
// download, filesystem), so the message is rewritten rather than shown raw.
func explainError(err string) (short, detail string) {
	clean := strings.TrimSpace(err)
	if clean == "" {
		return "", ""
	}
	detail = clean
	line := clean
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	lower := strings.ToLower(line)

	switch {
	case strings.Contains(lower, "disk quota exceeded"), strings.Contains(lower, "no space left"):
		return "Not enough disk space", detail
	case strings.Contains(lower, "returned http"):
		if i := strings.Index(lower, "returned http"); i >= 0 {
			code := strings.Fields(line[i+len("returned http"):])
			if len(code) > 0 {
				return "Server returned HTTP " + strings.Trim(code[0], "."), detail
			}
		}
		return "Server rejected the request", detail
	case strings.Contains(lower, "connection refused"):
		return "Server refused the connection", detail
	case strings.Contains(lower, "no such host"):
		return "Host not found", detail
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"):
		return "Timed out", detail
	case strings.Contains(lower, "permission denied"):
		return "Permission denied", detail
	case strings.Contains(lower, "connection reset"):
		return "Connection reset by the server", detail
	}

	line = strings.TrimPrefix(line, "probe: ")
	return clamp(line, 80), detail
}

// clamp shortens s to at most n runes, marking the cut.
func clamp(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "\u2026"
}

func sourceOf(d model.Download) string {
	if d.FinalURL != "" {
		return d.FinalURL
	}
	return d.URL
}

func hostOf(d model.Download) string {
	return format.Host(sourceOf(d))
}

// fileIcon picks a symbolic icon for the download's file type. It uses the
// filename extension only: no type sniffing, so the result is stable while a
// file is still being written and needs no extra daemon call.
func fileIcon(filename string) string {
	name := strings.ToLower(filename)
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 || dot == len(name)-1 {
		return "text-x-generic-symbolic"
	}
	switch name[dot+1:] {
	case "zip", "tar", "gz", "tgz", "bz2", "xz", "zst", "7z", "rar", "iso":
		return "package-x-generic-symbolic"
	case "mp4", "mkv", "avi", "mov", "webm", "m4v", "mpg", "mpeg", "ts":
		return "video-x-generic-symbolic"
	case "mp3", "flac", "ogg", "oga", "wav", "m4a", "opus", "aac":
		return "audio-x-generic-symbolic"
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "tif", "tiff", "avif":
		return "image-x-generic-symbolic"
	case "deb", "rpm", "appimage", "exe", "msi", "pkg", "flatpakref":
		return "application-x-executable-symbolic"
	default:
		return "text-x-generic-symbolic"
	}
}

// transferDetail describes a bounded transfer: bytes, rate, and ETA.
func transferDetail(downloaded, total int64, bytesPerSecond float64) string {
	detail := format.Bytes(downloaded) + " of " + format.Bytes(total)
	if bytesPerSecond >= 1 {
		detail = format.Context(detail, format.Rate(bytesPerSecond))
		if eta, ok := format.ETA(total-downloaded, bytesPerSecond); ok {
			detail = format.Context(detail, eta+" left")
		}
	}
	return detail
}

// unboundedDetail describes a transfer whose total size is unknown.
func unboundedDetail(downloaded int64, bytesPerSecond float64) string {
	parts := ""
	if downloaded > 0 {
		parts = format.Bytes(downloaded)
	}
	if bytesPerSecond >= 1 {
		parts = format.Context(parts, format.Rate(bytesPerSecond))
	}
	return parts
}

func pausedDetail(downloaded, total int64) string {
	if total > 0 {
		return format.Bytes(downloaded) + " of " + format.Bytes(total)
	}
	if downloaded > 0 {
		return format.Bytes(downloaded)
	}
	return ""
}
