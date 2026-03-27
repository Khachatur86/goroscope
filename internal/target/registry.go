// Package target manages multiple monitored Go processes, each with its own
// analysis engine and pprof poller.
package target

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/pprofpoll"
	"github.com/Khachatur86/goroscope/internal/session"
)

// Target represents a single monitored process.
type Target struct {
	ID       string
	Addr     string
	Label    string
	Engine   *analysis.Engine
	Sessions *session.Manager
	AddedAt  time.Time

	cancel context.CancelFunc
}

// AddInput holds parameters for Registry.Add (CS-5).
type AddInput struct {
	// Addr is the base URL of the target, e.g. "http://localhost:6060".
	Addr string
	// Label is an optional human-readable name shown in the UI dropdown.
	// Defaults to Addr when empty.
	Label string
	// PollInterval controls how often the pprof endpoint is polled.
	// Zero value defaults to 2 s.
	PollInterval time.Duration
	// Stderr receives poll-error log lines; io.Discard is used when nil.
	Stderr io.Writer
}

// Info is the JSON-serializable view of a Target, safe to return to callers.
type Info struct {
	ID      string    `json:"id"`
	Addr    string    `json:"addr"`
	Label   string    `json:"label"`
	AddedAt time.Time `json:"added_at"`
}

// Registry manages a set of monitored targets. Each target runs its own
// pprof poller goroutine whose lifetime is tied to a child context derived
// from the parent supplied to Add (CC-2).
type Registry struct {
	mu        sync.RWMutex
	targets   map[string]*Target
	defaultID string
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{targets: make(map[string]*Target)}
}

// Add creates a new Target, starts its pprof poller, and registers it.
// The poller goroutine is cancelled when either parentCtx is done or
// Registry.Remove is called for the returned target's ID.
func (r *Registry) Add(parentCtx context.Context, in AddInput) *Target {
	label := in.Label
	if label == "" {
		label = in.Addr
	}

	engine := analysis.NewEngine()
	sessions := session.NewManager()

	ctx, cancel := context.WithCancel(parentCtx)

	t := &Target{
		ID:       generateID(),
		Addr:     in.Addr,
		Label:    label,
		Engine:   engine,
		Sessions: sessions,
		AddedAt:  time.Now(),
		cancel:   cancel,
	}

	stderr := in.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	poller := pprofpoll.NewPoller(pprofpoll.PollInput{
		TargetURL:   in.Addr,
		Interval:    in.PollInterval,
		Engine:      engine,
		Sessions:    sessions,
		SessionName: label,
	})

	// CC-2: goroutine lifetime tied to ctx.
	go poller.Run(ctx, stderr)

	r.mu.Lock()
	r.targets[t.ID] = t
	if r.defaultID == "" {
		r.defaultID = t.ID
	}
	r.mu.Unlock()

	return t
}

// Remove cancels the poller goroutine and removes the target.
// Reports whether a target with that ID existed.
func (r *Registry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.targets[id]
	if !ok {
		return false
	}
	t.cancel()
	delete(r.targets, id)

	// Reassign default to another target if needed.
	if r.defaultID == id {
		r.defaultID = ""
		for newID := range r.targets {
			r.defaultID = newID
			break
		}
	}
	return true
}

// Get returns the Target for the given ID, if it exists.
func (r *Registry) Get(id string) (*Target, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.targets[id]
	return t, ok
}

// Default returns the first-added (default) target.
func (r *Registry) Default() (*Target, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultID == "" {
		return nil, false
	}
	t, ok := r.targets[r.defaultID]
	return t, ok
}

// List returns Info for all registered targets, sorted by AddedAt ascending.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Info, 0, len(r.targets))
	for _, t := range r.targets {
		out = append(out, Info{
			ID:      t.ID,
			Addr:    t.Addr,
			Label:   t.Label,
			AddedAt: t.AddedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AddedAt.Before(out[j].AddedAt)
	})
	return out
}

// Len returns the number of registered targets.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.targets)
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
