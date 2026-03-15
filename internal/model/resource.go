package model

// ResourceEdge describes a dependency relationship between two goroutines via a shared resource.
type ResourceEdge struct {
	FromGoroutineID int64  `json:"from_goroutine_id"`
	ToGoroutineID   int64  `json:"to_goroutine_id"`
	ResourceID      string `json:"resource_id"`
	Kind            string `json:"kind"`
}
