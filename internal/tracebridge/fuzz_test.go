package tracebridge

import (
	"bytes"
	"context"
	"os"
	rttrace "runtime/trace"
	"sync"
	"testing"
	"time"
)

// FuzzBuildCaptureFromRawTrace verifies that buildCaptureFromReader never
// panics on arbitrary input. It seeds the corpus with a real runtime/trace
// binary so the fuzzer has a valid starting point alongside garbage inputs.
func FuzzBuildCaptureFromRawTrace(f *testing.F) {
	// Static seeds: empty, garbage, truncated magic.
	f.Add([]byte{})
	f.Add([]byte("not a trace file"))
	f.Add([]byte("\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("go 1.22 trace\x00\x00\x00\x00"))

	// Real trace seed gives the fuzzer a valid binary to mutate from.
	if data := captureRealTraceBytes(); len(data) > 0 {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// The only requirement: must not panic.
		// Errors from malformed input are expected and ignored.
		ctx := context.Background()
		capture, err := buildCaptureFromReader(ctx, bytes.NewReader(data))
		if err != nil {
			return
		}

		// If parsing succeeded, basic invariants must hold.
		for i, ev := range capture.Events {
			if ev.Kind == "" {
				t.Errorf("event[%d] has empty Kind", i)
			}
			if ev.GoroutineID == 0 {
				t.Errorf("event[%d] has zero GoroutineID", i)
			}
		}
	})
}

// captureRealTraceBytes runs a short workload under runtime/trace and returns
// the raw binary bytes for use as fuzz seed corpus.
func captureRealTraceBytes() []byte {
	runtimeTraceMu.Lock()
	defer runtimeTraceMu.Unlock()

	f, err := os.CreateTemp("", "fuzz_seed*.out")
	if err != nil {
		return nil
	}
	name := f.Name()
	defer os.Remove(name) //nolint:errcheck

	if err := rttrace.Start(f); err != nil {
		_ = f.Close()
		return nil
	}

	var wg sync.WaitGroup
	ch := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-ch
	}()
	go func() {
		defer wg.Done()
		time.Sleep(time.Microsecond)
		close(ch)
	}()
	wg.Wait()

	rttrace.Stop()
	if err := f.Close(); err != nil {
		return nil
	}

	data, _ := os.ReadFile(name) //nolint:errcheck
	return data
}
