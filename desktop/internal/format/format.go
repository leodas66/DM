// Package format renders byte counts, rates, percentages, and durations for
// the download rows. It is pure: no GTK, no locale database, no app styling.
package format

import (
	"fmt"
	"math"
	"net/url"
	"strings"
)

// Bytes renders a byte count using binary units, e.g. "4.0 MiB". Counts below
// 1 KiB stay exact ("512 B"). Negative values are treated as zero, because a
// negative size means "unknown" on the wire.
func Bytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Rate renders a transfer speed, e.g. "2.3 MiB/s". Speeds under 1 B/s render
// as "0 B/s" instead of a negative or fractional value.
func Rate(bytesPerSecond float64) string {
	if bytesPerSecond < 1 {
		return "0 B/s"
	}
	return Bytes(int64(math.Round(bytesPerSecond))) + "/s"
}

// Percent returns the whole percentage of done out of total and whether it is
// meaningful. total must be positive: an unknown size has no percentage.
func Percent(done, total int64) (int, bool) {
	if total <= 0 {
		return 0, false
	}
	pct := int(math.Round(float64(done) / float64(total) * 100))
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// ETA renders the time left at the current rate. It reports false when there
// is nothing honest to show: unknown total, zero rate, or nothing remaining.
func ETA(remaining int64, bytesPerSecond float64) (string, bool) {
	if remaining <= 0 || bytesPerSecond < 1 {
		return "", false
	}
	seconds := float64(remaining) / bytesPerSecond
	return Duration(int64(math.Round(seconds))), true
}

// Duration renders a number of seconds compactly: "12 s", "4 min 5 s",
// "2 h 10 min".
func Duration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("%d s", seconds)
	case seconds < 3600:
		m, s := seconds/60, seconds%60
		if s == 0 {
			return fmt.Sprintf("%d min", m)
		}
		return fmt.Sprintf("%d min %d s", m, s)
	default:
		h, m := seconds/3600, (seconds%3600)/60
		if m == 0 {
			return fmt.Sprintf("%d h", h)
		}
		return fmt.Sprintf("%d h %d min", h, m)
	}
}

// Host shortens a URL to its host for row context. It never returns an empty
// string: unparseable input is shown as-is so the user still sees something.
func Host(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	return u.Host
}

// joinContext joins non-empty parts with a middle dot separator.
func joinContext(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " \u00b7 ")
}

// Context renders the secondary row line: source host plus whatever transfer
// detail the current state can honestly report.
func Context(host, detail string) string {
	return joinContext(host, detail)
}
