package tracebridge

import (
	"bytes"
	"context"
	"os"
	"runtime/trace"
	"sync"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// mockWriter captures all calls to an EngineWriter for assertions.
type mockWriter struct {
	mu             sync.Mutex
	events         []model.Event
	stacks         []model.StackSnapshot
	processorSegs  []model.ProcessorSegment
	parentIDs      map[int64]int64
	labelOverrides map[int64]model.Labels
	flushCount     int
}

func (m *mockWriter) ApplyEvent(ev model.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
}

func (m *mockWriter) ApplyStackSnapshot(ss model.StackSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stacks = append(m.stacks, ss)
}

func (m *mockWriter) AddProcessorSegments(segs []model.ProcessorSegment) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processorSegs = append(m.processorSegs, segs...)
}

func (m *mockWriter) SetParentIDs(ids map[int64]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.parentIDs == nil {
		m.parentIDs = make(map[int64]int64)
	}
	for k, v := range ids {
		m.parentIDs[k] = v
	}
}

func (m *mockWriter) SetLabelOverrides(overrides map[int64]model.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.labelOverrides == nil {
		m.labelOverrides = make(map[int64]model.Labels)
	}
	for k, v := range overrides {
		m.labelOverrides[k] = v
	}
}

func (m *mockWriter) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushCount++
}

// TestStreamBinaryTrace_Basic streams a real runtime trace through StreamBinaryTrace
// and asserts that events were emitted to the writer.
func TestStreamBinaryTrace_Basic(t *testing.T) {
	t.Parallel()

	buf := generateTraceBuf(t, func() {
		var wg sync.WaitGroup
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Millisecond)
			}()
		}
		wg.Wait()
	})

	w := &mockWriter{}
	err := StreamBinaryTrace(context.Background(), StreamBinaryTraceInput{
		Reader: buf,
		Writer: w,
	})
	if err != nil {
		t.Fatalf("StreamBinaryTrace: %v", err)
	}

	if len(w.events) == 0 {
		t.Fatal("expected at least one event; got none")
	}
	if w.flushCount == 0 {
		t.Fatal("expected at least one Flush call; got none")
	}
}

// TestStreamBinaryTrace_ContextCancelled verifies that a pre-cancelled context
// causes StreamBinaryTrace to return an error on the first loop iteration.
// A valid trace buffer is required so that xtrace.NewReader can parse the
// header before the context check fires inside the event loop.
func TestStreamBinaryTrace_ContextCancelled(t *testing.T) {
	t.Parallel()

	buf := generateTraceBuf(t, func() {
		time.Sleep(time.Millisecond)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before StreamBinaryTrace enters the loop

	w := &mockWriter{}
	err := StreamBinaryTrace(ctx, StreamBinaryTraceInput{
		Reader: buf,
		Writer: w,
	})
	if err == nil {
		t.Fatal("expected error from cancelled context; got nil")
	}
}

// TestStreamBinaryTrace_FlushCalled verifies that Flush is called after the
// final EOF, even when fewer than flushEvery events were emitted.
func TestStreamBinaryTrace_FlushCalled(t *testing.T) {
	t.Parallel()

	buf := generateTraceBuf(t, func() {
		// Minimal: just the main goroutine.
		time.Sleep(time.Millisecond)
	})

	w := &mockWriter{}
	if err := StreamBinaryTrace(context.Background(), StreamBinaryTraceInput{
		Reader: buf,
		Writer: w,
	}); err != nil {
		t.Fatalf("StreamBinaryTrace: %v", err)
	}

	if w.flushCount == 0 {
		t.Error("Flush must be called at least once (final flush at EOF)")
	}
}

// TestTailReader_ReturnsEOFOnDone verifies that a TailReader returns io.EOF
// when the done channel is closed with no pending data.
func TestTailReader_ReturnsEOFOnDone(t *testing.T) {
	t.Parallel()

	f := tempEmptyFile(t)
	done := make(chan struct{})
	tr := NewTailReader(f, done, 10*time.Millisecond)

	// Close done immediately — next Read should yield EOF quickly.
	close(done)

	buf := make([]byte, 64)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("TailReader did not return io.EOF within 2s after done closed")
		default:
		}
		n, err := tr.Read(buf)
		if err != nil {
			// io.EOF is the expected outcome.
			if n != 0 {
				t.Errorf("expected 0 bytes with EOF; got %d", n)
			}
			return
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// generateTraceBuf runs fn with runtime tracing enabled and returns the binary
// trace data as a *bytes.Reader.
func generateTraceBuf(t *testing.T, fn func()) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		t.Fatalf("trace.Start: %v", err)
	}
	fn()
	trace.Stop()
	return bytes.NewReader(buf.Bytes())
}

// tempEmptyFile creates a temporary empty file for TailReader tests.
func tempEmptyFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "tailreader-*.tmp")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
