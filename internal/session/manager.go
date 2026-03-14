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

	return cloneSession(m.current)
}

func (m *Manager) CompleteCurrent() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == nil {
		return
	}

	now := time.Now().UTC()
	m.current.Status = model.SessionStatusCompleted
	m.current.EndedAt = &now
}

func (m *Manager) Current() *model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneSession(m.current)
}

func cloneSession(session *model.Session) *model.Session {
	if session == nil {
		return nil
	}

	copy := *session
	if session.EndedAt != nil {
		endedAt := *session.EndedAt
		copy.EndedAt = &endedAt
	}

	return &copy
}
