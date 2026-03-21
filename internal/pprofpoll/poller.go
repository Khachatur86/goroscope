package pprofpoll

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
)

// EngineLoader is the subset of *analysis.Engine used by the Poller.
// Keeping it as an interface avoids a circular import between pprofpoll and analysis.
type EngineLoader interface {
	// LoadCapture replaces the engine's state with the provided snapshot.
	LoadCapture(sess *model.Session, capture model.Capture)
}

// PollInput holds all configuration for a Poller. Context is passed to Run,
// not stored here, so the struct satisfies CS-5 (input struct for >2 args).
type PollInput struct {
	// TargetURL is the base URL of the target process, e.g. "http://localhost:6060".
	// The poller appends /debug/pprof/goroutine?debug=2 automatically.
	TargetURL string
	// Interval between polls (default 2s).
	Interval time.Duration
	// Engine is the destination for captured goroutine state.
	Engine EngineLoader
	// Sessions manages the current session lifecycle.
	Sessions *session.Manager
	// SessionName is used when creating a new session.
	SessionName string
	// HTTPClient allows injecting a custom client (e.g. with auth headers).
	// If nil, a default client with a 10 s timeout is used.
	HTTPClient *http.Client
}

// Poller polls a pprof endpoint and feeds accumulated captures into an engine.
type Poller struct {
	in      PollInput
	session *model.Session

	mu       sync.Mutex
	events   []model.Event
	stacks   []model.StackSnapshot
	knownIDs map[int64]bool
	seq      uint64
	seqStack uint64
}

// NewPoller creates a Poller and starts a session on the session manager.
func NewPoller(in PollInput) *Poller {
	if in.Interval <= 0 {
		in.Interval = 2 * time.Second
	}
	if in.HTTPClient == nil {
		in.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	sess := in.Sessions.StartSession(in.SessionName, in.TargetURL)
	return &Poller{
		in:       in,
		session:  sess,
		knownIDs: make(map[int64]bool),
	}
}

// Session returns the session created for this polling run.
func (p *Poller) Session() *model.Session { return p.session }

// PollOnce performs a single poll and returns an error if the target is
// unreachable or the response cannot be parsed. It is used by callers to
// verify connectivity before starting the serve loop.
func (p *Poller) PollOnce(ctx context.Context) error {
	endpoint := strings.TrimRight(p.in.TargetURL, "/") + "/debug/pprof/goroutine?debug=2"
	return p.poll(ctx, endpoint)
}

// Run polls the pprof endpoint on a ticker until ctx is cancelled.
// It logs transient errors to stderr but does not return them — only a
// context cancellation causes Run to return.
func (p *Poller) Run(ctx context.Context, stderr io.Writer) {
	endpoint := strings.TrimRight(p.in.TargetURL, "/") + "/debug/pprof/goroutine?debug=2"
	if stderr == nil {
		stderr = io.Discard
	}

	ticker := time.NewTicker(p.in.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.in.Sessions.CompleteCurrent()
			return
		case <-ticker.C:
			if err := p.poll(ctx, endpoint); err != nil {
				_, _ = fmt.Fprintf(stderr, "goroscope attach: poll error: %v\n", err)
			}
		}
	}
}

// poll fetches the goroutine dump, parses it, and calls engine.LoadCapture.
func (p *Poller) poll(ctx context.Context, endpoint string) error {
	body, err := p.fetchDump(ctx, endpoint)
	if err != nil {
		return err
	}

	goroutines, err := parseGoroutineDump(body)
	if err != nil {
		return fmt.Errorf("parse goroutine dump: %w", err)
	}

	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	currentIDs := make(map[int64]bool, len(goroutines))
	for _, g := range goroutines {
		currentIDs[g.id] = true

		// Emit create event the first time we see this goroutine.
		if !p.knownIDs[g.id] {
			p.events = append(p.events, model.Event{
				SessionID:   p.session.ID,
				Seq:         p.nextSeq(),
				Timestamp:   now,
				Kind:        model.EventKindGoroutineCreate,
				GoroutineID: g.id,
				ParentID:    g.parentID,
			})
			p.knownIDs[g.id] = true
		}

		// Always emit a state event — the engine deduplicates no-op transitions.
		p.events = append(p.events, model.Event{
			SessionID:   p.session.ID,
			Seq:         p.nextSeq(),
			Timestamp:   now,
			Kind:        model.EventKindGoroutineState,
			GoroutineID: g.id,
			State:       g.state,
			Reason:      g.reason,
			ResourceID:  g.resourceID,
		})

		// Emit a stack snapshot if we have frames.
		if len(g.frames) > 0 {
			stackID := fmt.Sprintf("pprof-%d-%d", g.id, now.UnixNano())
			p.stacks = append(p.stacks, model.StackSnapshot{
				SessionID:   p.session.ID,
				Seq:         p.nextStackSeq(),
				Timestamp:   now,
				StackID:     stackID,
				GoroutineID: g.id,
				Frames:      g.frames,
			})
			// Link the state event to this stack.
			p.events[len(p.events)-1].StackID = stackID
		}
	}

	// Goroutines that disappeared → emit end events.
	for id := range p.knownIDs {
		if !currentIDs[id] {
			p.events = append(p.events, model.Event{
				SessionID:   p.session.ID,
				Seq:         p.nextSeq(),
				Timestamp:   now,
				Kind:        model.EventKindGoroutineEnd,
				GoroutineID: id,
			})
			delete(p.knownIDs, id)
		}
	}

	// Rebuild the full capture from accumulated events and push to the engine.
	// LoadCapture replaces all engine state, so we feed the complete history
	// each time — simple and correct for a polling-based source.
	capture := model.Capture{
		Name:   p.session.Name,
		Target: p.in.TargetURL,
		Events: append([]model.Event(nil), p.events...),
		Stacks: append([]model.StackSnapshot(nil), p.stacks...),
	}
	p.in.Engine.LoadCapture(p.session, capture)
	return nil
}

// fetchDump performs the HTTP GET and returns the response body as a string.
func (p *Poller) fetchDump(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := p.in.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", endpoint, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(data), nil
}

func (p *Poller) nextSeq() uint64 {
	p.seq++
	return p.seq
}

func (p *Poller) nextStackSeq() uint64 {
	p.seqStack++
	return p.seqStack
}
