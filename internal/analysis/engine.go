// Package analysis implements the goroutine state engine and timeline construction.
package analysis

import (
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// Engine processes goroutine events and maintains timeline state.
type Engine struct {
	mu                sync.RWMutex
	session           *model.Session
	stateMachine      *StateMachine
	goroutines        map[int64]model.Goroutine
	closedSegments    []model.TimelineSegment
	activeSegments    map[int64]activeSegment
	edges             []model.ResourceEdge
	processorSegments []model.ProcessorSegment
	dataVersion       uint64 // incremented on any state change, for ETag

	subsMu      sync.Mutex
	subscribers map[chan struct{}]struct{}
}

type activeSegment struct {
	Start      time.Time
	State      model.GoroutineState
	Reason     model.BlockingReason
	ResourceID string
}

// NewEngine returns an empty, ready-to-use Engine.
func NewEngine() *Engine {
	return &Engine{
		stateMachine:   NewStateMachine(),
		goroutines:     make(map[int64]model.Goroutine),
		activeSegments: make(map[int64]activeSegment),
		subscribers:    make(map[chan struct{}]struct{}),
	}
}

// Subscribe returns a channel that receives a signal whenever the engine state
// is updated via LoadCapture. The caller must call Unsubscribe when done.
// The channel is buffered (capacity 1); slow consumers will miss intermediate
// updates but will never block the engine.
func (e *Engine) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	e.subsMu.Lock()
	e.subscribers[ch] = struct{}{}
	e.subsMu.Unlock()
	return ch
}

// Unsubscribe removes ch from the subscriber set and closes it.
func (e *Engine) Unsubscribe(ch chan struct{}) {
	e.subsMu.Lock()
	delete(e.subscribers, ch)
	e.subsMu.Unlock()
	close(ch)
}

// notifySubscribers sends a non-blocking signal to every subscriber.
func (e *Engine) notifySubscribers() {
	e.subsMu.Lock()
	defer e.subsMu.Unlock()
	for ch := range e.subscribers {
		select {
		case ch <- struct{}{}:
		default: // subscriber is busy; drop the extra tick, it will catch the next one
		}
	}
}

// Reset clears all state and initialises the engine for a new session.
func (e *Engine) Reset(session *model.Session) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.resetLocked(session)
}

// LoadCapture replaces the engine state with the provided capture snapshot.
func (e *Engine) LoadCapture(session *model.Session, capture model.Capture) {
	func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		e.resetLocked(session)
		e.applyEventsLocked(capture.Events)
		for _, snapshot := range capture.Stacks {
			e.applyStackSnapshotLocked(snapshot)
		}
		e.edges = append([]model.ResourceEdge(nil), capture.Resources...)
		e.processorSegments = append([]model.ProcessorSegment(nil), capture.ProcessorSegments...)

		// Apply parent IDs after all events and stacks so every goroutine is
		// already present in the map.  This handles the common case where the
		// create event was filtered by the trace parser (no user frame at spawn
		// time) and the engine never received an EventKindGoroutineCreate for
		// the goroutine.
		for goID, parentID := range capture.ParentIDs {
			if g, ok := e.goroutines[goID]; ok && g.ParentID == 0 {
				g.ParentID = parentID
				e.goroutines[goID] = g
			}
		}
	}()

	e.notifySubscribers()
}

// ApplyEvent processes a single event and updates goroutine state.
func (e *Engine) ApplyEvent(event model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyEventLocked(event)
}

// ApplyEvents processes a slice of events in order.
func (e *Engine) ApplyEvents(events []model.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyEventsLocked(events)
}

// ApplyStackSnapshot attaches a stack snapshot to the relevant goroutine.
func (e *Engine) ApplyStackSnapshot(snapshot model.StackSnapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyStackSnapshotLocked(snapshot)
}

// SetResourceGraph replaces the current resource edge set.
func (e *Engine) SetResourceGraph(edges []model.ResourceEdge) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.edges = append([]model.ResourceEdge(nil), edges...)
	e.dataVersion++
}

// DataVersion returns a monotonic version that changes whenever engine state changes.
// Used for ETag / conditional requests.
func (e *Engine) DataVersion() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dataVersion
}

// CurrentSession returns the active session, or nil if none is set.
func (e *Engine) CurrentSession() *model.Session {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.session.Clone()
}

// ListGoroutines returns all tracked goroutines sorted by ID.
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

// GetGoroutine returns the goroutine with the given ID, or false if not found.
func (e *Engine) GetGoroutine(id int64) (model.Goroutine, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	goroutine, ok := e.goroutines[id]
	if !ok {
		return model.Goroutine{}, false
	}

	return cloneGoroutine(goroutine), true
}

// Timeline returns all closed and open timeline segments.
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

// ProcessorTimeline returns a snapshot of the processor-segment log. Each
// segment records an interval during which a specific goroutine ran on a
// specific logical processor (P).
func (e *Engine) ProcessorTimeline() []model.ProcessorSegment {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.ProcessorSegment, len(e.processorSegments))
	copy(out, e.processorSegments)
	return out
}

// ResourceGraph returns the current set of resource dependency edges.
func (e *Engine) ResourceGraph() []model.ResourceEdge {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.ResourceEdge, len(e.edges))
	copy(out, e.edges)
	return out
}

func (e *Engine) resetLocked(session *model.Session) {
	e.session = session.Clone()
	e.goroutines = make(map[int64]model.Goroutine)
	e.closedSegments = nil
	e.activeSegments = make(map[int64]activeSegment)
	e.edges = nil
	e.processorSegments = nil
	e.dataVersion++
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

	// Normalize events that arrive with incomplete fields (e.g. via ApplyEvent
	// from an external agent). Trace-sourced events already have these set, so
	// this is a no-op for the happy path.
	if event.Kind == model.EventKindGoroutineState && event.State == "" {
		event.State = model.StateWaiting
	}
	if isWaitState(event.State) && event.Reason == "" {
		event.Reason = model.ReasonUnknown
	}

	current := e.goroutines[event.GoroutineID]
	next := e.stateMachine.Apply(current, event)
	// Lock in the creator identity on the first create event. Subsequent
	// state-change events keep ParentID via the next := current copy in
	// StateMachine.Apply, so we only need to act when the field is still zero.
	if event.Kind == model.EventKindGoroutineCreate && event.ParentID != 0 && next.ParentID == 0 {
		next.ParentID = event.ParentID
	}
	e.updateSegmentsLocked(event.GoroutineID, current, next, event)
	e.goroutines[next.ID] = next
	e.dataVersion++
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
	e.dataVersion++
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
