package target_test

import (
	"context"
	"testing"

	"github.com/Khachatur86/goroscope/internal/target"
)

func TestRegistry_AddAndList(t *testing.T) {
	t.Parallel()

	reg := target.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t1 := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6060", Label: "svc-a"})
	t2 := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6061"})

	if reg.Len() != 2 {
		t.Fatalf("expected 2 targets, got %d", reg.Len())
	}

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("list: expected 2 entries, got %d", len(list))
	}
	// List is sorted by AddedAt; t1 was added first.
	if list[0].ID != t1.ID {
		t.Errorf("expected first entry to be t1 (%s), got %s", t1.ID, list[0].ID)
	}
	if list[1].ID != t2.ID {
		t.Errorf("expected second entry to be t2 (%s), got %s", t2.ID, list[1].ID)
	}
	if list[0].Label != "svc-a" {
		t.Errorf("label: got %q, want %q", list[0].Label, "svc-a")
	}
	// When label is empty, defaults to addr.
	if list[1].Label != "http://localhost:6061" {
		t.Errorf("default label: got %q, want %q", list[1].Label, "http://localhost:6061")
	}
}

func TestRegistry_Default(t *testing.T) {
	t.Parallel()

	reg := target.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Empty registry.
	if _, ok := reg.Default(); ok {
		t.Error("expected Default to return false on empty registry")
	}

	t1 := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6060"})
	_ = reg.Add(ctx, target.AddInput{Addr: "http://localhost:6061"})

	def, ok := reg.Default()
	if !ok {
		t.Fatal("expected Default to return true after adding targets")
	}
	if def.ID != t1.ID {
		t.Errorf("default should be first added target; got %s, want %s", def.ID, t1.ID)
	}
}

func TestRegistry_Remove(t *testing.T) {
	t.Parallel()

	reg := target.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t1 := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6060"})
	t2 := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6061"})

	// Remove non-existent.
	if reg.Remove("nope") {
		t.Error("Remove of unknown ID should return false")
	}

	// Remove first (default) target.
	if !reg.Remove(t1.ID) {
		t.Fatal("Remove of existing target should return true")
	}
	if reg.Len() != 1 {
		t.Fatalf("expected 1 target after remove, got %d", reg.Len())
	}

	// Default should now be t2.
	def, ok := reg.Default()
	if !ok {
		t.Fatal("expected a default after removing first target")
	}
	if def.ID != t2.ID {
		t.Errorf("default after remove: got %s, want %s", def.ID, t2.ID)
	}

	// Remove last target; default becomes empty.
	if !reg.Remove(t2.ID) {
		t.Fatal("Remove of last target should return true")
	}
	if _, ok := reg.Default(); ok {
		t.Error("expected no default after removing all targets")
	}
}

func TestRegistry_Get(t *testing.T) {
	t.Parallel()

	reg := target.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tgt := reg.Add(ctx, target.AddInput{Addr: "http://localhost:6060", Label: "foo"})

	got, ok := reg.Get(tgt.ID)
	if !ok {
		t.Fatal("Get should find existing target")
	}
	if got.Label != "foo" {
		t.Errorf("label: got %q, want %q", got.Label, "foo")
	}

	if _, ok := reg.Get("missing"); ok {
		t.Error("Get of missing ID should return false")
	}
}
