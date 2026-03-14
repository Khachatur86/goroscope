package analysis

import (
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

type Engine struct {
	mu             sync.RWMutex
	session        *model.Session
	stateMachine   *StateMachine
	goroutines     map[int64]model.Goroutine
	closedSegments []model.TimelineSegment
	activeSegments map[int64]activeSegment
	edges          []model.ResourceEdge
}

type activeSegment struct {
	Start      time.Time
	State      model.GoroutineState
	Reason     model.BlockingReason
	ResourceID string
}

type demoSessionData struct {
	events []model.Event
	stacks []model.StackSnapshot
	edges  []model.ResourceEdge
}

func NewEngine() *Engine {
	return &Engine{
		stateMachine:   NewStateMachine(),
		goroutines:     make(map[int64]model.Goroutine),
		activeSegments: make(map[int64]activeSegment),
	}
}

func (e *Engine) Reset(session *model.Session) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.resetLocked(session)
}

func (e *Engine) SeedDemoSession(session *model.Session) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.resetLocked(session)

	data := buildDemoSessionData(session)
	e.applyEventsLocked(data.events)
	for _, snapshot := range data.stacks {
		e.applyStackSnapshotLocked(snapshot)
	}
	e.edges = append([]model.ResourceEdge(nil), data.edges...)
}

func (e *Engine) ApplyEvent(event model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyEventLocked(event)
}

func (e *Engine) ApplyEvents(events []model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyEventsLocked(events)
}

func (e *Engine) ApplyStackSnapshot(snapshot model.StackSnapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyStackSnapshotLocked(snapshot)
}

func (e *Engine) SetResourceGraph(edges []model.ResourceEdge) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.edges = append([]model.ResourceEdge(nil), edges...)
}

func (e *Engine) CurrentSession() *model.Session {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return cloneSession(e.session)
}

func (e *Engine) ListGoroutines() []model.Goroutine {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ids := make([]int64, 0, len(e.goroutines))
	for id := range e.goroutines {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make([]model.Goroutine, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneGoroutine(e.goroutines[id]))
	}

	return out
}

func (e *Engine) GetGoroutine(id int64) (model.Goroutine, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	goroutine, ok := e.goroutines[id]
	if !ok {
		return model.Goroutine{}, false
	}

	return cloneGoroutine(goroutine), true
}

func (e *Engine) Timeline() []model.TimelineSegment {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.TimelineSegment, 0, len(e.closedSegments)+len(e.activeSegments))
	out = append(out, e.closedSegments...)

	for goroutineID, segment := range e.activeSegments {
		goroutine, ok := e.goroutines[goroutineID]
		if !ok {
			continue
		}

		if derived, ok := buildTimelineSegment(goroutineID, segment, goroutine.LastSeenAt); ok {
			out = append(out, derived)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].GoroutineID != out[j].GoroutineID {
			return out[i].GoroutineID < out[j].GoroutineID
		}
		if out[i].StartNS != out[j].StartNS {
			return out[i].StartNS < out[j].StartNS
		}
		return out[i].EndNS < out[j].EndNS
	})

	return out
}

func (e *Engine) ResourceGraph() []model.ResourceEdge {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.ResourceEdge, len(e.edges))
	copy(out, e.edges)
	return out
}

func (e *Engine) resetLocked(session *model.Session) {
	e.session = cloneSession(session)
	e.goroutines = make(map[int64]model.Goroutine)
	e.closedSegments = nil
	e.activeSegments = make(map[int64]activeSegment)
	e.edges = nil
}

func (e *Engine) applyEventsLocked(events []model.Event) {
	for _, event := range events {
		e.applyEventLocked(event)
	}
}

func (e *Engine) applyEventLocked(event model.Event) {
	if event.GoroutineID == 0 {
		return
	}

	current := e.goroutines[event.GoroutineID]
	next := e.stateMachine.Apply(current, event)
	e.updateSegmentsLocked(event.GoroutineID, current, next, event)
	e.goroutines[next.ID] = next
}

func (e *Engine) applyStackSnapshotLocked(snapshot model.StackSnapshot) {
	if snapshot.GoroutineID == 0 {
		return
	}

	goroutine := e.goroutines[snapshot.GoroutineID]
	if goroutine.ID == 0 {
		goroutine.ID = snapshot.GoroutineID
		if !snapshot.Timestamp.IsZero() {
			goroutine.CreatedAt = snapshot.Timestamp
			goroutine.LastSeenAt = snapshot.Timestamp
		}
	}

	if snapshot.Timestamp.After(goroutine.LastSeenAt) {
		goroutine.LastSeenAt = snapshot.Timestamp
	}

	stackCopy := snapshot
	stackCopy.Frames = append([]model.StackFrame(nil), snapshot.Frames...)
	goroutine.LastStack = &stackCopy
	e.goroutines[goroutine.ID] = goroutine
}

func (e *Engine) updateSegmentsLocked(id int64, current, next model.Goroutine, event model.Event) {
	if !isTimelineEventKind(event.Kind) || event.Timestamp.IsZero() {
		return
	}

	active, hasActive := e.activeSegments[id]
	if hasActive && segmentMatchesGoroutine(active, current) {
		if current.State != next.State || current.Reason != next.Reason || current.ResourceID != next.ResourceID {
			if segment, ok := buildTimelineSegment(id, active, event.Timestamp); ok {
				e.closedSegments = append(e.closedSegments, segment)
			}
			delete(e.activeSegments, id)
			hasActive = false
		}
	}

	if hasActive {
		return
	}

	if !shouldTrackState(next.State) {
		return
	}

	e.activeSegments[id] = activeSegment{
		Start:      event.Timestamp,
		State:      next.State,
		Reason:     next.Reason,
		ResourceID: next.ResourceID,
	}
}

func isTimelineEventKind(kind model.EventKind) bool {
	switch kind {
	case model.EventKindGoroutineCreate, model.EventKindGoroutineStart, model.EventKindGoroutineState, model.EventKindGoroutineEnd:
		return true
	default:
		return false
	}
}

func shouldTrackState(state model.GoroutineState) bool {
	return state != "" && state != model.StateDone
}

func segmentMatchesGoroutine(segment activeSegment, goroutine model.Goroutine) bool {
	return segment.State == goroutine.State &&
		segment.Reason == goroutine.Reason &&
		segment.ResourceID == goroutine.ResourceID
}

func buildTimelineSegment(goroutineID int64, segment activeSegment, end time.Time) (model.TimelineSegment, bool) {
	if segment.Start.IsZero() || end.IsZero() || !end.After(segment.Start) {
		return model.TimelineSegment{}, false
	}

	return model.TimelineSegment{
		GoroutineID: goroutineID,
		StartNS:     segment.Start.UnixNano(),
		EndNS:       end.UnixNano(),
		State:       segment.State,
		Reason:      segment.Reason,
		ResourceID:  segment.ResourceID,
	}, true
}

func buildDemoSessionData(session *model.Session) demoSessionData {
	base := time.Now().UTC().Add(-2 * time.Second)

	return demoSessionData{
		events: []model.Event{
			{
				SessionID:   session.ID,
				Seq:         1,
				Timestamp:   base,
				Kind:        model.EventKindGoroutineCreate,
				GoroutineID: 1,
				Labels:      model.Labels{"function": "main.producer"},
			},
			{
				SessionID:   session.ID,
				Seq:         2,
				Timestamp:   base,
				Kind:        model.EventKindGoroutineStart,
				GoroutineID: 1,
			},
			{
				SessionID:   session.ID,
				Seq:         3,
				Timestamp:   base.Add(800 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 1,
				State:       model.StateRunnable,
			},
			{
				SessionID:   session.ID,
				Seq:         4,
				Timestamp:   base.Add(50 * time.Millisecond),
				Kind:        model.EventKindGoroutineCreate,
				GoroutineID: 42,
				Labels:      model.Labels{"function": "main.worker"},
			},
			{
				SessionID:   session.ID,
				Seq:         5,
				Timestamp:   base.Add(100 * time.Millisecond),
				Kind:        model.EventKindGoroutineStart,
				GoroutineID: 42,
			},
			{
				SessionID:   session.ID,
				Seq:         6,
				Timestamp:   base.Add(900 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 42,
				State:       model.StateBlocked,
				Reason:      model.ReasonChanRecv,
				ResourceID:  "chan:0xc000018230",
			},
			{
				SessionID:   session.ID,
				Seq:         7,
				Timestamp:   base.Add(1600 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 42,
				State:       model.StateRunning,
			},
			{
				SessionID:   session.ID,
				Seq:         8,
				Timestamp:   base.Add(1770 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 42,
				State:       model.StateBlocked,
				Reason:      model.ReasonChanRecv,
				ResourceID:  "chan:0xc000018230",
			},
			{
				SessionID:   session.ID,
				Seq:         9,
				Timestamp:   base.Add(2 * time.Second),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 42,
				State:       model.StateBlocked,
				Reason:      model.ReasonChanRecv,
				ResourceID:  "chan:0xc000018230",
			},
			{
				SessionID:   session.ID,
				Seq:         10,
				Timestamp:   base.Add(100 * time.Millisecond),
				Kind:        model.EventKindGoroutineCreate,
				GoroutineID: 77,
				Labels:      model.Labels{"function": "main.sink"},
			},
			{
				SessionID:   session.ID,
				Seq:         11,
				Timestamp:   base.Add(100 * time.Millisecond),
				Kind:        model.EventKindGoroutineStart,
				GoroutineID: 77,
			},
			{
				SessionID:   session.ID,
				Seq:         12,
				Timestamp:   base.Add(200 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 77,
				State:       model.StateWaiting,
				Reason:      model.ReasonMutexLock,
				ResourceID:  "mutex:0xc000014180",
			},
			{
				SessionID:   session.ID,
				Seq:         13,
				Timestamp:   base.Add(1300 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 77,
				State:       model.StateSyscall,
				Reason:      model.ReasonSyscall,
			},
			{
				SessionID:   session.ID,
				Seq:         14,
				Timestamp:   base.Add(1890 * time.Millisecond),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 77,
				State:       model.StateWaiting,
				Reason:      model.ReasonMutexLock,
				ResourceID:  "mutex:0xc000014180",
			},
			{
				SessionID:   session.ID,
				Seq:         15,
				Timestamp:   base.Add(2 * time.Second),
				Kind:        model.EventKindGoroutineState,
				GoroutineID: 77,
				State:       model.StateWaiting,
				Reason:      model.ReasonMutexLock,
				ResourceID:  "mutex:0xc000014180",
			},
		},
		stacks: []model.StackSnapshot{
			{
				SessionID:   session.ID,
				Seq:         16,
				Timestamp:   base.Add(900 * time.Millisecond),
				StackID:     "stk_producer",
				GoroutineID: 1,
				Frames: []model.StackFrame{
					{Func: "main.producer", File: "/workspace/app/main.go", Line: 24},
					{Func: "main.main", File: "/workspace/app/main.go", Line: 88},
				},
			},
			{
				SessionID:   session.ID,
				Seq:         17,
				Timestamp:   base.Add(2 * time.Second),
				StackID:     "stk_worker",
				GoroutineID: 42,
				Frames: []model.StackFrame{
					{Func: "main.worker", File: "/workspace/app/main.go", Line: 57},
					{Func: "main.main", File: "/workspace/app/main.go", Line: 92},
				},
			},
			{
				SessionID:   session.ID,
				Seq:         18,
				Timestamp:   base.Add(2 * time.Second),
				StackID:     "stk_sink",
				GoroutineID: 77,
				Frames: []model.StackFrame{
					{Func: "main.sink", File: "/workspace/app/main.go", Line: 73},
					{Func: "main.main", File: "/workspace/app/main.go", Line: 95},
				},
			},
		},
		edges: []model.ResourceEdge{
			{FromGoroutineID: 1, ToGoroutineID: 42, ResourceID: "chan:0xc000018230", Kind: "channel"},
			{FromGoroutineID: 42, ToGoroutineID: 77, ResourceID: "mutex:0xc000014180", Kind: "mutex"},
		},
	}
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

func cloneGoroutine(in model.Goroutine) model.Goroutine {
	out := in

	if in.LastStack != nil {
		stackCopy := *in.LastStack
		stackCopy.Frames = append([]model.StackFrame(nil), in.LastStack.Frames...)
		out.LastStack = &stackCopy
	}

	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		for key, value := range in.Labels {
			out.Labels[key] = value
		}
	}

	return out
}
