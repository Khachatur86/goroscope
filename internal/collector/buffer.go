package collector

import (
	"sync"

	"github.com/Khachatur86/goroscope/internal/model"
)

type Buffer struct {
	mu     sync.Mutex
	max    int
	events []model.Event
}

func NewBuffer(max int) *Buffer {
	if max <= 0 {
		max = 1024
	}

	return &Buffer{
		max:    max,
		events: make([]model.Event, 0, max),
	}
}

func (b *Buffer) Add(event model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.events) == b.max {
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = event
		return
	}

	b.events = append(b.events, event)
}

func (b *Buffer) List() []model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]model.Event, len(b.events))
	copy(out, b.events)
	return out
}
