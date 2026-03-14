package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

type Manager struct {
	mu      sync.RWMutex
	nextID  uint64
	current *model.Session
}

func NewManager() *Manager {
	return &Manager{}
}

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

func (m *Manager) CompleteCurrent() {
	m.finishCurrent(model.SessionStatusCompleted, "")
}

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
}

func (m *Manager) Current() *model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.current.Clone()
}
