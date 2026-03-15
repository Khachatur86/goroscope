package model

// ProcessorSegment records an interval when a specific goroutine ran on a
// logical processor (P).  It is derived from the P= field emitted by
// "go tool trace -d=parsed" and populated by the trace parser.
//
// ProcessorID values are zero-based indices up to GOMAXPROCS-1.  A value of
// -1 means the goroutine ran without a P (rare system/GC contexts) and such
// segments are not emitted.
type ProcessorSegment struct {
	ProcessorID int   `json:"processor_id"`
	GoroutineID int64 `json:"goroutine_id"`
	StartNS     int64 `json:"start_ns"`
	EndNS       int64 `json:"end_ns"`
}
