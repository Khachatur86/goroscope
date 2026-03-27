// Package model defines the core domain types shared across goroscope packages.
package model

// Annotation is a user-supplied note attached to a specific point in time
// within a capture. Stored inside the .gtrace file; loaded by the UI as
// named timeline bookmarks.
type Annotation struct {
	ID     string `json:"id"`
	TimeNS int64  `json:"time_ns"`
	Note   string `json:"note"`
}

// Capture is a point-in-time snapshot of goroutine events and stack traces.
type Capture struct {
	Name      string          `json:"name"`
	Target    string          `json:"target,omitempty"`
	Events    []Event         `json:"events"`
	Stacks    []StackSnapshot `json:"stacks,omitempty"`
	Resources []ResourceEdge  `json:"resources,omitempty"`
	// ParentIDs maps goroutine ID → creator goroutine ID, populated by the
	// trace parser from GoID= fields on NotExist→* transitions. Stored
	// separately because the create event itself is often filtered out (it
	// arrives before the goroutine has any user-frame stack), yet the
	// parent-child relationship still needs to reach the engine.
	ParentIDs map[int64]int64 `json:"parent_ids,omitempty"`
	// ProcessorSegments records intervals when a goroutine ran on a specific
	// logical processor (P).  Populated by the trace parser from P= fields.
	ProcessorSegments []ProcessorSegment `json:"processor_segments,omitempty"`
	// LabelOverrides merges into goroutine Labels (e.g. from agent.WithRequestID).
	// Keys are goroutine IDs; values are label key-value pairs to merge.
	LabelOverrides map[int64]Labels `json:"label_overrides,omitempty"`
	// Annotations are user-supplied notes added via `goroscope annotate`.
	// They are stored in the .gtrace file and loaded by the UI as named bookmarks.
	Annotations []Annotation `json:"annotations,omitempty"`
}
