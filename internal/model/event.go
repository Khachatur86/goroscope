package model

import "time"

// EventKind identifies the type of a goroutine lifecycle event.
type EventKind string

// Event kind constants.
const (
	EventKindGoroutineCreate EventKind = "goroutine.create"
	EventKindGoroutineStart  EventKind = "goroutine.start"
	EventKindGoroutineState  EventKind = "goroutine.state"
	EventKindGoroutineEnd    EventKind = "goroutine.end"
	EventKindStackSnapshot   EventKind = "stack.snapshot"
	EventKindResourceEdge    EventKind = "resource.edge"
)

// GoroutineState is the scheduling state of a goroutine.
type GoroutineState string

// Goroutine state constants.
const (
	StateRunning  GoroutineState = "RUNNING"
	StateRunnable GoroutineState = "RUNNABLE"
	StateWaiting  GoroutineState = "WAITING"
	StateBlocked  GoroutineState = "BLOCKED"
	StateSyscall  GoroutineState = "SYSCALL"
	StateDone     GoroutineState = "DONE"
)

// BlockingReason describes why a goroutine is blocked or waiting.
type BlockingReason string

// Blocking reason constants.
const (
	ReasonUnknown     BlockingReason = "unknown"
	ReasonChanSend    BlockingReason = "chan_send"
	ReasonChanRecv    BlockingReason = "chan_recv"
	ReasonSelect      BlockingReason = "select"
	ReasonMutexLock   BlockingReason = "mutex_lock"
	ReasonRWMutexLock BlockingReason = "rwmutex_lock"
	ReasonRWMutexR    BlockingReason = "rwmutex_rlock"
	ReasonSyncCond    BlockingReason = "sync_cond"
	ReasonSyscall     BlockingReason = "syscall"
	ReasonSleep       BlockingReason = "sleep"
	ReasonGCAssist    BlockingReason = "gc_assist"
)

// Labels is a set of key-value metadata attached to a goroutine event.
type Labels map[string]string

// Event is a single goroutine lifecycle event emitted by the runtime trace bridge.
type Event struct {
	SessionID   string    `json:"session_id"`
	Seq         uint64    `json:"seq"`
	Timestamp   time.Time `json:"timestamp"`
	Kind        EventKind `json:"kind"`
	GoroutineID int64     `json:"goroutine_id,omitempty"`
	// ParentID is the goroutine that executed the "go" statement when Kind is
	// EventKindGoroutineCreate. Zero means unknown or not applicable.
	ParentID   int64          `json:"parent_id,omitempty"`
	State      GoroutineState `json:"state,omitempty"`
	Reason     BlockingReason `json:"reason,omitempty"`
	ResourceID string         `json:"resource_id,omitempty"`
	StackID    string         `json:"stack_id,omitempty"`
	Labels     Labels         `json:"labels,omitempty"`
}

// StackFrame represents one frame in a goroutine's call stack.
type StackFrame struct {
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// StackSnapshot is a point-in-time capture of a goroutine's call stack.
type StackSnapshot struct {
	SessionID   string       `json:"session_id"`
	Seq         uint64       `json:"seq"`
	Timestamp   time.Time    `json:"timestamp"`
	StackID     string       `json:"stack_id"`
	GoroutineID int64        `json:"goroutine_id"`
	Frames      []StackFrame `json:"frames"`
}
