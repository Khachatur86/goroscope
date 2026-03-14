package session

import (
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func TestManagerCompleteCurrent(t *testing.T) {
	manager := NewManager()
	manager.StartSession("demo", "./demo")

	manager.CompleteCurrent()

	current := manager.Current()
	if current == nil {
		t.Fatal("expected active session")
	}
	if current.Status != model.SessionStatusCompleted {
		t.Fatalf("expected completed status, got %s", current.Status)
	}
	if current.EndedAt == nil {
		t.Fatal("expected ended timestamp to be set")
	}
	if current.Error != "" {
		t.Fatalf("expected empty error, got %q", current.Error)
	}
}

func TestManagerFailCurrent(t *testing.T) {
	manager := NewManager()
	manager.StartSession("demo", "./demo")

	manager.FailCurrent("target exited with status 1")

	current := manager.Current()
	if current == nil {
		t.Fatal("expected active session")
	}
	if current.Status != model.SessionStatusFailed {
		t.Fatalf("expected failed status, got %s", current.Status)
	}
	if current.EndedAt == nil {
		t.Fatal("expected ended timestamp to be set")
	}
	if current.Error != "target exited with status 1" {
		t.Fatalf("unexpected error message %q", current.Error)
	}
}
