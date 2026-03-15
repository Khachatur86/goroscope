package model

import "time"

// SessionStatus indicates the lifecycle state of a tracing session.
type SessionStatus string

// Session status constants.
const (
	SessionStatusRunning   SessionStatus = "RUNNING"
	SessionStatusCompleted SessionStatus = "COMPLETED"
	SessionStatusFailed    SessionStatus = "FAILED"
)

// Session represents a single goroscope tracing session.
type Session struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Target    string        `json:"target"`
	Status    SessionStatus `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   *time.Time    `json:"ended_at,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Clone returns a deep copy of s. If s is nil, Clone returns nil.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}

	out := *s
	if s.EndedAt != nil {
		endedAt := *s.EndedAt
		out.EndedAt = &endedAt
	}

	return &out
}
