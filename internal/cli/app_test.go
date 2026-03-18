package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
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

func TestRun_Export_CSV(t *testing.T) {
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
	err = Run(context.Background(), []string{"export", "--format=csv", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run export csv: %v", err)
	}

	r := csv.NewReader(strings.NewReader(stdout.String()))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header + at least 1 row, got %d rows", len(rows))
	}
	header := rows[0]
	wantCols := []string{"goroutine_id", "state", "start_ns", "end_ns", "reason", "resource_id"}
	for _, w := range wantCols {
		found := false
		for _, h := range header {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected column %q in header %v", w, header)
		}
	}
}

func TestRun_Export_JSON(t *testing.T) {
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
	err = Run(context.Background(), []string{"export", "--format=json", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run export json: %v", err)
	}

	var body struct {
		Segments []map[string]any `json:"segments"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body.Segments == nil {
		t.Error("expected segments array, got nil")
	}
	if len(body.Segments) > 0 {
		seg := body.Segments[0]
		for _, key := range []string{"goroutine_id", "state", "start_ns", "end_ns"} {
			if _, ok := seg[key]; !ok {
				t.Errorf("expected segment to have %q, got %v", key, seg)
			}
		}
	}
}
