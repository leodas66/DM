package model

// Segment is one byte range of a download and the progress made on it.
//
// Start and End are inclusive HTTP byte offsets, so a segment covers
// End-Start+1 bytes. Downloaded counts bytes written by the worker for this
// range; it never exceeds End-Start+1. For a single-stream download that
// cannot be split, the download has exactly one segment covering the whole
// file, or none at all when the remote size is unknown.
type Segment struct {
	Index      int           `json:"index"`
	Start      int64         `json:"start"`
	End        int64         `json:"end"`
	Downloaded int64         `json:"downloaded"`
	Status     SegmentStatus `json:"status"`
}
