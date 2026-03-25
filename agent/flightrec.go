package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"runtime/trace"
	"time"
)

// FlightRecorderServerConfig controls the embedded Flight Recorder HTTP server.
// Zero values are replaced with the defaults documented on each field.
// Context is not stored here — pass it to StartFlightRecorder.
type FlightRecorderServerConfig struct {
	// Addr is the listen address for the snapshot HTTP server.
	// Default: reads GOROSCOPE_SNAPSHOT_ADDR env, falls back to "127.0.0.1:7071".
	Addr string

	// MinAge is the lower-bound on event age kept in the ring buffer.
	// Default: 10 s.
	MinAge time.Duration

	// MaxBytes is the upper-bound on ring-buffer size.
	// Default: 16 MiB.
	MaxBytes uint64

	// AnomalyThreshold is the goroutine count above which the recorder
	// automatically writes a snapshot to AnomalyDir.
	// 0 disables anomaly detection.
	AnomalyThreshold int

	// AnomalyDir is the directory for auto-saved snapshots.
	// Default: reads GOROSCOPE_ANOMALY_DIR env, falls back to os.TempDir().
	AnomalyDir string
}

// StartFlightRecorder starts a runtime/trace.FlightRecorder and an HTTP server
// that exposes GET /debug/goroscope/snapshot (binary trace) and GET
// /debug/goroscope/status (JSON health info).
//
// At most one FlightRecorder may be active at a time; calling StartFlightRecorder
// while another is running returns an error.
//
// The returned stop function shuts down the HTTP server and stops the recorder.
// Calling stop more than once is safe.
func StartFlightRecorder(ctx context.Context, cfg FlightRecorderServerConfig) (stop func(), err error) {
	addr := cfg.Addr
	if addr == "" {
		addr = os.Getenv("GOROSCOPE_SNAPSHOT_ADDR")
	}
	if addr == "" {
		addr = "127.0.0.1:7071"
	}

	minAge := cfg.MinAge
	if minAge <= 0 {
		minAge = 10 * time.Second
	}

	maxBytes := cfg.MaxBytes
	if maxBytes == 0 {
		maxBytes = 16 << 20 // 16 MiB
	}

	anomalyDir := cfg.AnomalyDir
	if anomalyDir == "" {
		anomalyDir = os.Getenv("GOROSCOPE_ANOMALY_DIR")
	}
	if anomalyDir == "" {
		anomalyDir = os.TempDir()
	}

	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{
		MinAge:   minAge,
		MaxBytes: maxBytes,
	})
	if err := fr.Start(); err != nil {
		return nil, fmt.Errorf("start flight recorder: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/goroscope/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := fr.WriteTo(w); err != nil {
			// Headers are already sent; best effort logging only.
			_, _ = fmt.Fprintf(io.Discard, "flight recorder WriteTo: %v", err)
		}
	})
	mux.HandleFunc("/debug/goroscope/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"enabled":%v,"goroutines":%d,"anomaly_threshold":%d}`,
			fr.Enabled(), runtime.NumGoroutine(), cfg.AnomalyThreshold,
		)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	// Start anomaly-detection loop if configured.
	anomalyCtx, anomalyCancel := context.WithCancel(ctx)
	if cfg.AnomalyThreshold > 0 {
		go detectGoroutineAnomaly(anomalyCtx, fr, cfg.AnomalyThreshold, anomalyDir)
	}

	var stopOnce bool
	stop = func() {
		if stopOnce {
			return
		}
		stopOnce = true
		anomalyCancel()
		_ = server.Shutdown(context.Background())
		fr.Stop()
	}

	// Propagate context cancellation.
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-errCh:
		}
	}()

	return stop, nil
}

// detectGoroutineAnomaly polls runtime.NumGoroutine() and writes a snapshot
// when the count exceeds threshold. It waits 30 s between checks and respects ctx.
func detectGoroutineAnomaly(ctx context.Context, fr *trace.FlightRecorder, threshold int, dir string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var lastSnap int64 // Unix seconds of last snapshot to avoid flooding
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n := runtime.NumGoroutine()
			if n <= threshold {
				continue
			}
			now := time.Now().Unix()
			if now-lastSnap < 60 { // at most one anomaly snapshot per minute
				continue
			}
			lastSnap = now
			writeAnomalySnapshot(fr, dir, n)
		}
	}
}

// writeAnomalySnapshot saves the current flight recorder window to a timestamped file.
func writeAnomalySnapshot(fr *trace.FlightRecorder, dir string, goroutineCount int) {
	name := fmt.Sprintf("goroscope-anomaly-%s-g%d.trace",
		time.Now().UTC().Format("20060102T150405Z"),
		goroutineCount,
	)
	path := dir + "/" + name
	//nolint:gosec // path is constructed from a controlled directory + safe timestamp
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fr.WriteTo(f)
}
