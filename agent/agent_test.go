package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWithRequestID_EmptyID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out := WithRequestID(ctx, "")
	if out != ctx {
		t.Errorf("WithRequestID(ctx, \"\") should return ctx unchanged, got %p", out)
	}
}

func TestGetRequestID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if got := GetRequestID(ctx); got != "" {
		t.Errorf("GetRequestID(empty ctx) = %q, want \"\"", got)
	}
	ctx = WithRequestID(ctx, "req-123")
	if got := GetRequestID(ctx); got != "req-123" {
		t.Errorf("GetRequestID(WithRequestID(ctx, \"req-123\")) = %q, want \"req-123\"", got)
	}
}

func TestCurrentGoroutineID(t *testing.T) {
	t.Parallel()
	id := currentGoroutineID()
	if id <= 0 {
		t.Errorf("currentGoroutineID() = %d, want positive", id)
	}
}

func TestWithRequestID_WritesSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.out")
	os.Setenv(traceFileEnv, tracePath)
	defer os.Unsetenv(traceFileEnv)

	ctx := WithRequestID(context.Background(), "test-req-id")
	_ = ctx

	data, err := os.ReadFile(tracePath + ".labels")
	if err != nil {
		t.Fatalf("read labels file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("labels file is empty")
	}
	// Should contain goroutine_id and request_id
	if len(data) < 20 {
		t.Errorf("labels file too short: %q", string(data))
	}
}
