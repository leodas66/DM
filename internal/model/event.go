package model

import "time"

// EventType identifies what changed. It is the discriminator in the JSON
// frames sent over /api/ws, so the string values are part of the wire
// contract with every client and must not be renamed casually.
type EventType string

const (
	// EventAdded means a download was accepted into the queue. Download is
	// the full new record.
	EventAdded EventType = "added"

	// EventProgress means byte counters changed. Download carries the
	// current per-segment Downloaded values; clients should use it to
	// replace, not accumulate, their copy.
	EventProgress EventType = "progress"

	// EventStatusChanged means Status, Error, or the segment set changed.
	// Download is the full record after the change.
	EventStatusChanged EventType = "status"

	// EventRemoved means the download left the list. Download is nil and
	// DownloadID identifies the record; the file may or may not still exist
	// on disk, depending on how it was removed.
	EventRemoved EventType = "removed"
)

// Event is one message on the Manager's event bus, broadcast to every
// connected WebSocket client. It is a flat struct rather than an interface so
// that clients can unmarshal it without a type registry.
type Event struct {
	// Type is the kind of change being reported.
	Type EventType `json:"type"`

	// Download is the affected record. Nil for EventRemoved.
	Download *Download `json:"download,omitempty"`

	// DownloadID is always set, even when Download is nil.
	DownloadID string `json:"downloadId"`

	// At is when the Manager emitted the event, in UTC.
	At time.Time `json:"at"`
}
