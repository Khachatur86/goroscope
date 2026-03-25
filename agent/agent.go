// Package agent provides opt-in trace bootstrap for target Go programs.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
)

const traceFileEnv = "GOROSCOPE_TRACE_FILE"

// traceFilePath returns the active trace file path from the environment, or "".
func traceFilePath() string { return os.Getenv(traceFileEnv) }

// DefaultRequestIDKey is the default label key for request/trace IDs.
const DefaultRequestIDKey = "request_id"

var goroutineIDRE = regexp.MustCompile(`^goroutine (\d+) `)

// StartFromEnv enables runtime tracing when Goroscope provides a trace path.
// If the environment is not configured, it returns a no-op cleanup.
func StartFromEnv() (func() error, error) {
	tracePath := os.Getenv(traceFileEnv)
	if tracePath == "" {
		return func() error { return nil }, nil
	}

	//nolint:gosec // trace path comes from env, not user input
	file, err := os.Create(tracePath)
	if err != nil {
		return nil, err
	}

	if err := trace.Start(file); err != nil {
		_ = file.Close()
		return nil, err
	}

	return func() error {
		trace.Stop()
		return file.Close()
	}, nil
}

type contextKey struct{}

// WithRequestID attaches a request/trace ID to the current goroutine.
// Use at the start of a net/http handler: ctx = agent.WithRequestID(ctx, r.Header.Get("X-Request-Id")).
// The ID is stored in context (retrieve with GetRequestID), set via pprof labels (for future trace
// support), and written to a sidecar file that Goroscope merges into goroutine metadata.
// The label key is configurable via GOROSCOPE_REQUEST_ID_KEY (default "request_id").
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	key := os.Getenv("GOROSCOPE_REQUEST_ID_KEY")
	if key == "" {
		key = DefaultRequestIDKey
	}
	ctx = context.WithValue(ctx, contextKey{}, requestID)
	labels := pprof.Labels(key, requestID)
	ctx = pprof.WithLabels(ctx, labels)
	pprof.SetGoroutineLabels(ctx)

	if tracePath := traceFilePath(); tracePath != "" {
		writeLabelToSidecar(tracePath+".labels", key, requestID)
	}
	return ctx
}

// GetRequestID returns the request ID from ctx if set by WithRequestID.
func GetRequestID(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

var labelsFileMu sync.Mutex

func writeLabelToSidecar(path, key, value string) {
	goID := currentGoroutineID()
	if goID <= 0 {
		return
	}
	line, err := json.Marshal(map[string]interface{}{
		"goroutine_id": goID,
		"labels":       map[string]string{key: value},
	})
	if err != nil {
		return
	}
	labelsFileMu.Lock()
	defer labelsFileMu.Unlock()
	//nolint:gosec // path is from GOROSCOPE_TRACE_FILE env set by goroscope
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, string(line))
	_ = f.Close()
}

func currentGoroutineID() int64 {
	buf := make([]byte, 64)
	n := runtime.Stack(buf, false)
	if n == 0 {
		return 0
	}
	if m := goroutineIDRE.FindSubmatch(buf[:n]); len(m) >= 2 {
		var id int64
		if _, err := fmt.Sscanf(string(m[1]), "%d", &id); err == nil {
			return id
		}
	}
	return 0
}
