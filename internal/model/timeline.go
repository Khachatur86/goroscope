package model

type TimelineSegment struct {
	GoroutineID int64          `json:"goroutine_id"`
	StartNS     int64          `json:"start_ns"`
	EndNS       int64          `json:"end_ns"`
	State       GoroutineState `json:"state"`
	Reason      BlockingReason `json:"reason,omitempty"`
	ResourceID  string         `json:"resource_id,omitempty"`
}
