// Package model defines the core types shared across the daemon: Download,
// Segment, Status, Event, Credentials, and Category.
package model

import "time"

// Download is one file the daemon is fetching, as seen by the API, the UI and
// the store. The Manager owns every live Download; other packages receive
// copies and must not treat them as shared state.
//
// The type is persisted by internal/store, so it carries only data that is
// safe and useful to keep across restarts. Request secrets (cookies, auth
// headers) live in Credentials instead and are never stored.
type Download struct {
	// ID is a random 128-bit identifier rendered as 32 lowercase hex
	// characters. It appears in URLs and is the store's primary key.
	ID string `json:"id"`

	// URL is the address the user asked for, before redirects.
	URL string `json:"url"`

	// FinalURL is the address that actually served the file, after any
	// redirect chain. Empty until probing succeeds. Resume uses it.
	FinalURL string `json:"finalUrl,omitempty"`

	// Filename is the sanitized base name to write. It never contains path
	// separators or ".." and is chosen from Content-Disposition first, then
	// the final URL path.
	Filename string `json:"filename"`

	// Dir is the absolute destination directory. The file is always
	// Dir/Filename; nothing may be written outside Dir.
	Dir string `json:"dir"`

	// CategoryID is the id of the category this download belongs to, or
	// empty when uncategorized.
	CategoryID string `json:"categoryId,omitempty"`

	// StartAt is the time at which the download should begin. When zero the
	// download starts immediately; when in the future it stays in
	// StatusScheduled until the time arrives, at which point the Manager
	// promotes it to StatusQueued.
	StartAt time.Time `json:"startAt,omitempty"`

	// Size is the total file size in bytes, or 0 when the server did not
	// report a length. A 0 Size forces single-stream download.
	Size int64 `json:"size"`

	// AcceptsRanges reports whether the server honours Range requests for
	// this resource. False forces single-stream download.
	AcceptsRanges bool `json:"acceptsRanges"`

	// ETag is the entity validator seen while probing, used to detect a
	// changed resource before resuming. Empty when absent.
	ETag string `json:"etag,omitempty"`

	// LastModified is the Last-Modified header seen while probing, used
	// only when ETag is unavailable. Empty when absent.
	LastModified string `json:"lastModified,omitempty"`

	// Status is the current lifecycle state, owned by the Manager.
	Status Status `json:"status"`

	// Segments is the current split of the file. Empty for single-stream
	// downloads with unknown size. Indexes are stable for the download's
	// lifetime; retries reuse a segment rather than renaming it.
	Segments []Segment `json:"segments,omitempty"`

	// Headers are extra request headers supplied by the user or extension.
	// They are memory-only and deliberately excluded from JSON so that
	// secrets such as Authorization never reach the UI or a log.
	Headers map[string]string `json:"-"`

	// Referrer is the referring page, sent as the Referer header. It is
	// also memory-only: it can carry session-identifying information.
	Referrer string `json:"-"`

	// CreatedAt is when the download was accepted, in UTC.
	CreatedAt time.Time `json:"createdAt"`

	// Error is a human-readable failure reason. Set only for StatusFailed;
	// cleared when the download is retried or resumed.
	Error string `json:"error,omitempty"`
}
