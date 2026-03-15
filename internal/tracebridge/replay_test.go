package tracebridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestLoadDemoCapture(t *testing.T) {
	capture, err := LoadDemoCapture()
	if err != nil {
		t.Fatalf("expected embedded demo capture to load: %v", err)
	}

	if capture.Name == "" {
		t.Fatal("expected demo capture to have a name")
	}
	if len(capture.Events) == 0 {
		t.Fatal("expected demo capture to include events")
	}
	if len(capture.Stacks) == 0 {
		t.Fatal("expected demo capture to include stack snapshots")
	}
	if capture.ParentIDs[42] != 1 || capture.ParentIDs[77] != 42 {
		t.Fatalf("expected demo capture parent_ids {42:1,77:42}, got %v", capture.ParentIDs)
	}
}

func TestLoadCaptureFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.gtrace")
	content := `{
  "name": "test-capture",
  "events": [
    {
      "seq": 1,
      "timestamp": "2026-03-14T12:00:00Z",
      "kind": "goroutine.create",
      "goroutine_id": 1
    }
  ]
}`

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp capture: %v", err)
	}

	capture, err := LoadCaptureFile(path)
	if err != nil {
		t.Fatalf("expected capture file to load: %v", err)
	}

	if capture.Name != "test-capture" {
		t.Fatalf("expected name %q, got %q", "test-capture", capture.Name)
	}
	if len(capture.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(capture.Events))
	}
}

func TestLoadCaptureFile_MalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.gtrace")
	if err := os.WriteFile(path, []byte(`{invalid json`), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := LoadCaptureFile(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode-related error, got %v", err)
	}
}

func TestLoadCaptureFile_EmptyEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.gtrace")
	content := `{"name":"empty","events":[]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := LoadCaptureFile(path)
	if err == nil {
		t.Fatal("expected error for capture with no events")
	}
	if !strings.Contains(err.Error(), "no events") {
		t.Fatalf("expected no-events error, got %v", err)
	}
}

func TestBindCaptureSessionPreservesParentIDs(t *testing.T) {
	capture := model.Capture{
		Events: []model.Event{
			{Seq: 1, GoroutineID: 1},
		},
		ParentIDs: map[int64]int64{
			42: 1,
			77: 42,
		},
	}

	bound := BindCaptureSession(capture, "sess_123")

	if bound.Events[0].SessionID != "sess_123" {
		t.Fatalf("expected bound event session_id %q, got %q", "sess_123", bound.Events[0].SessionID)
	}
	if bound.ParentIDs[42] != 1 || bound.ParentIDs[77] != 42 {
		t.Fatalf("expected bound parent_ids {42:1,77:42}, got %v", bound.ParentIDs)
	}

	bound.ParentIDs[42] = 99
	if capture.ParentIDs[42] != 1 {
		t.Fatalf("expected source capture ParentIDs to remain unchanged, got %v", capture.ParentIDs)
	}
}
