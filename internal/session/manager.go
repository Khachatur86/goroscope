// Package session manages the lifecycle of goroscope tracing sessions.
package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// Manager tracks the current and past tracing sessions.
type Manager struct {
	mu      sync.RWMutex
	nextID  uint64
	current *model.Session
	history []*model.Session
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{}
}

// StartSession creates and activates a new session with the given name and target.
func (m *Manager) StartSession(name, target string) *model.Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	m.current = &model.Session{
		ID:        fmt.Sprintf("sess_%06d", m.nextID),
		Name:      name,
		Target:    target,
		Status:    model.SessionStatusRunning,
		StartedAt: time.Now().UTC(),
	}

	return m.current.Clone()
}

// CompleteCurrent marks the active session as completed.
func (m *Manager) CompleteCurrent() {
	m.finishCurrent(model.SessionStatusCompleted, "")
}

// FailCurrent marks the active session as failed with the given error message.
func (m *Manager) FailCurrent(message string) {
	m.finishCurrent(model.SessionStatusFailed, message)
}

func (m *Manager) finishCurrent(status model.SessionStatus, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == nil {
		return
	}

	now := time.Now().UTC()
	m.current.Status = status
	m.current.EndedAt = &now
	m.current.Error = message

	m.history = append(m.history, m.current.Clone())
}

// Current returns a clone of the active session, or nil if none exists.
func (m *Manager) Current() *model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.current.Clone()
}

// History returns clones of all completed or failed sessions, oldest first.
func (m *Manager) History() []*model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*model.Session, len(m.history))
	for i, s := range m.history {
		out[i] = s.Clone()
	}

	return out
}
