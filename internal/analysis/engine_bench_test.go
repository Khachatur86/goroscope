package analysis

import (
	"fmt"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func genEvents(nGoroutines int) []model.Event {
	base := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	events := make([]model.Event, 0, nGoroutines*4+2)

	events = append(events,
		model.Event{Seq: 1, Kind: model.EventKindGoroutineCreate, GoroutineID: 1, Timestamp: base},
		model.Event{Seq: 2, Kind: model.EventKindGoroutineStart, GoroutineID: 1, Timestamp: base},
	)

	seq := uint64(3)
	for i := 2; i <= nGoroutines; i++ {
		ts := base.Add(time.Duration(i*10) * time.Millisecond)
		events = append(events,
			model.Event{Seq: seq, Kind: model.EventKindGoroutineCreate, GoroutineID: int64(i), ParentID: 1, Timestamp: ts},
			model.Event{Seq: seq + 1, Kind: model.EventKindGoroutineStart, GoroutineID: int64(i), Timestamp: ts},
			model.Event{Seq: seq + 2, Kind: model.EventKindGoroutineState, GoroutineID: int64(i), State: model.StateBlocked, Reason: model.ReasonChanRecv, Timestamp: ts.Add(50 * time.Millisecond)},
			model.Event{Seq: seq + 3, Kind: model.EventKindGoroutineState, GoroutineID: int64(i), State: model.StateRunning, Timestamp: ts.Add(100 * time.Millisecond)},
		)
		seq += 4
	}

	return events
}

func BenchmarkEngineLoadCapture(b *testing.B) {
	sizes := []int{100, 1000, 5000, 10000}
	for _, n := range sizes {
		n := n
		events := genEvents(n)
		capture := model.Capture{
			Name:   "bench",
			Events: events,
		}
		session := &model.Session{ID: "sess", Name: "bench", Target: "bench", Status: model.SessionStatusRunning, StartedAt: time.Now()}

		b.Run(fmt.Sprintf("goroutines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				engine := NewEngine()
				engine.LoadCapture(session, capture)
			}
		})
	}
}

func BenchmarkEngineListGoroutines(b *testing.B) {
	sizes := []int{100, 1000, 10000}
	for _, n := range sizes {
		n := n
		events := genEvents(n)
		capture := model.Capture{Name: "bench", Events: events}
		session := &model.Session{ID: "sess", Name: "bench", Target: "bench", Status: model.SessionStatusRunning, StartedAt: time.Now()}

		engine := NewEngine()
		engine.LoadCapture(session, capture)

		b.Run(fmt.Sprintf("goroutines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = engine.ListGoroutines()
			}
		})
	}
}
