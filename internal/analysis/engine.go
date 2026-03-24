// Package analysis implements the goroutine state engine and timeline construction.
package analysis

import (
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// RetentionPolicy bounds the amount of history the Engine retains in memory.
// Zero values disable the corresponding limit.
type RetentionPolicy struct {
	// MaxClosedSegments caps the total number of closed timeline segments.
	// When the cap is exceeded, the oldest segments are evicted first.
	// A value of 0 means unlimited. Default: 500 000.
	MaxClosedSegments int
	// MaxStacksPerGoroutine caps the number of historical stack snapshots
	// retained per goroutine. When the cap is exceeded, the oldest snapshots
	// for that goroutine are evicted.
	// A value of 0 means unlimited. Default: 200.
	MaxStacksPerGoroutine int
}

// DefaultRetentionPolicy returns a RetentionPolicy with sensible defaults
// that keep memory usage well below 256 MB for most workloads.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		MaxClosedSegments:     500_000,
		MaxStacksPerGoroutine: 200,
	}
}

// Option is a functional option for NewEngine.
type Option func(*Engine)

// WithRetention configures the engine's data-retention policy.
// Use DefaultRetentionPolicy() for production defaults.
// Pass a zero-value RetentionPolicy to disable all limits.
func WithRetention(p RetentionPolicy) Option {
	return func(e *Engine) {
		e.retention = p
	}
}

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
	dataVersion       uint64                // incremented on any state change, for ETag
	stacks            []model.StackSnapshot // historical stacks for stack-at-segment lookup

	retention RetentionPolicy

	subsMu      sync.Mutex
	subscribers map[chan struct{}]struct{}
}

type activeSegment struct {
	Start      time.Time
	State      model.GoroutineState
	Reason     model.BlockingReason
	ResourceID string
}

// MemoryStats reports the current in-memory data volumes held by the Engine.
// All counts are point-in-time snapshots taken under the read lock.
type MemoryStats struct {
	// ClosedSegments is the number of completed timeline segments retained.
	ClosedSegments int `json:"closed_segments"`
	// ActiveSegments is the number of currently open (in-progress) segments.
	ActiveSegments int `json:"active_segments"`
	// Goroutines is the number of goroutines tracked.
	Goroutines int `json:"goroutines"`
	// StackSnapshots is the total number of historical stack snapshots retained.
	StackSnapshots int `json:"stack_snapshots"`
	// ProcessorSegments is the number of GMP processor segments retained.
	ProcessorSegments int `json:"processor_segments"`
	// MaxClosedSegments is the configured cap (0 = unlimited).
	MaxClosedSegments int `json:"max_closed_segments"`
	// MaxStacksPerGoroutine is the configured cap (0 = unlimited).
	MaxStacksPerGoroutine int `json:"max_stacks_per_goroutine"`
}

// NewEngine returns an empty, ready-to-use Engine configured with the given
// options. If no WithRetention option is supplied, DefaultRetentionPolicy is used.
func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		stateMachine:   NewStateMachine(),
		goroutines:     make(map[int64]model.Goroutine),
		activeSegments: make(map[int64]activeSegment),
		subscribers:    make(map[chan struct{}]struct{}),
		retention:      DefaultRetentionPolicy(),
	}
	for _, o := range opts {
		o(e)
	}
	return e
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
			e.applyStackSnapshotLocked(snapshot, false)
		}
		e.stacks = append([]model.StackSnapshot(nil), capture.Stacks...)
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
		// Merge label overrides (e.g. from agent.WithRequestID).
		for goID, labels := range capture.LabelOverrides {
			if g, ok := e.goroutines[goID]; ok && len(labels) > 0 {
				if g.Labels == nil {
					g.Labels = make(map[string]string, len(labels))
				}
				for k, v := range labels {
					g.Labels[k] = v
				}
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

	e.applyStackSnapshotLocked(snapshot, true)
}

// AddProcessorSegments appends processor segments to the engine.
// Designed for the streaming live-trace path (A-1), where segments arrive
// incrementally rather than in a single LoadCapture batch.
func (e *Engine) AddProcessorSegments(segs []model.ProcessorSegment) {
	if len(segs) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.processorSegments = append(e.processorSegments, segs...)
	e.dataVersion++
}

// SetParentIDs merges the provided goroutine→parent mapping into the engine.
// It sets ParentID only for goroutines that are already tracked and have no
// parent set, matching the behaviour of LoadCapture.
func (e *Engine) SetParentIDs(ids map[int64]int64) {
	if len(ids) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for goID, parentID := range ids {
		if g, ok := e.goroutines[goID]; ok && g.ParentID == 0 {
			g.ParentID = parentID
			e.goroutines[goID] = g
		}
	}
	e.dataVersion++
}

// SetLabelOverrides merges label overrides (e.g. from agent.WithRequestID)
// into tracked goroutines. Existing labels are preserved; override values win
// on key collisions. Mirrors the label-merge logic in LoadCapture.
func (e *Engine) SetLabelOverrides(overrides map[int64]model.Labels) {
	if len(overrides) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for goID, labels := range overrides {
		if g, ok := e.goroutines[goID]; ok && len(labels) > 0 {
			if g.Labels == nil {
				g.Labels = make(map[string]string, len(labels))
			}
			for k, v := range labels {
				g.Labels[k] = v
			}
			e.goroutines[goID] = g
		}
	}
	e.dataVersion++
}

// Flush notifies all subscribers that engine state has been updated.
// Call this after applying a batch of streaming events so the UI can refresh.
func (e *Engine) Flush() {
	e.notifySubscribers()
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

// GetStackAt returns the stack snapshot for the given goroutine at or before the
// given nanosecond timestamp. Returns nil if no such snapshot exists (e.g. live
// mode with no history, or replay with no stacks for that goroutine at that time).
func (e *Engine) GetStackAt(goroutineID int64, ns int64) *model.StackSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	target := time.Unix(0, ns)
	var best *model.StackSnapshot
	for i := range e.stacks {
		s := &e.stacks[i]
		if s.GoroutineID != goroutineID {
			continue
		}
		if s.Timestamp.After(target) {
			continue
		}
		if best == nil || s.Timestamp.After(best.Timestamp) {
			best = s
		}
	}
	if best == nil {
		return nil
	}
	out := *best
	out.Frames = append([]model.StackFrame(nil), best.Frames...)
	return &out
}

// GetStacksFor returns all historical stack snapshots for the given goroutine,
// sorted by timestamp ascending. Used to build per-goroutine flame graphs.
func (e *Engine) GetStacksFor(goroutineID int64) []model.StackSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var out []model.StackSnapshot
	for i := range e.stacks {
		s := &e.stacks[i]
		if s.GoroutineID != goroutineID {
			continue
		}
		cp := *s
		cp.Frames = append([]model.StackFrame(nil), s.Frames...)
		out = append(out, cp)
	}
	// Sort ascending by timestamp so callers get a chronological sequence.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// LeakCandidates returns goroutines that have been in WAITING or BLOCKED
// state for longer than thresholdNS. These may indicate goroutine leaks.
func (e *Engine) LeakCandidates(thresholdNS int64) []model.Goroutine {
	e.mu.RLock()
	goroutines := make([]model.Goroutine, 0, len(e.goroutines))
	for _, g := range e.goroutines {
		goroutines = append(goroutines, cloneGoroutine(g))
	}
	e.mu.RUnlock()
	return LeakCandidates(goroutines, thresholdNS)
}

// ResourceContention returns contention metrics per resource from the timeline.
func (e *Engine) ResourceContention() []ResourceContention {
	e.mu.RLock()
	segments := make([]model.TimelineSegment, 0, len(e.closedSegments)+len(e.activeSegments))
	segments = append(segments, e.closedSegments...)
	for goroutineID, segment := range e.activeSegments {
		goroutine, ok := e.goroutines[goroutineID]
		if !ok {
			continue
		}
		if derived, ok := buildTimelineSegment(goroutineID, segment, goroutine.LastSeenAt); ok {
			segments = append(segments, derived)
		}
	}
	e.mu.RUnlock()
	return ComputeResourceContention(segments)
}

// ResourceGraph returns the current set of resource dependency edges.
func (e *Engine) ResourceGraph() []model.ResourceEdge {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.ResourceEdge, len(e.edges))
	copy(out, e.edges)
	return out
}

// ExportCapture builds a model.Capture from the current engine state.
// The returned capture is self-contained and can be saved or replayed.
// One create event and one current-state event are synthesised per goroutine;
// all historical stacks, resource edges, and processor segments are included.
func (e *Engine) ExportCapture() model.Capture {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sessionID := ""
	name := "export"
	if e.session != nil {
		sessionID = e.session.ID
		name = e.session.Name
	}

	events := make([]model.Event, 0, len(e.goroutines)*2)
	parentIDs := make(map[int64]int64)
	labelOverrides := make(map[int64]model.Labels)
	seq := uint64(0)

	for _, g := range e.goroutines {
		ts := g.CreatedAt
		if ts.IsZero() {
			ts = g.LastSeenAt
		}

		seq++
		events = append(events, model.Event{
			SessionID:   sessionID,
			Seq:         seq,
			Timestamp:   ts,
			Kind:        model.EventKindGoroutineCreate,
			GoroutineID: g.ID,
			ParentID:    g.ParentID,
		})

		if g.State != "" {
			seq++
			events = append(events, model.Event{
				SessionID:   sessionID,
				Seq:         seq,
				Timestamp:   g.LastSeenAt,
				Kind:        model.EventKindGoroutineState,
				GoroutineID: g.ID,
				State:       g.State,
				Reason:      g.Reason,
				ResourceID:  g.ResourceID,
			})
		}

		if g.ParentID != 0 {
			parentIDs[g.ID] = g.ParentID
		}

		if len(g.Labels) > 0 {
			cp := make(model.Labels, len(g.Labels))
			for k, v := range g.Labels {
				cp[k] = v
			}
			labelOverrides[g.ID] = cp
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].Seq < events[j].Seq
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	for i := range events {
		events[i].Seq = uint64(i + 1)
	}

	stacks := make([]model.StackSnapshot, len(e.stacks))
	for i, s := range e.stacks {
		cp := s
		cp.Frames = append([]model.StackFrame(nil), s.Frames...)
		stacks[i] = cp
	}

	edges := make([]model.ResourceEdge, len(e.edges))
	copy(edges, e.edges)

	procSegs := make([]model.ProcessorSegment, len(e.processorSegments))
	copy(procSegs, e.processorSegments)

	if len(parentIDs) == 0 {
		parentIDs = nil
	}
	if len(labelOverrides) == 0 {
		labelOverrides = nil
	}

	return model.Capture{
		Name:              name,
		Events:            events,
		Stacks:            stacks,
		Resources:         edges,
		ProcessorSegments: procSegs,
		ParentIDs:         parentIDs,
		LabelOverrides:    labelOverrides,
	}
}

// MemoryStats returns point-in-time counts of the data held by the Engine.
// It acquires the read lock, so it is safe to call from any goroutine.
func (e *Engine) MemoryStats() MemoryStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return MemoryStats{
		ClosedSegments:        len(e.closedSegments),
		ActiveSegments:        len(e.activeSegments),
		Goroutines:            len(e.goroutines),
		StackSnapshots:        len(e.stacks),
		ProcessorSegments:     len(e.processorSegments),
		MaxClosedSegments:     e.retention.MaxClosedSegments,
		MaxStacksPerGoroutine: e.retention.MaxStacksPerGoroutine,
	}
}

// trimClosedSegmentsLocked drops the oldest closed segments when the
// configured cap is exceeded. Must be called with e.mu held for write.
//
// A 10 % hysteresis band avoids trimming on every single call: trimming only
// triggers when the slice is more than 10 % over the cap, and then it trims
// back down to exactly the cap.
func (e *Engine) trimClosedSegmentsLocked() {
	max := e.retention.MaxClosedSegments
	if max <= 0 {
		return
	}
	// Trigger at 110 % of cap to amortise the cost over many appends.
	if len(e.closedSegments) <= max+(max/10) {
		return
	}
	drop := len(e.closedSegments) - max
	copy(e.closedSegments, e.closedSegments[drop:])
	// Zero the tail before reslicing so the GC can reclaim strings
	// (ResourceID, Reason) held by evicted elements. Zeroing first makes the
	// intent impossible to break by swapping the reslice line above.
	for i := max; i < len(e.closedSegments); i++ {
		e.closedSegments[i] = model.TimelineSegment{}
	}
	e.closedSegments = e.closedSegments[:max]
}

// trimStacksForGoroutineLocked drops the oldest stack snapshots for the given
// goroutine when MaxStacksPerGoroutine is exceeded.
// Must be called with e.mu held for write.
func (e *Engine) trimStacksForGoroutineLocked(goroutineID int64) {
	maxPer := e.retention.MaxStacksPerGoroutine
	if maxPer <= 0 {
		return
	}
	// Count how many snapshots exist for this goroutine, newest-first.
	count := 0
	for i := len(e.stacks) - 1; i >= 0; i-- {
		if e.stacks[i].GoroutineID == goroutineID {
			count++
		}
	}
	if count <= maxPer {
		return
	}
	// Remove the oldest (count - maxPer) snapshots for this goroutine.
	toRemove := count - maxPer
	removed := 0
	origLen := len(e.stacks)
	out := e.stacks[:0]
	for _, s := range e.stacks {
		if s.GoroutineID == goroutineID && removed < toRemove {
			removed++
			continue
		}
		out = append(out, s)
	}
	// Zero the tail before reassigning so the GC can reclaim Frames slices
	// held by evicted snapshots. Using origLen (captured before the filter
	// loop) makes the bounds explicit and independent of the assignment below.
	for i := len(out); i < origLen; i++ {
		e.stacks[i] = model.StackSnapshot{}
	}
	e.stacks = out
}

func (e *Engine) resetLocked(session *model.Session) {
	e.session = session.Clone()
	e.goroutines = make(map[int64]model.Goroutine)
	e.closedSegments = nil
	e.activeSegments = make(map[int64]activeSegment)
	e.edges = nil
	e.processorSegments = nil
	e.stacks = nil
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
	e.trimClosedSegmentsLocked()
	e.goroutines[next.ID] = next
	e.dataVersion++
}

func (e *Engine) applyStackSnapshotLocked(snapshot model.StackSnapshot, appendToHistory bool) {
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
	if appendToHistory {
		e.stacks = append(e.stacks, stackCopy)
		e.trimStacksForGoroutineLocked(snapshot.GoroutineID)
	}
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
