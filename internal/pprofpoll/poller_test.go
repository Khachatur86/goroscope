package pprofpoll

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
)

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeEngine struct {
	mu      sync.Mutex
	calls   int
	lastCap model.Capture
}

func (e *fakeEngine) LoadCapture(_ *model.Session, cap model.Capture) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.lastCap = cap
}

func (e *fakeEngine) loadCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// pprofServer returns a test HTTP server serving a static pprof goroutine dump.
func pprofServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/goroutine", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestNewPoller_CreatesSession(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	eng := &fakeEngine{}
	srv := pprofServer(t, sampleDump)
	defer srv.Close()

	p := NewPoller(PollInput{
		TargetURL:   srv.URL,
		Engine:      eng,
		Sessions:    mgr,
		SessionName: "test",
	})

	if p.Session() == nil {
		t.Fatal("expected non-nil session")
	}
	if p.Session().Name != "test" {
		t.Errorf("want session name 'test', got %q", p.Session().Name)
	}
}

func TestPollOnce_Success(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	eng := &fakeEngine{}
	srv := pprofServer(t, sampleDump)
	defer srv.Close()

	p := NewPoller(PollInput{
		TargetURL: srv.URL,
		Engine:    eng,
		Sessions:  mgr,
	})

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if eng.loadCount() != 1 {
		t.Errorf("want 1 LoadCapture call, got %d", eng.loadCount())
	}
}

func TestPollOnce_LoadsGoroutines(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	eng := &fakeEngine{}
	srv := pprofServer(t, sampleDump)
	defer srv.Close()

	p := NewPoller(PollInput{
		TargetURL: srv.URL,
		Engine:    eng,
		Sessions:  mgr,
	})

	if err := p.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	// sampleDump has 4 goroutines → 4 create events on first poll.
	var creates int
	for _, ev := range eng.lastCap.Events {
		if ev.Kind == model.EventKindGoroutineCreate {
			creates++
		}
	}
	if creates != 4 {
		t.Errorf("want 4 create events, got %d", creates)
	}
}

func TestPollOnce_Unreachable(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	eng := &fakeEngine{}

	p := NewPoller(PollInput{
		TargetURL:  "http://127.0.0.1:1", // nothing listening here
		Engine:     eng,
		Sessions:   mgr,
		HTTPClient: &http.Client{Timeout: 500 * time.Millisecond},
	})

	if err := p.PollOnce(context.Background()); err == nil {
		t.Fatal("expected error for unreachable target, got nil")
	}
}

func TestPollOnce_NonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	mgr := session.NewManager()
	eng := &fakeEngine{}

	p := NewPoller(PollInput{
		TargetURL: srv.URL,
		Engine:    eng,
		Sessions:  mgr,
	})

	if err := p.PollOnce(context.Background()); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestRun_PollsMultipleTimes(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	eng := &fakeEngine{}
	srv := pprofServer(t, sampleDump)
	defer srv.Close()

	p := NewPoller(PollInput{
		TargetURL: srv.URL,
		Interval:  20 * time.Millisecond,
		Engine:    eng,
		Sessions:  mgr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	p.Run(ctx, nil)

	// At 20 ms interval over ~80 ms we expect at least 3 polls.
	if eng.loadCount() < 3 {
		t.Errorf("want ≥3 LoadCapture calls, got %d", eng.loadCount())
	}
}

func TestRun_IncrementalEvents(t *testing.T) {
	t.Parallel()

	// Second dump adds goroutine 99 and drops goroutine 1.
	dump2 := `goroutine 18 [chan receive]:
net/http.(*connReader).Read(0xc0003e8000, {0xc00076e000, 0x1000, 0x1000})
	/usr/local/go/src/net/http/server.go:789 +0x149

goroutine 42 [sync.Mutex.Lock]:
main.(*Worker).Process(0xc000b20000)
	/home/user/app/worker.go:88 +0x5c

goroutine 99 [syscall]:
syscall.Syscall(0x1, 0x5, 0xc0004e2000, 0x200)
	/usr/local/go/src/net/http/server.go:789 +0x149

goroutine 200 [running]:
main.newWorker()
	/home/user/app/worker.go:10 +0x1
`

	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		body := sampleDump
		if call > 1 {
			body = dump2
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	mgr := session.NewManager()
	eng := &fakeEngine{}

	p := NewPoller(PollInput{
		TargetURL: srv.URL,
		Interval:  20 * time.Millisecond,
		Engine:    eng,
		Sessions:  mgr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	p.Run(ctx, nil)

	// After two polls the event list should contain:
	// • creates for 4 original goroutines (goroutines 1, 18, 42, 99)
	// • a create for goroutine 200 (new in dump2)
	// • an end event for goroutine 1 (disappeared in dump2)
	var creates, ends int
	for _, ev := range eng.lastCap.Events {
		switch ev.Kind {
		case model.EventKindGoroutineCreate:
			creates++
		case model.EventKindGoroutineEnd:
			ends++
		}
	}
	if creates < 5 {
		t.Errorf("want ≥5 create events (4 initial + goroutine 200), got %d", creates)
	}
	if ends < 1 {
		t.Errorf("want ≥1 end event (goroutine 1 disappeared), got %d", ends)
	}
}
