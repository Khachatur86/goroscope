// Package tracebridge — stream.go
//
// A-1: Streaming live-trace path.
//
// EngineWriter abstracts *analysis.Engine so tracebridge can emit events to it
// without creating a circular import.  TailReader follows a growing binary
// trace file, blocking at EOF until the target process exits.
// StreamBinaryTrace wires them together: it feeds the TailReader into
// golang.org/x/exp/trace.NewReader and emits parsed events directly to the
// EngineWriter as they arrive — O(n) total, no full file re-read on each tick.
package tracebridge

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	xtrace "golang.org/x/exp/trace"

	"github.com/Khachatur86/goroscope/internal/model"
)

// EngineWriter is the sink interface consumed by StreamBinaryTrace.
// *analysis.Engine satisfies this interface; tests may supply mocks.
type EngineWriter interface {
	ApplyEvent(event model.Event)
	ApplyStackSnapshot(snapshot model.StackSnapshot)
	AddProcessorSegments(segs []model.ProcessorSegment)
	SetParentIDs(ids map[int64]int64)
	SetLabelOverrides(overrides map[int64]model.Labels)
	Flush()
}

// TailReader wraps an *os.File and blocks Read at EOF until the done channel
// is closed, at which point it returns io.EOF.  This lets
// golang.org/x/exp/trace stream a growing binary trace file in-process
// without subprocess round-trips or goroutine leaks.
type TailReader struct {
	f         *os.File
	done      <-chan struct{}
	pollDelay time.Duration
}

// NewTailReader returns a TailReader that follows f as it grows.
// When done is closed the next blocking Read returns io.EOF.
// pollDelay controls how long Read sleeps between retries.
func NewTailReader(f *os.File, done <-chan struct{}, pollDelay time.Duration) *TailReader {
	return &TailReader{f: f, done: done, pollDelay: pollDelay}
}

// Read implements io.Reader.  It retries on EOF until new data arrives,
// the done channel is closed, or a real error is returned by the underlying file.
func (t *TailReader) Read(p []byte) (int, error) {
	for {
		n, err := t.f.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != nil && err != io.EOF {
			return 0, err
		}

		// At EOF: check done without blocking.
		select {
		case <-t.done:
			// Target exited — drain any bytes written just before exit.
			n, _ = t.f.Read(p)
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		default:
		}

		// Wait for new data or target exit.
		select {
		case <-t.done:
			n, _ = t.f.Read(p)
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		case <-time.After(t.pollDelay):
		}
	}
}

// WaitForTraceFile polls until path exists and can be opened, the context is
// cancelled, or done is closed (target exited without creating the file).
// Returns an open *os.File on success; the caller is responsible for closing it.
func WaitForTraceFile(ctx context.Context, path string, done <-chan struct{}, pollDelay time.Duration) (*os.File, error) {
	for {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("open trace file %s: %w", path, err)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled waiting for trace file: %w", ctx.Err())
		case <-done:
			// Target exited — try once more in case it wrote the file on the way out.
			f, err := os.Open(path)
			if err != nil {
				return nil, fmt.Errorf(
					"target exited without creating trace file %s; "+
						"import agent and call agent.StartFromEnv() in main: %w",
					path, err,
				)
			}
			return f, nil
		case <-time.After(pollDelay):
		}
	}
}

// StreamBinaryTraceInput holds parameters for StreamBinaryTrace.
// Contexts should not be placed in structs (CTX-1); ctx is the first arg of
// StreamBinaryTrace itself.
type StreamBinaryTraceInput struct {
	// Reader is the source of the binary trace stream (e.g. a *TailReader).
	Reader io.Reader
	// Writer receives parsed events as they arrive.
	Writer EngineWriter
}

// StreamBinaryTrace reads a Go runtime/trace binary stream from in.Reader and
// emits events to in.Writer as they are parsed.  It is designed for the live
// path where in.Reader blocks at EOF until new data arrives (TailReader).
//
// When the reader returns io.EOF (target exited or stream ended),
// StreamBinaryTrace emits accumulated ParentID and ProcessorSegment data and
// calls Writer.Flush.  Returns nil on a clean EOF, or a wrapped error on
// parse/context failure.
func StreamBinaryTrace(ctx context.Context, in StreamBinaryTraceInput) error {
	reader, err := xtrace.NewReader(in.Reader)
	if err != nil {
		return fmt.Errorf("create trace reader: %w", err)
	}

	b := &streamBuilder{
		writer:        in.Writer,
		keptGoroutine: make(map[int64]bool),
		parentIDs:     make(map[int64]int64),
		activePSlots:  make(map[int64]activePSlot),
	}

	var eventSeq, stackSeq uint64
	var pendingFlush int
	const flushEvery = 64 // notify subscribers roughly every 64 kept events

	// Time-based flush: even when fewer than flushEvery events arrive (e.g.
	// when the TailReader is stalled at EOF), push UI updates every 200 ms so
	// goroutines in blocking states are visible while the target is alive.
	// Writer.Flush is idempotent (it only wakes SSE subscribers), so calling
	// it from both the ticker and the event loop is safe.
	// The goroutine is tied to streamDone (CC-2: goroutine lifetime to context).
	streamDone := make(chan struct{})
	defer close(streamDone)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-streamDone:
				return
			case <-ticker.C:
				in.Writer.Flush()
			}
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("trace stream cancelled: %w", err)
		}

		ev, err := reader.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read trace event: %w", err)
		}

		if ev.Kind() != xtrace.EventStateTransition {
			continue
		}
		st := ev.StateTransition()
		if st.Resource.Kind != xtrace.ResourceGoroutine {
			continue
		}

		emitted := b.appendEvent(ev, st, &eventSeq, &stackSeq)
		if emitted {
			pendingFlush++
			if pendingFlush >= flushEvery {
				in.Writer.Flush()
				pendingFlush = 0
			}
		}
	}

	// Emit accumulated metadata now that the trace is complete.
	if len(b.parentIDs) > 0 {
		kept := make(map[int64]int64, len(b.parentIDs))
		for goID, parentID := range b.parentIDs {
			if b.keptGoroutine[goID] {
				kept[goID] = parentID
			}
		}
		if len(kept) > 0 {
			in.Writer.SetParentIDs(kept)
		}
	}

	var keptSegs []model.ProcessorSegment
	for _, seg := range b.rawPSegments {
		if b.keptGoroutine[seg.GoroutineID] {
			keptSegs = append(keptSegs, seg)
		}
	}
	if len(keptSegs) > 0 {
		in.Writer.AddProcessorSegments(keptSegs)
	}

	in.Writer.Flush()
	return nil
}

// streamBuilder accumulates per-goroutine filter state during streaming.
// It mirrors xBuilder but emits to an EngineWriter instead of model.Capture.
type streamBuilder struct {
	writer        EngineWriter
	keptGoroutine map[int64]bool
	parentIDs     map[int64]int64
	activePSlots  map[int64]activePSlot
	rawPSegments  []model.ProcessorSegment
}

// appendEvent processes one goroutine StateTransition and emits to writer if
// the goroutine passes the user-frame filter.  Returns true if an event was
// emitted.
func (b *streamBuilder) appendEvent(ev xtrace.Event, st xtrace.StateTransition, eventSeq, stackSeq *uint64) bool {
	goID := int64(st.Resource.Goroutine())
	from, to := st.Goroutine()
	timestamp := xTimestamp(ev.Time())

	// Record parent before the user-frame filter — same logic as xBuilder.
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

	// User-frame filter — keep if already known user goroutine, has user frame, or is G1.
	stack := xFrames(st.Stack)
	if !b.keptGoroutine[goID] && !hasUserFrame(stack) && goID != 1 {
		return false
	}
	b.keptGoroutine[goID] = true

	label := primaryFunction(stack)
	labels := model.Labels{}
	if label != "" {
		labels["function"] = label
	}

	if to == xtrace.GoNotExist {
		*eventSeq++
		b.writer.ApplyEvent(model.Event{
			Seq:         *eventSeq,
			Timestamp:   timestamp,
			Kind:        model.EventKindGoroutineEnd,
			GoroutineID: goID,
			Labels:      labels,
		})
		return true
	}

	state, reason := xMapState(to, st.Reason)
	kind := model.EventKindGoroutineState
	if from == xtrace.GoNotExist || from == xtrace.GoUndetermined {
		kind = model.EventKindGoroutineCreate
	}

	var parentID int64
	if kind == model.EventKindGoroutineCreate {
		parentID = b.parentIDs[goID]
	}

	*eventSeq++
	b.writer.ApplyEvent(model.Event{
		Seq:         *eventSeq,
		Timestamp:   timestamp,
		Kind:        kind,
		GoroutineID: goID,
		ParentID:    parentID,
		State:       state,
		Reason:      reason,
		Labels:      labels,
	})

	if len(stack) > 0 {
		*stackSeq++
		b.writer.ApplyStackSnapshot(model.StackSnapshot{
			Seq:         *stackSeq,
			Timestamp:   timestamp,
			StackID:     fmt.Sprintf("stream_stack_%d", *stackSeq),
			GoroutineID: goID,
			Frames:      stack,
		})
	}

	return true
}
