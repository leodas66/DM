package model

// Status is the lifecycle state of a download.
//
// The Manager is the only component that changes it. Valid transitions:
//
//	queued    -> probing | canceled
//	probing   -> running | failed | canceled
//	running   -> paused | completed | failed | canceled
//	paused    -> queued (on resume) | canceled
//	completed, failed, canceled are terminal.
type Status string

const (
	// StatusQueued means the download is accepted but not yet started.
	StatusQueued Status = "queued"
	// StatusProbing means the daemon is inspecting the remote resource
	// (size, range support, filename, validators).
	StatusProbing Status = "probing"
	// StatusRunning means at least one segment worker is transferring.
	StatusRunning Status = "running"
	// StatusPaused means the download is stopped with its partial data kept
	// on disk and in the store, ready to resume.
	StatusPaused Status = "paused"
	// StatusCompleted means every byte of the file has been written and
	// verified against the expected size.
	StatusCompleted Status = "completed"
	// StatusFailed means the download stopped on an error that retrying
	// will not fix on its own (fatal HTTP status, unrecoverable probe
	// failure, disk error). Error explains why.
	StatusFailed Status = "failed"
	// StatusCanceled means the user stopped the download. Partial data may
	// still exist on disk until the download is removed.
	StatusCanceled Status = "canceled"
)

// SegmentStatus is the lifecycle state of a single segment worker.
// Segment workers report it upward; they never read it from shared memory.
type SegmentStatus string

const (
	// SegmentPending means the range has not been requested yet.
	SegmentPending SegmentStatus = "pending"
	// SegmentRunning means a worker is currently transferring this range.
	SegmentRunning SegmentStatus = "running"
	// SegmentDone means every byte of the range has been written.
	SegmentDone SegmentStatus = "done"
	// SegmentFailed means the worker gave up on this range. The download
	// does not necessarily fail: a retry may reassign the range.
	SegmentFailed SegmentStatus = "failed"
)
