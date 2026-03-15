// Package model defines the core domain types shared across goroscope packages.
package model

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
}
