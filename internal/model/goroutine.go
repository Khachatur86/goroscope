package model

import "time"

// Goroutine holds the current state and metadata for a tracked goroutine.
type Goroutine struct {
	ID int64 `json:"goroutine_id"`
	// ParentID is the goroutine that spawned this one via a "go" statement.
	// Zero means it was the root goroutine (G1) or the origin is unknown.
	ParentID   int64          `json:"parent_id,omitempty"`
	State      GoroutineState `json:"state"`
	Reason     BlockingReason `json:"reason,omitempty"`
	ResourceID string         `json:"resource_id,omitempty"`
	WaitNS     int64          `json:"wait_ns,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	LastSeenAt time.Time      `json:"last_seen_at"`
	// BornNS is the nanosecond timestamp when the goroutine was first observed.
	// Zero means the birth time is unknown (e.g. goroutine predates the trace).
	BornNS int64 `json:"born_ns,omitempty"`
	// DiedNS is the nanosecond timestamp when the goroutine reached DONE state.
	// Zero means the goroutine is still alive or the death time is unknown.
	DiedNS    int64             `json:"died_ns,omitempty"`
	IsAlive   bool              `json:"is_alive"`
	LastStack *StackSnapshot    `json:"last_stack,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}
