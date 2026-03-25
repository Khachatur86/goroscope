// Package tracebridge — xtrace.go
//
// BuildCaptureFromRawTrace is implemented here using golang.org/x/exp/trace,
// a direct in-process binary reader for Go runtime trace v2 files.
// This replaces the previous subprocess approach (go tool trace -d=parsed).
//
// # Timestamp semantics
//
// golang.org/x/exp/trace.Time is an int64 of nanoseconds produced by the
// reader after applying the frequency calibration event embedded in the
// trace. The reader correlates these nanosecond values against the wall
// clock using the sync events inside the trace, so treating the raw int64
// as Unix nanoseconds (via time.Unix(0, n)) gives a correct, comparable
// time.Time for all events within the same capture. Relative durations are
// always accurate regardless of the absolute epoch.
package tracebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	xtrace "golang.org/x/exp/trace"

	"github.com/Khachatur86/goroscope/internal/model"
)

// BuildCaptureFromReader parses a raw runtime/trace binary stream into a Capture.
// It uses golang.org/x/exp/trace for direct in-process reading — no subprocess.
// Unlike BuildCaptureFromRawTrace, no label sidecar is applied.
// This is useful when the trace data arrives over a network (e.g. Flight Recorder snapshot).
func BuildCaptureFromReader(ctx context.Context, r io.Reader) (model.Capture, error) {
	return buildCaptureFromReader(ctx, r)
}

// BuildCaptureFromRawTrace parses a raw runtime/trace binary file into a Capture.
// It uses golang.org/x/exp/trace for direct in-process reading — no subprocess.
// If tracePath+".labels" exists (written by agent.WithRequestID), label overrides
// are merged into the returned Capture.
func BuildCaptureFromRawTrace(ctx context.Context, tracePath string) (model.Capture, error) {
	f, err := os.Open(tracePath) //nolint:gosec // tracePath points to a local trace artifact selected by the caller.
	if err != nil {
		return model.Capture{}, fmt.Errorf("open trace file %s: %w", tracePath, err)
	}

	capture, err := buildCaptureFromReader(ctx, f)
	if closeErr := f.Close(); closeErr != nil {
		if err != nil {
			return model.Capture{}, errors.Join(err, fmt.Errorf("close trace file %s: %w", tracePath, closeErr))
		}
		return model.Capture{}, fmt.Errorf("close trace file %s: %w", tracePath, closeErr)
	}
	if err != nil {
		return model.Capture{}, err
	}

	overrides, err := ReadLabelsFile(tracePath + ".labels")
	if err != nil {
		return model.Capture{}, err
	}
	if len(overrides) > 0 {
		capture.LabelOverrides = overrides
	}
	return capture, nil
}

// buildCaptureFromReader reads a runtime/trace binary stream from r into a Capture.
// Exported for use by the streaming path (A-1) once it lands.
func buildCaptureFromReader(ctx context.Context, r io.Reader) (model.Capture, error) {
	reader, err := xtrace.NewReader(r)
	if err != nil {
		return model.Capture{}, fmt.Errorf("create trace reader: %w", err)
	}

	b := xBuilder{
		capture: model.Capture{
			Name: "runtime-trace",
		},
		keptGoroutine: make(map[int64]bool),
		parentIDs:     make(map[int64]int64),
		activePSlots:  make(map[int64]activePSlot),
	}

	for {
		if err := ctx.Err(); err != nil {
			return model.Capture{}, fmt.Errorf("trace read cancelled: %w", err)
		}

		ev, err := reader.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return model.Capture{}, fmt.Errorf("read trace event: %w", err)
		}

		if ev.Kind() != xtrace.EventStateTransition {
			continue
		}

		st := ev.StateTransition()
		if st.Resource.Kind != xtrace.ResourceGoroutine {
			continue
		}

		if err := b.appendXEvent(ev, st); err != nil {
			return model.Capture{}, err
		}
	}

	b.capture = filterCaptureToRelevantGoroutines(b.capture)
	if len(b.capture.Events) == 0 {
		return model.Capture{}, fmt.Errorf("parsed trace produced no goroutine events")
	}

	// Populate ParentIDs for kept goroutines only.  We cannot restrict this
	// earlier because filterCaptureToRelevantGoroutines determines the keep
	// set, which is unavailable while scanning.
	if len(b.parentIDs) > 0 {
		b.capture.ParentIDs = make(map[int64]int64, len(b.parentIDs))
		for goID, parentID := range b.parentIDs {
			if b.keptGoroutine[goID] {
				b.capture.ParentIDs[goID] = parentID
			}
		}
	}

	// Retain P segments only for kept (user) goroutines.
	for _, seg := range b.rawPSegments {
		if b.keptGoroutine[seg.GoroutineID] {
			b.capture.ProcessorSegments = append(b.capture.ProcessorSegments, seg)
		}
	}

	return b.capture, nil
}

// xBuilder accumulates events from golang.org/x/exp/trace into a model.Capture.
// Its fields mirror parsedTraceBuilder for consistency; shared helpers from
// parsedtrace.go (activePSlot, hasUserFrame, primaryFunction, …) are reused.
type xBuilder struct {
	eventSeq      uint64
	stackSeq      uint64
	capture       model.Capture
	keptGoroutine map[int64]bool
	// parentIDs is populated unconditionally on NotExist/Undetermined→* transitions
	// so that the parent-child relationship survives the user-frame filter.
	parentIDs    map[int64]int64
	activePSlots map[int64]activePSlot
	rawPSegments []model.ProcessorSegment
}

// appendXEvent processes a single goroutine StateTransition event.
func (b *xBuilder) appendXEvent(ev xtrace.Event, st xtrace.StateTransition) error {
	goID := int64(st.Resource.Goroutine())
	from, to := st.Goroutine()
	timestamp := xTimestamp(ev.Time())

	// Record parent before the user-frame filter so create events with no
	// user stack still preserve the parent-child relationship.
	// ev.Goroutine() returns the goroutine running at event time — on create
	// events that is the goroutine that executed the "go" statement (parent).
	if from == xtrace.GoNotExist || from == xtrace.GoUndetermined {
		parentGoID := int64(ev.Goroutine())
		if parentGoID > 0 && parentGoID != goID {
			b.parentIDs[goID] = parentGoID
		}
	}

	// Track P-slot intervals unconditionally so goroutines identified as user
	// code only after their first Running transition still get correct P data.
	if from == xtrace.GoRunning {
		if slot, ok := b.activePSlots[goID]; ok {
			startWall := xTimestamp(xtrace.Time(slot.startNS))
			if timestamp.After(startWall) {
				b.rawPSegments = append(b.rawPSegments, model.ProcessorSegment{
					ProcessorID: slot.processorID,
					GoroutineID: goID,
					StartNS:     startWall.UnixNano(),
					EndNS:       timestamp.UnixNano(),
				})
			}
			delete(b.activePSlots, goID)
		}
	}
	if to == xtrace.GoRunning {
		procID := int64(ev.Proc())
		if procID >= 0 {
			b.activePSlots[goID] = activePSlot{
				processorID: int(procID),
				startNS:     int64(ev.Time()),
			}
		}
	}

	// User-frame filter — mirrors parsedTraceBuilder.appendTransition.
	stack := xFrames(st.Stack)
	keep := b.keptGoroutine[goID] || hasUserFrame(stack) || goID == 1
	if !keep {
		return nil
	}
	b.keptGoroutine[goID] = true

	label := primaryFunction(stack)
	labels := model.Labels{}
	if label != "" {
		labels["function"] = label
	}

	// Goroutine ended.
	if to == xtrace.GoNotExist {
		b.eventSeq++
		b.capture.Events = append(b.capture.Events, model.Event{
			Seq:         b.eventSeq,
			Timestamp:   timestamp,
			Kind:        model.EventKindGoroutineEnd,
			GoroutineID: goID,
			Labels:      labels,
		})
		return nil
	}

	state, reason := xMapState(to, st.Reason)
	kind := model.EventKindGoroutineState
	if from == xtrace.GoNotExist || from == xtrace.GoUndetermined {
		kind = model.EventKindGoroutineCreate
	}

	// ParentID is only meaningful on the create event; look it up once.
	var parentID int64
	if kind == model.EventKindGoroutineCreate {
		parentID = b.parentIDs[goID]
	}

	b.eventSeq++
	b.capture.Events = append(b.capture.Events, model.Event{
		Seq:         b.eventSeq,
		Timestamp:   timestamp,
		Kind:        kind,
		GoroutineID: goID,
		ParentID:    parentID,
		State:       state,
		Reason:      reason,
		Labels:      labels,
	})

	if len(stack) > 0 {
		b.stackSeq++
		b.capture.Stacks = append(b.capture.Stacks, model.StackSnapshot{
			Seq:         b.stackSeq,
			Timestamp:   timestamp,
			StackID:     fmt.Sprintf("xtrace_stack_%d", b.stackSeq),
			GoroutineID: goID,
			Frames:      stack,
		})
	}

	return nil
}

// xMapState maps a GoState and reason string to model domain types.
// It reuses mapWaitingReason from parsedtrace.go for consistent reason mapping.
func xMapState(to xtrace.GoState, reason string) (model.GoroutineState, model.BlockingReason) {
	switch to {
	case xtrace.GoRunning:
		return model.StateRunning, ""
	case xtrace.GoRunnable:
		return model.StateRunnable, ""
	case xtrace.GoSyscall:
		return model.StateSyscall, model.ReasonSyscall
	case xtrace.GoWaiting:
		return mapWaitingReason(reason)
	default:
		return model.StateWaiting, model.ReasonUnknown
	}
}

// xTimestamp converts a golang.org/x/exp/trace.Time to time.Time.
// The reader calibrates trace clock ticks to nanoseconds using the frequency
// event in the trace and correlates them to wall time via sync events, so the
// resulting int64 can be treated as Unix nanoseconds.
func xTimestamp(t xtrace.Time) time.Time {
	return time.Unix(0, int64(t))
}

// xFrames converts a golang.org/x/exp/trace.Stack into []model.StackFrame.
// Stack.Frames() returns a Go 1.22 range-over-function iterator.
func xFrames(stack xtrace.Stack) []model.StackFrame {
	var frames []model.StackFrame
	for f := range stack.Frames() {
		frames = append(frames, model.StackFrame{
			Func: f.Func,
			File: f.File,
			Line: xLineNumber(f.Line),
		})
	}
	return frames
}

func xLineNumber(line uint64) int {
	maxInt := uint64(^uint(0) >> 1)
	if line > maxInt {
		return int(maxInt)
	}
	return int(line)
}
