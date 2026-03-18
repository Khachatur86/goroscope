package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

func TestRun_Version(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run version: %v", err)
	}
	out := stdout.String()
	if out == "" {
		t.Error("expected version output, got empty")
	}
	if strings.Contains(out, "\n\n") {
		t.Errorf("version should be single line, got: %q", out)
	}
}

func TestRun_Check_NoHints(t *testing.T) {
	t.Parallel()

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		t.Fatalf("load demo capture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.gtrace")
	if err := tracebridge.SaveCaptureFile(path, capture); err != nil {
		t.Fatalf("save capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run(context.Background(), []string{"check", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run check (no hints expected): %v", err)
	}
	if !strings.Contains(stdout.String(), "No deadlock hints") {
		t.Errorf("expected 'No deadlock hints' in stdout, got: %s", stdout.String())
	}
}

func TestRun_Check_WithHints(t *testing.T) {
	t.Parallel()

	// Capture with resource cycle: G1->G2->G3->G1
	content := `{
  "name": "deadlock-test",
  "events": [
    {"seq": 1, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 1},
    {"seq": 2, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 1, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x1"},
    {"seq": 3, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 2},
    {"seq": 4, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 2, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x2"},
    {"seq": 5, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 3},
    {"seq": 6, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 3, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x3"}
  ],
  "resources": [
    {"from_goroutine_id": 1, "to_goroutine_id": 2, "resource_id": "chan:0x1"},
    {"from_goroutine_id": 2, "to_goroutine_id": 3, "resource_id": "chan:0x2"},
    {"from_goroutine_id": 3, "to_goroutine_id": 1, "resource_id": "chan:0x3"}
  ]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "deadlock.gtrace")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"check", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected check to fail with deadlock hints, got nil")
	}
	if !strings.Contains(err.Error(), "deadlock hints") {
		t.Errorf("expected 'deadlock hints' in error, got: %v", err)
	}
}

func TestRun_Check_MissingFile(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"check", "/nonexistent/path.gtrace"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
