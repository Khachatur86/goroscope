package model

import "time"

type Goroutine struct {
	ID         int64             `json:"goroutine_id"`
	State      GoroutineState    `json:"state"`
	Reason     BlockingReason    `json:"reason,omitempty"`
	ResourceID string            `json:"resource_id,omitempty"`
	WaitNS     int64             `json:"wait_ns,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	LastSeenAt time.Time         `json:"last_seen_at"`
	LastStack  *StackSnapshot    `json:"last_stack,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}
