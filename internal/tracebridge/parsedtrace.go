// Package tracebridge bridges runtime/trace execution, parsing, and replay.
package tracebridge

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

var (
	syncLineRE = regexp.MustCompile(`Sync Time=(\d+).*Wall=([0-9T:\-+:.Z]+)`)
	// stateTransitionRE matches a goroutine state-transition line emitted by
	// "go tool trace -d=parsed".
	//
	// Actual line format (from go tool trace output):
	//   M=<m> P=<p> G=<g> StateTransition Time=<t> Resource=Goroutine(<id>) Reason="<r>" GoID=<id> <From>-><To>
	//
	// Key insight: GoID= is always equal to Resource=Goroutine(N) — it is the
	// new/transitioning goroutine's own ID, not the parent's.  The parent
	// (creator) goroutine is the G= field in the line prefix.  -1 means no
	// goroutine is running (system context or initial bootstrap).
	//
	// Groups:
	//   [1] P= logical processor ID, may be -1 (system context)
	//   [2] G= running goroutine ID, may be -1 (parent on NotExist→* lines)
	//   [3] monotonic time nanoseconds
	//   [4] goroutine ID that is changing state
	//   [5] reason string (may be empty)
	//   [6] from-state label
	//   [7] to-state label
	stateTransitionRE = regexp.MustCompile(`\bP=(-?\d+)\s+G=(-?\d+)\s.*\bTime=(\d+)\s+Resource=Goroutine\((\d+)\)\s+Reason="([^"]*)"(?:\s+GoID=\d+)?\s+([A-Za-z]+)->([A-Za-z]+)$`)
	workspaceRoot     = mustGetwd()
)

type parsedTransition struct {
	TimeNS int64
	GoID   int64
	// ParentID is the goroutine captured in the GoID= field on a
	// NotExist→* transition. Zero means the field was absent or non-create.
	ParentID    int64
	ProcessorID int // -1 means no P (system/GC context)
	Reason      string
	From        string
	To          string
	Stack       []model.StackFrame
}

// activePSlot tracks the start of a goroutine's current run on a P.
type activePSlot struct {
	processorID int
	startNS     int64 // raw trace nanoseconds (before wall-clock conversion)
}

// LiveTraceRun manages a running Go target process with tracing enabled.
type LiveTraceRun struct {
	tracePath string
	tempDir   string

	done      chan struct{}
	waitErr   error
	waitMu    sync.RWMutex
	closeOnce sync.Once
}

type parsedTraceBuilder struct {
	baseTime      int64
	baseWall      time.Time
	eventSeq      uint64
	stackSeq      uint64
	capture       model.Capture
	keptGoroutine map[int64]bool
	// parentIDs is populated unconditionally on NotExist→* transitions, even
	// for goroutines that will be filtered out by the user-frame filter.
	// This lets the engine set ParentID on goroutines whose create event had
	// no stack yet (the common case).
	parentIDs map[int64]int64
	// activePSlots tracks open P-slot intervals indexed by goroutine ID.
	// Populated unconditionally so that goroutines identified as "user" only
	// after their first Running transition still get correct P segments.
	activePSlots map[int64]activePSlot
	// rawPSegments accumulates all closed P-slot intervals.  They are
	// filtered to kept goroutines and moved to capture.ProcessorSegments
	// after the full trace is scanned.
	rawPSegments []model.ProcessorSegment
}

// BuildCaptureFromRawTrace parses a raw runtime/trace file into a Capture.
func BuildCaptureFromRawTrace(ctx context.Context, tracePath string) (model.Capture, error) {
	cmd := exec.CommandContext(ctx, "go", "tool", "trace", "-d=parsed", tracePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return model.Capture{}, fmt.Errorf("parse raw trace with go tool trace: %w\n%s", err, string(output))
	}

	return ParseParsedTrace(bytes.NewReader(output))
}

// RunGoTargetWithTrace runs the target, collects the trace, and returns the resulting Capture.
func RunGoTargetWithTrace(ctx context.Context, target string, stdout, stderr io.Writer) (model.Capture, error) {
	liveRun, err := StartGoTargetWithTrace(ctx, target, stdout, stderr)
	if err != nil {
		return model.Capture{}, err
	}
	defer func() { _ = liveRun.Close() }()

	if err := liveRun.Wait(); err != nil {
		return model.Capture{}, fmt.Errorf("run target %q: %w", target, err)
	}

	return liveRun.BuildCapture(ctx)
}

// StartGoTargetWithTrace launches the target with tracing and returns a LiveTraceRun handle.
func StartGoTargetWithTrace(ctx context.Context, target string, stdout, stderr io.Writer) (*LiveTraceRun, error) {
	tempDir, err := os.MkdirTemp("", "goroscope-trace-*")
	if err != nil {
		return nil, fmt.Errorf("create temp trace dir: %w", err)
	}

	tracePath := filepath.Join(tempDir, "trace.out")
	cmd := exec.CommandContext(ctx, "go", "run", target)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "GOROSCOPE_TRACE_FILE="+tracePath)

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("start target %q: %w", target, err)
	}

	liveRun := &LiveTraceRun{
		tracePath: tracePath,
		tempDir:   tempDir,
		done:      make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		liveRun.waitMu.Lock()
		liveRun.waitErr = err
		liveRun.waitMu.Unlock()
		close(liveRun.done)
	}()

	return liveRun, nil
}

// Done returns a channel closed when the target process exits.
func (r *LiveTraceRun) Done() <-chan struct{} {
	return r.done
}

// Wait blocks until the target process exits and returns any error.
func (r *LiveTraceRun) Wait() error {
	<-r.done

	r.waitMu.RLock()
	defer r.waitMu.RUnlock()
	return r.waitErr
}

// TraceSize returns the current size of the trace file in bytes.
func (r *LiveTraceRun) TraceSize() (int64, error) {
	info, err := os.Stat(r.tracePath)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// BuildCapture finalises the trace and builds a Capture from the current trace file.
func (r *LiveTraceRun) BuildCapture(ctx context.Context) (model.Capture, error) {
	size, err := r.TraceSize()
	if err != nil {
		if os.IsNotExist(err) {
			return model.Capture{}, fmt.Errorf("target did not emit a runtime trace yet; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
		}
		return model.Capture{}, err
	}
	if size == 0 {
		return model.Capture{}, fmt.Errorf("target did not emit a runtime trace yet; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
	}

	return BuildCaptureFromRawTrace(ctx, r.tracePath)
}

// Close stops the target process and releases resources.
func (r *LiveTraceRun) Close() error {
	var err error
	r.closeOnce.Do(func() {
		err = os.RemoveAll(r.tempDir)
	})

	return err
}

// ParseParsedTrace decodes a runtime/trace stream from r into a Capture.
func ParseParsedTrace(r io.Reader) (model.Capture, error) {
	builder := parsedTraceBuilder{
		capture: model.Capture{
			Name: "runtime-trace",
		},
		keptGoroutine: make(map[int64]bool),
		parentIDs:     make(map[int64]int64),
		activePSlots:  make(map[int64]activePSlot),
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var current *parsedTransition
	collectingStack := false
	pendingFunc := ""

	flushCurrent := func() error {
		if current == nil {
			return nil
		}
		if err := builder.appendTransition(*current); err != nil {
			return err
		}
		current = nil
		collectingStack = false
		pendingFunc = ""
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		if builder.baseWall.IsZero() {
			if match := syncLineRE.FindStringSubmatch(line); match != nil {
				baseTime, err := strconv.ParseInt(match[1], 10, 64)
				if err != nil {
					return model.Capture{}, fmt.Errorf("parse sync time: %w", err)
				}
				baseWall, err := time.Parse(time.RFC3339, match[2])
				if err != nil {
					return model.Capture{}, fmt.Errorf("parse sync wall time: %w", err)
				}
				builder.baseTime = baseTime
				builder.baseWall = baseWall
			}
		}

		if match := stateTransitionRE.FindStringSubmatch(line); match != nil {
			if err := flushCurrent(); err != nil {
				return model.Capture{}, err
			}

			// Group layout (see stateTransitionRE definition):
			//   match[1] = P= logical processor ID ("-1" = system context)
			//   match[2] = G= running goroutine (parent on create events, "-1" = none)
			//   match[3] = Time nanoseconds
			//   match[4] = goroutine being transitioned
			//   match[5] = Reason string
			//   match[6] = from-state label
			//   match[7] = to-state label
			//
			// Skip malformed transitions instead of failing (NFR: collector must not crash).
			timeNS, err := strconv.ParseInt(match[3], 10, 64)
			if err != nil {
				continue
			}
			goID, err := strconv.ParseInt(match[4], 10, 64)
			if err != nil {
				continue
			}
			processorID := -1
			if pid, err := strconv.Atoi(match[1]); err == nil {
				processorID = pid
			}

			current = &parsedTransition{
				TimeNS:      timeNS,
				GoID:        goID,
				ProcessorID: processorID,
				Reason:      match[5],
				From:        match[6],
				To:          match[7],
			}
			// On create transitions the G= prefix field is the running goroutine
			// that executed the "go" statement — i.e. the parent.
			// G=-1 means no goroutine (bootstrap/system context), not a real parent.
			if (match[6] == "NotExist" || match[6] == "Undetermined") && match[2] != "-1" {
				parentID, err := strconv.ParseInt(match[2], 10, 64)
				if err == nil && parentID != goID {
					current.ParentID = parentID
				}
			}
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case line == "Stack=":
			collectingStack = true
			pendingFunc = ""
		case line == "TransitionStack=":
			collectingStack = false
			pendingFunc = ""
		case strings.TrimSpace(line) == "":
			if collectingStack {
				collectingStack = false
				pendingFunc = ""
			}
		case collectingStack:
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.Contains(trimmed, " @ "):
				pendingFunc = strings.SplitN(trimmed, " @ ", 2)[0]
			case pendingFunc != "" && strings.HasPrefix(trimmed, "/"):
				file, lineNumber := parseStackLocation(trimmed)
				current.Stack = append(current.Stack, model.StackFrame{
					Func: pendingFunc,
					File: file,
					Line: lineNumber,
				})
				pendingFunc = ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return model.Capture{}, fmt.Errorf("scan parsed trace: %w", err)
	}
	if err := flushCurrent(); err != nil {
		return model.Capture{}, err
	}
	builder.capture = filterCaptureToRelevantGoroutines(builder.capture)
	if len(builder.capture.Events) == 0 {
		return model.Capture{}, fmt.Errorf("parsed trace produced no goroutine events")
	}

	// Populate ParentIDs for kept goroutines only.  We cannot restrict this
	// earlier because filterCaptureToRelevantGoroutines determines the keep
	// set, which is unavailable while scanning.
	if len(builder.parentIDs) > 0 {
		builder.capture.ParentIDs = make(map[int64]int64, len(builder.parentIDs))
		for goID, parentID := range builder.parentIDs {
			if builder.keptGoroutine[goID] {
				builder.capture.ParentIDs[goID] = parentID
			}
		}
	}

	// Retain P segments only for kept (user) goroutines.
	for _, seg := range builder.rawPSegments {
		if builder.keptGoroutine[seg.GoroutineID] {
			builder.capture.ProcessorSegments = append(builder.capture.ProcessorSegments, seg)
		}
	}

	return builder.capture, nil
}

func (b *parsedTraceBuilder) appendTransition(transition parsedTransition) error {
	if b.baseWall.IsZero() {
		return fmt.Errorf("parsed trace is missing sync line")
	}

	// Record the creator before the user-frame filter.  Create events often
	// arrive before the goroutine has any stack, so they would be filtered
	// out, losing the parent relationship permanently.
	if (transition.From == "NotExist" || transition.From == "Undetermined") && transition.ParentID != 0 {
		b.parentIDs[transition.GoID] = transition.ParentID
	}

	// Track P-slot intervals unconditionally so goroutines identified as
	// "user code" only after their first Running transition still get P data.
	if transition.From == "Running" {
		if slot, ok := b.activePSlots[transition.GoID]; ok {
			startWall := b.baseWall.Add(time.Duration(slot.startNS - b.baseTime))
			endWall := b.baseWall.Add(time.Duration(transition.TimeNS - b.baseTime))
			if endWall.After(startWall) {
				b.rawPSegments = append(b.rawPSegments, model.ProcessorSegment{
					ProcessorID: slot.processorID,
					GoroutineID: transition.GoID,
					StartNS:     startWall.UnixNano(),
					EndNS:       endWall.UnixNano(),
				})
			}
			delete(b.activePSlots, transition.GoID)
		}
	}
	if transition.To == "Running" && transition.ProcessorID >= 0 {
		b.activePSlots[transition.GoID] = activePSlot{
			processorID: transition.ProcessorID,
			startNS:     transition.TimeNS,
		}
	}

	keep := b.keptGoroutine[transition.GoID] || hasUserFrame(transition.Stack)
	if !keep {
		return nil
	}
	b.keptGoroutine[transition.GoID] = true

	timestamp := b.baseWall.Add(time.Duration(transition.TimeNS - b.baseTime))
	label := primaryFunction(transition.Stack)
	labels := model.Labels{}
	if label != "" {
		labels["function"] = label
	}

	if transition.To == "NotExist" {
		b.eventSeq++
		b.capture.Events = append(b.capture.Events, model.Event{
			Seq:         b.eventSeq,
			Timestamp:   timestamp,
			Kind:        model.EventKindGoroutineEnd,
			GoroutineID: transition.GoID,
			Labels:      labels,
		})
		return nil
	}

	state, reason := mapTraceState(transition.To, transition.Reason)
	kind := model.EventKindGoroutineState
	if transition.From == "NotExist" || transition.From == "Undetermined" {
		kind = model.EventKindGoroutineCreate
	}

	b.eventSeq++
	b.capture.Events = append(b.capture.Events, model.Event{
		Seq:         b.eventSeq,
		Timestamp:   timestamp,
		Kind:        kind,
		GoroutineID: transition.GoID,
		ParentID:    transition.ParentID, // non-zero only on create events
		State:       state,
		Reason:      reason,
		Labels:      labels,
	})

	if len(transition.Stack) > 0 {
		b.stackSeq++
		b.capture.Stacks = append(b.capture.Stacks, model.StackSnapshot{
			Seq:         b.stackSeq,
			Timestamp:   timestamp,
			StackID:     fmt.Sprintf("trace_stack_%d", b.stackSeq),
			GoroutineID: transition.GoID,
			Frames:      append([]model.StackFrame(nil), transition.Stack...),
		})
	}

	return nil
}

func mapTraceState(toState, reason string) (model.GoroutineState, model.BlockingReason) {
	switch toState {
	case "Running":
		return model.StateRunning, ""
	case "Runnable":
		return model.StateRunnable, ""
	case "Syscall":
		return model.StateSyscall, model.ReasonSyscall
	case "Waiting":
		return mapWaitingReason(reason)
	default:
		return model.StateWaiting, model.ReasonUnknown
	}
}

// mapWaitingReason maps the Reason string from a "go tool trace -d=parsed"
// Waiting transition to a domain state and blocking reason.
//
// Reason strings are defined as constants in the Go runtime (src/runtime/trace.go)
// and are stable within a major version. The full set observed in practice:
//
//	"chan receive", "chan send", "select",
//	"sync", "sync.(*Cond).Wait",
//	"sleep", "network", "forever",
//	"preempted", "wait for debug call", "wait until GC ends",
//	"GC mark assist wait for work", "GC background sweeper wait",
//	"GC weak to strong wait", "system goroutine wait", "synctest"
func mapWaitingReason(reason string) (model.GoroutineState, model.BlockingReason) {
	switch reason {
	case "chan receive":
		return model.StateBlocked, model.ReasonChanRecv
	case "chan send":
		return model.StateBlocked, model.ReasonChanSend
	case "select":
		return model.StateBlocked, model.ReasonSelect
	case "sync":
		// Generic mutex / RWMutex lock — trace does not distinguish the two.
		return model.StateBlocked, model.ReasonMutexLock
	case "sync.(*Cond).Wait":
		return model.StateWaiting, model.ReasonSyncCond
	case "sleep":
		return model.StateWaiting, model.ReasonSleep
	default:
		if strings.HasPrefix(reason, "GC") {
			return model.StateWaiting, model.ReasonGCAssist
		}
		return model.StateWaiting, model.ReasonUnknown
	}
}

func parseStackLocation(raw string) (string, int) {
	idx := strings.LastIndex(raw, ":")
	if idx == -1 {
		return raw, 0
	}

	lineNumber, err := strconv.Atoi(raw[idx+1:])
	if err != nil {
		return raw, 0
	}

	return raw[:idx], lineNumber
}

func hasUserFrame(frames []model.StackFrame) bool {
	for _, frame := range frames {
		if isRelevantFrame(frame) {
			return true
		}
	}
	return false
}

func primaryFunction(frames []model.StackFrame) string {
	for _, frame := range frames {
		if isRelevantFrame(frame) {
			return frame.Func
		}
	}
	if len(frames) > 0 {
		return frames[0].Func
	}
	return ""
}

func isUserFunction(function string) bool {
	return function != "" &&
		!strings.HasPrefix(function, "runtime.") &&
		!strings.HasPrefix(function, "internal/") &&
		!strings.HasPrefix(function, "syscall.") &&
		!strings.HasPrefix(function, "runtime/")
}

func isRelevantFrame(frame model.StackFrame) bool {
	if frame.File != "" {
		cleanFile := filepath.Clean(frame.File)
		if workspaceRoot != "" && strings.HasPrefix(cleanFile, workspaceRoot+string(os.PathSeparator)) {
			return true
		}

		gorootSrc := filepath.Clean(filepath.Join(runtime.GOROOT(), "src")) + string(os.PathSeparator)
		if strings.HasPrefix(cleanFile, gorootSrc) {
			return false
		}
		if cleanFile != "" {
			return true
		}
	}

	return isUserFunction(frame.Func)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(wd)
}

func filterCaptureToRelevantGoroutines(capture model.Capture) model.Capture {
	latest := make(map[int64]model.StackSnapshot)
	for _, snapshot := range capture.Stacks {
		current, ok := latest[snapshot.GoroutineID]
		if !ok || snapshot.Timestamp.After(current.Timestamp) {
			latest[snapshot.GoroutineID] = snapshot
		}
	}

	keep := make(map[int64]bool)
	for goroutineID, snapshot := range latest {
		if goroutineID == 1 || hasUserFrame(snapshot.Frames) {
			keep[goroutineID] = true
		}
	}

	if len(keep) == 0 {
		return capture
	}

	filtered := model.Capture{
		Name:   capture.Name,
		Target: capture.Target,
	}

	for _, event := range capture.Events {
		if keep[event.GoroutineID] {
			filtered.Events = append(filtered.Events, event)
		}
	}
	for _, snapshot := range capture.Stacks {
		if keep[snapshot.GoroutineID] {
			filtered.Stacks = append(filtered.Stacks, snapshot)
		}
	}
	for _, edge := range capture.Resources {
		if keep[edge.FromGoroutineID] || keep[edge.ToGoroutineID] {
			filtered.Resources = append(filtered.Resources, edge)
		}
	}

	return filtered
}
