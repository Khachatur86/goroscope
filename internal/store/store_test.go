package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func makeCapture(goroutines int) model.Capture {
	now := time.Unix(0, 1_000_000_000)
	events := make([]model.Event, goroutines*2)
	for i := range goroutines {
		gid := int64(i + 1)
		events[i*2] = model.Event{
			GoroutineID: gid,
			Kind:        model.EventKindGoroutineCreate,
			Timestamp:   now,
		}
		events[i*2+1] = model.Event{
			GoroutineID: gid,
			Kind:        model.EventKindGoroutineState,
			Timestamp:   now.Add(time.Second),
		}
	}
	return model.Capture{Name: "test", Events: events}
}

func TestStore_SaveAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	capture := makeCapture(3)
	now := time.Now()
	path, err := s.Save(SaveInput{
		Capture:   capture,
		Target:    "test://target",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if path == "" {
		t.Fatal("Save returned empty path")
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Target != "test://target" {
		t.Errorf("Target = %q, want %q", e.Target, "test://target")
	}
	if e.GoroutineCount != 3 {
		t.Errorf("GoroutineCount = %d, want 3", e.GoroutineCount)
	}
	if e.DurationNS != int64(time.Second) {
		t.Errorf("DurationNS = %d, want %d", e.DurationNS, int64(time.Second))
	}
	if filepath.Base(s.FilePath(e)) != e.Filename {
		t.Errorf("FilePath mismatch: %s vs %s", s.FilePath(e), e.Filename)
	}
}

func TestStore_MultipleEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := time.Now()
	for i := range 3 {
		_, err := s.Save(SaveInput{
			Capture:   makeCapture(i + 1),
			Target:    "target",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
}

func TestStore_EmptyList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("want 0 entries, got %d", len(entries))
	}
}

func TestCaptureDurationNS(t *testing.T) {
	t.Parallel()

	c := makeCapture(2)
	d := captureDurationNS(c)
	if d != int64(time.Second) {
		t.Errorf("durationNS = %d, want %d", d, int64(time.Second))
	}
}

func TestCaptureGoroutineCount(t *testing.T) {
	t.Parallel()

	c := makeCapture(5)
	n := captureGoroutineCount(c)
	if n != 5 {
		t.Errorf("goroutineCount = %d, want 5", n)
	}
}
