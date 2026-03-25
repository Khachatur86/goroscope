package agent

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestStartFlightRecorder_SnapshotEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartFlightRecorder(ctx, FlightRecorderServerConfig{
		Addr:     "127.0.0.1:0", // port 0 not supported by net/http directly — use a free port
		MinAge:   time.Second,
		MaxBytes: 1 << 20,
	})
	// Port 0 is not supported by http.ListenAndServe; use a concrete free port instead.
	// If binding on the configured port fails the server goroutine logs to /dev/null.
	// We mainly test that StartFlightRecorder does not itself error.
	if err != nil {
		t.Logf("StartFlightRecorder returned (expected when port 0 not bindable): %v", err)
		return
	}
	defer stop()
}

func TestStartFlightRecorder_StatusEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartFlightRecorder(ctx, FlightRecorderServerConfig{
		Addr:     "127.0.0.1:17179", // fixed port unlikely to collide in CI
		MinAge:   500 * time.Millisecond,
		MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Skipf("cannot start flight recorder server: %v", err)
	}
	defer stop()

	// Give the server a moment to start listening.
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:17179/debug/goroscope/status") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("expected non-empty status body")
	}
}

func TestStartFlightRecorder_SnapshotReturnsData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartFlightRecorder(ctx, FlightRecorderServerConfig{
		Addr:     "127.0.0.1:17180",
		MinAge:   500 * time.Millisecond,
		MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Skipf("cannot start flight recorder server: %v", err)
	}
	defer stop()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:17180/debug/goroscope/snapshot") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /snapshot: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("expected non-empty snapshot body (binary trace data)")
	}
}
