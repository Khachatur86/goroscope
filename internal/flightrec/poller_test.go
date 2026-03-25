package flightrec

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime/trace"
	"sync"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
)

// captureEngine records LoadCapture calls for assertions.
type captureEngine struct {
	mu       sync.Mutex
	captures []model.Capture
}

func (e *captureEngine) LoadCapture(_ *model.Session, c model.Capture) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.captures = append(e.captures, c)
}

func (e *captureEngine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.captures)
}

// captureTrace generates a minimal binary runtime trace for use in tests.
func captureTrace(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		t.Fatalf("trace.Start: %v", err)
	}
	trace.Stop()
	return buf.Bytes()
}

func TestPoller_PollOnce_Success(t *testing.T) {
	t.Parallel()

	traceData := captureTrace(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug/goroscope/snapshot" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(traceData)
	}))
	defer srv.Close()

	eng := &captureEngine{}
	mgr := session.NewManager()
	p := NewPoller(PollerInput{
		BaseURL:     srv.URL,
		Engine:      eng,
		Sessions:    mgr,
		SessionName: "test",
	})

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if eng.count() != 1 {
		t.Errorf("expected 1 capture, got %d", eng.count())
	}
}

func TestPoller_PollOnce_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	eng := &captureEngine{}
	mgr := session.NewManager()
	p := NewPoller(PollerInput{
		BaseURL:     srv.URL,
		Engine:      eng,
		Sessions:    mgr,
		SessionName: "test",
	})

	if err := p.PollOnce(context.Background()); err == nil {
		t.Error("expected error for 500 response")
	}
	if eng.count() != 0 {
		t.Errorf("expected 0 captures after error, got %d", eng.count())
	}
}

func TestPoller_PollOnce_EmptyBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	eng := &captureEngine{}
	mgr := session.NewManager()
	p := NewPoller(PollerInput{
		BaseURL:     srv.URL,
		Engine:      eng,
		Sessions:    mgr,
		SessionName: "test",
	})

	if err := p.PollOnce(context.Background()); err == nil {
		t.Error("expected error for empty body")
	}
}

func TestPoller_Run_ContextCancelled(t *testing.T) {
	t.Parallel()

	traceData := captureTrace(t)
	var reqCount int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		reqCount++
		mu.Unlock()
		_, _ = w.Write(traceData)
	}))
	defer srv.Close()

	eng := &captureEngine{}
	mgr := session.NewManager()
	p := NewPoller(PollerInput{
		BaseURL:     srv.URL,
		Interval:    50 * time.Millisecond,
		Engine:      eng,
		Sessions:    mgr,
		SessionName: "test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	p.Run(ctx, &buf)

	mu.Lock()
	n := reqCount
	mu.Unlock()

	if n == 0 {
		t.Error("expected at least one poll before context was cancelled")
	}
}
