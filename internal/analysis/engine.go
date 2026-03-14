package analysis

import (
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

type Engine struct {
	mu         sync.RWMutex
	session    *model.Session
	goroutines map[int64]model.Goroutine
	segments   []model.TimelineSegment
	edges      []model.ResourceEdge
}

func NewEngine() *Engine {
	return &Engine{
		goroutines: make(map[int64]model.Goroutine),
	}
}

func (e *Engine) SeedDemoSession(session *model.Session) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.session = cloneSession(session)
	e.goroutines = make(map[int64]model.Goroutine)
	e.segments = e.segments[:0]
	e.edges = e.edges[:0]

	base := time.Now().UTC().Add(-2 * time.Second)
	stackWorker := &model.StackSnapshot{
		SessionID:   session.ID,
		Seq:         12,
		Timestamp:   base.Add(1600 * time.Millisecond),
		StackID:     "stk_worker",
		GoroutineID: 42,
		Frames: []model.StackFrame{
			{Func: "main.worker", File: "/workspace/app/main.go", Line: 57},
			{Func: "main.main", File: "/workspace/app/main.go", Line: 92},
		},
	}
	stackProducer := &model.StackSnapshot{
		SessionID:   session.ID,
		Seq:         6,
		Timestamp:   base.Add(900 * time.Millisecond),
		StackID:     "stk_producer",
		GoroutineID: 1,
		Frames: []model.StackFrame{
			{Func: "main.producer", File: "/workspace/app/main.go", Line: 24},
			{Func: "main.main", File: "/workspace/app/main.go", Line: 88},
		},
	}
	stackSink := &model.StackSnapshot{
		SessionID:   session.ID,
		Seq:         17,
		Timestamp:   base.Add(1800 * time.Millisecond),
		StackID:     "stk_sink",
		GoroutineID: 77,
		Frames: []model.StackFrame{
			{Func: "main.sink", File: "/workspace/app/main.go", Line: 73},
			{Func: "main.main", File: "/workspace/app/main.go", Line: 95},
		},
	}

	e.goroutines[1] = model.Goroutine{
		ID:         1,
		State:      model.StateRunning,
		CreatedAt:  base,
		LastSeenAt: base.Add(2 * time.Second),
		LastStack:  stackProducer,
		Labels:     map[string]string{"function": "main.producer"},
	}
	e.goroutines[42] = model.Goroutine{
		ID:         42,
		State:      model.StateBlocked,
		Reason:     model.ReasonChanRecv,
		ResourceID: "chan:0xc000018230",
		WaitNS:     int64(230 * time.Millisecond),
		CreatedAt:  base.Add(50 * time.Millisecond),
		LastSeenAt: base.Add(2 * time.Second),
		LastStack:  stackWorker,
		Labels:     map[string]string{"function": "main.worker"},
	}
	e.goroutines[77] = model.Goroutine{
		ID:         77,
		State:      model.StateWaiting,
		Reason:     model.ReasonMutexLock,
		ResourceID: "mutex:0xc000014180",
		WaitNS:     int64(110 * time.Millisecond),
		CreatedAt:  base.Add(100 * time.Millisecond),
		LastSeenAt: base.Add(2 * time.Second),
		LastStack:  stackSink,
		Labels:     map[string]string{"function": "main.sink"},
	}

	e.segments = []model.TimelineSegment{
		{GoroutineID: 1, StartNS: base.UnixNano(), EndNS: base.Add(800 * time.Millisecond).UnixNano(), State: model.StateRunning},
		{GoroutineID: 1, StartNS: base.Add(800 * time.Millisecond).UnixNano(), EndNS: base.Add(2 * time.Second).UnixNano(), State: model.StateRunnable},
		{GoroutineID: 42, StartNS: base.Add(100 * time.Millisecond).UnixNano(), EndNS: base.Add(900 * time.Millisecond).UnixNano(), State: model.StateRunning},
		{GoroutineID: 42, StartNS: base.Add(900 * time.Millisecond).UnixNano(), EndNS: base.Add(1600 * time.Millisecond).UnixNano(), State: model.StateBlocked, Reason: model.ReasonChanRecv, ResourceID: "chan:0xc000018230"},
		{GoroutineID: 42, StartNS: base.Add(1600 * time.Millisecond).UnixNano(), EndNS: base.Add(2 * time.Second).UnixNano(), State: model.StateRunning},
		{GoroutineID: 77, StartNS: base.Add(200 * time.Millisecond).UnixNano(), EndNS: base.Add(1300 * time.Millisecond).UnixNano(), State: model.StateWaiting, Reason: model.ReasonMutexLock, ResourceID: "mutex:0xc000014180"},
		{GoroutineID: 77, StartNS: base.Add(1300 * time.Millisecond).UnixNano(), EndNS: base.Add(2 * time.Second).UnixNano(), State: model.StateSyscall, Reason: model.ReasonSyscall},
	}

	e.edges = []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 42, ResourceID: "chan:0xc000018230", Kind: "channel"},
		{FromGoroutineID: 42, ToGoroutineID: 77, ResourceID: "mutex:0xc000014180", Kind: "mutex"},
	}
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

	out := make([]model.TimelineSegment, len(e.segments))
	copy(out, e.segments)
	return out
}

func (e *Engine) ResourceGraph() []model.ResourceEdge {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]model.ResourceEdge, len(e.edges))
	copy(out, e.edges)
	return out
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
