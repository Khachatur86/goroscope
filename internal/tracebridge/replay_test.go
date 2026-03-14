package tracebridge

import (
	"os"
	"path/filepath"
	"testing"
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

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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
