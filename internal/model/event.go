package model

import "time"

type EventKind string

const (
	EventKindGoroutineCreate EventKind = "goroutine.create"
	EventKindGoroutineStart  EventKind = "goroutine.start"
	EventKindGoroutineState  EventKind = "goroutine.state"
	EventKindGoroutineEnd    EventKind = "goroutine.end"
	EventKindStackSnapshot   EventKind = "stack.snapshot"
	EventKindResourceEdge    EventKind = "resource.edge"
)

type GoroutineState string

const (
	StateRunning  GoroutineState = "RUNNING"
	StateRunnable GoroutineState = "RUNNABLE"
	StateWaiting  GoroutineState = "WAITING"
	StateBlocked  GoroutineState = "BLOCKED"
	StateSyscall  GoroutineState = "SYSCALL"
	StateDone     GoroutineState = "DONE"
)

type BlockingReason string

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

type Labels map[string]string

type Event struct {
	SessionID   string         `json:"session_id"`
	Seq         uint64         `json:"seq"`
	Timestamp   time.Time      `json:"timestamp"`
	Kind        EventKind      `json:"kind"`
	GoroutineID int64          `json:"goroutine_id,omitempty"`
	// ParentID is the goroutine that executed the "go" statement when Kind is
	// EventKindGoroutineCreate. Zero means unknown or not applicable.
	ParentID    int64          `json:"parent_id,omitempty"`
	State       GoroutineState `json:"state,omitempty"`
	Reason      BlockingReason `json:"reason,omitempty"`
	ResourceID  string         `json:"resource_id,omitempty"`
	StackID     string         `json:"stack_id,omitempty"`
	Labels      Labels         `json:"labels,omitempty"`
}

type StackFrame struct {
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type StackSnapshot struct {
	SessionID   string       `json:"session_id"`
	Seq         uint64       `json:"seq"`
	Timestamp   time.Time    `json:"timestamp"`
	StackID     string       `json:"stack_id"`
	GoroutineID int64        `json:"goroutine_id"`
	Frames      []StackFrame `json:"frames"`
}
