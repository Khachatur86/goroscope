// Package flightrec polls a goroscope Flight Recorder snapshot endpoint and
// feeds parsed goroutine captures into an analysis engine.
package flightrec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

// EngineLoader is the subset of *analysis.Engine used by the Poller.
type EngineLoader interface {
	LoadCapture(sess *model.Session, capture model.Capture)
}

// PollerInput holds configuration for a Flight Recorder Poller.
// Context is passed to Run, not stored here (CTX-1).
type PollerInput struct {
	// BaseURL is the target process base URL, e.g. "http://localhost:7071".
	// The poller appends /debug/goroscope/snapshot automatically.
	BaseURL string
	// Interval between snapshot polls. Default: 2 s.
	Interval time.Duration
	// Engine receives parsed captures.
	Engine EngineLoader
	// Sessions manages the session lifecycle.
	Sessions *session.Manager
	// SessionName is the name of the session created on first poll.
	SessionName string
	// HTTPClient is used for requests; nil uses a default 30 s timeout client.
	HTTPClient *http.Client
}

// Poller fetches Flight Recorder snapshots from a running Go process and
// loads parsed captures into an engine.
type Poller struct {
	in      PollerInput
	session *model.Session
	client  *http.Client
}

// NewPoller creates a Poller and registers a session.
func NewPoller(in PollerInput) *Poller {
	if in.Interval <= 0 {
		in.Interval = 2 * time.Second
	}
	client := in.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	p := &Poller{in: in, client: client}
	p.session = in.Sessions.StartSession(in.SessionName, in.BaseURL)
	return p
}

// PollOnce fetches a single snapshot and loads it into the engine.
// Returns an error if the snapshot cannot be fetched or parsed.
func (p *Poller) PollOnce(ctx context.Context) error {
	snapshotURL := strings.TrimRight(p.in.BaseURL, "/") + "/debug/goroscope/snapshot"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL, nil)
	if err != nil {
		return fmt.Errorf("build snapshot request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch snapshot from %s: %w", snapshotURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("snapshot endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read snapshot body: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("snapshot response is empty")
	}

	capture, err := tracebridge.BuildCaptureFromReader(ctx, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse flight recorder snapshot: %w", err)
	}
	capture.Target = p.in.BaseURL

	p.in.Engine.LoadCapture(p.session, tracebridge.BindCaptureSession(capture, p.session.ID))
	return nil
}

// Run polls the snapshot endpoint repeatedly until ctx is cancelled.
// Transient errors are logged to stderr; fatal failures stop the loop.
func (p *Poller) Run(ctx context.Context, stderr io.Writer) {
	ticker := time.NewTicker(p.in.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.PollOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				_, _ = fmt.Fprintf(stderr, "goroscope flight-recorder: %v\n", err)
			}
		}
	}
}
