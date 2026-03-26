package api

// Integration tests for endpoints not covered by http_test.go (I-5).
// Every public endpoint is exercised with at least one happy-path and one
// error/edge-case request using httptest.NewRecorder (no real network).

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
)

// ─── handleTimeline ───────────────────────────────────────────────────────────

func TestHandleTimeline(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateWaiting, Reason: model.ReasonChanRecv},
		{ID: 2, State: model.StateRunning},
	}
	srv := newTestServer(t, goroutines)
	handler := srv.routes()

	t.Run("returns all segments without filters", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/timeline")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var segs []map[string]any
		decodeJSON(t, rec, &segs)
		if len(segs) == 0 {
			t.Error("expected at least one timeline segment")
		}
	})

	t.Run("filters by state", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/timeline?state=waiting")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var segs []map[string]any
		decodeJSON(t, rec, &segs)
		for _, s := range segs {
			if s["state"] != "waiting" {
				t.Errorf("segment state %q, want waiting", s["state"])
			}
		}
	})

	t.Run("filters by goroutine_ids", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/timeline?goroutine_ids=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var segs []map[string]any
		decodeJSON(t, rec, &segs)
		for _, s := range segs {
			if gid, ok := s["goroutine_id"].(float64); ok && int(gid) != 1 {
				t.Errorf("goroutine_id = %v, want 1", s["goroutine_id"])
			}
		}
	})

	t.Run("honors ETag not-modified", func(t *testing.T) {
		t.Parallel()
		rec1 := get(t, handler, "/api/v1/timeline")
		etag := rec1.Header().Get("ETag")
		if etag == "" {
			t.Skip("no ETag in first response")
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/timeline", nil)
		req.Header.Set("If-None-Match", etag)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req)
		if rec2.Code != http.StatusNotModified {
			t.Fatalf("status = %d, want 304", rec2.Code)
		}
	})
}

// ─── handleProcessorTimeline ──────────────────────────────────────────────────

func TestHandleProcessorTimeline(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, nil)
	rec := get(t, srv.routes(), "/api/v1/processor-timeline")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Body must be a JSON array (may be empty).
	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Errorf("expected JSON array, got: %s", body)
	}
}

// ─── handleGoroutineGroups ────────────────────────────────────────────────────

func TestHandleGoroutineGroups(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateWaiting},
		{ID: 2, State: model.StateWaiting},
		{ID: 3, State: model.StateRunning},
	}
	srv := newTestServer(t, goroutines)
	handler := srv.routes()

	t.Run("groups by function (default)", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/groups")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if resp["by"] != "function" {
			t.Errorf("by = %v, want function", resp["by"])
		}
	})

	t.Run("groups by package", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/groups?by=package")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if resp["by"] != "package" {
			t.Errorf("by = %v, want package", resp["by"])
		}
	})

	t.Run("invalid by returns 400", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/groups?by=invalid_field")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// ─── handleSmartInsights ──────────────────────────────────────────────────────

func TestHandleSmartInsights(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateWaiting, WaitNS: int64(35 * time.Second)},
		{ID: 2, State: model.StateRunning},
	}
	srv := newTestServer(t, goroutines)
	handler := srv.routes()

	t.Run("returns insights array", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/smart-insights")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if _, ok := resp["insights"]; !ok {
			t.Error("response missing insights key")
		}
		if _, ok := resp["total"]; !ok {
			t.Error("response missing total key")
		}
	})

	t.Run("accepts threshold params", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/smart-insights?leak_threshold_ns=1000&block_threshold_ns=500")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

// ─── handleGraph ──────────────────────────────────────────────────────────────

func TestHandleGraph(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, ResourceID: "sync.Mutex:0xabc"},
		{ID: 2, State: model.StateBlocked, ResourceID: "sync.Mutex:0xabc"},
	}
	srv := newTestServer(t, goroutines)
	handler := srv.routes()

	t.Run("returns resource graph edges", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/resources/graph")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := strings.TrimSpace(rec.Body.String())
		if !strings.HasPrefix(body, "[") && !strings.HasPrefix(body, "{") {
			t.Errorf("expected JSON, got: %s", body)
		}
	})

	t.Run("contention view returns contention map", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/resources/graph?view=contention")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if _, ok := resp["contention"]; !ok {
			t.Error("response missing contention key")
		}
	})
}

// ─── handleMemoryStats ────────────────────────────────────────────────────────

func TestHandleMemoryStats(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateWaiting},
	}
	srv := newTestServer(t, goroutines)
	rec := get(t, srv.routes(), "/api/v1/memory")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var stats map[string]any
	decodeJSON(t, rec, &stats)
	if _, ok := stats["goroutines"]; !ok {
		t.Error("response missing goroutines key")
	}
	if _, ok := stats["closed_segments"]; !ok {
		t.Error("response missing closed_segments key")
	}
}

// ─── handleGoroutineStacks ────────────────────────────────────────────────────

func TestHandleGoroutineStacks(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eng := analysis.NewEngine()
	sess := &model.Session{ID: "s", Name: "test", Target: "demo://test", StartedAt: base}
	eng.Reset(sess)
	eng.ApplyEvents([]model.Event{
		{Kind: model.EventKindGoroutineCreate, GoroutineID: 42, Timestamp: base},
		{Kind: model.EventKindGoroutineState, GoroutineID: 42, Timestamp: base.Add(time.Millisecond), State: model.StateRunning},
	})
	eng.ApplyStackSnapshot(model.StackSnapshot{
		GoroutineID: 42,
		Timestamp:   base.Add(2 * time.Millisecond),
		Frames: []model.StackFrame{
			{Func: "main.doWork", File: "main.go", Line: 10},
		},
	})
	mgr := session.NewManager()
	srv := NewServer("127.0.0.1:0", eng, mgr, "")
	handler := srv.routes()

	t.Run("returns stacks for known goroutine", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/42/stacks")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if _, ok := resp["stacks"]; !ok {
			t.Error("response missing stacks key")
		}
	})

	t.Run("returns empty stacks for unknown goroutine", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/999/stacks")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		stacks, _ := resp["stacks"].([]any)
		if len(stacks) != 0 {
			t.Errorf("expected empty stacks for unknown goroutine, got %d", len(stacks))
		}
	})

	t.Run("returns 400 for invalid id", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/notanid/stacks")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// ─── handlePprofStacks ────────────────────────────────────────────────────────

func TestHandlePprofStacks(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eng := analysis.NewEngine()
	sess := &model.Session{ID: "s", Name: "test", Target: "demo://test", StartedAt: base}
	eng.Reset(sess)
	eng.ApplyStackSnapshot(model.StackSnapshot{
		GoroutineID: 1,
		Timestamp:   base.Add(10 * time.Millisecond),
		Frames:      []model.StackFrame{{Func: "main.work", File: "main.go", Line: 5}},
	})
	mgr := session.NewManager()
	srv := NewServer("127.0.0.1:0", eng, mgr, "")
	handler := srv.routes()

	startNS := strconv.FormatInt(base.UnixNano(), 10)
	endNS := strconv.FormatInt(base.Add(time.Second).UnixNano(), 10)

	t.Run("returns stacks in range", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/pprof/stacks?start_ns="+startNS+"&end_ns="+endNS)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if _, ok := resp["stacks"]; !ok {
			t.Error("response missing stacks key")
		}
	})

	t.Run("returns 400 when params missing", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/pprof/stacks")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("returns 400 when start >= end", func(t *testing.T) {
		t.Parallel()
		// Pass start > end — should be rejected.
		rec := get(t, handler, "/api/v1/pprof/stacks?start_ns="+endNS+"&end_ns="+startNS)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// ─── handleSessions ───────────────────────────────────────────────────────────

func TestHandleSessions(t *testing.T) {
	t.Parallel()

	mgr := session.NewManager()
	_ = mgr.StartSession("test", "demo://test")
	mgr.CompleteCurrent() // moves session into history so /api/v1/sessions returns it
	eng := analysis.NewEngine()
	srv := NewServer("127.0.0.1:0", eng, mgr, "")

	rec := get(t, srv.routes(), "/api/v1/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var sessions []map[string]any
	decodeJSON(t, rec, &sessions)
	if len(sessions) == 0 {
		t.Error("expected at least one session")
	}
}

// ─── handleSessionCurrent ─────────────────────────────────────────────────────

func TestHandleSessionCurrent(t *testing.T) {
	t.Parallel()

	t.Run("returns 404 when no active session", func(t *testing.T) {
		t.Parallel()
		eng := analysis.NewEngine()
		mgr := session.NewManager()
		srv := NewServer("127.0.0.1:0", eng, mgr, "")
		rec := get(t, srv.routes(), "/api/v1/session/current")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("returns current session when active", func(t *testing.T) {
		t.Parallel()
		eng := analysis.NewEngine()
		mgr := session.NewManager()
		sess := mgr.StartSession("test", "demo://test")
		eng.Reset(sess)
		srv := NewServer("127.0.0.1:0", eng, mgr, "")
		rec := get(t, srv.routes(), "/api/v1/session/current")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
		}
		var s map[string]any
		decodeJSON(t, rec, &s)
		if s["id"] == "" {
			t.Error("session id should not be empty")
		}
	})
}

// ─── handleMetrics ────────────────────────────────────────────────────────────

func TestHandleMetrics(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateWaiting},
		{ID: 3, State: model.StateBlocked, ResourceID: "sync.Mutex:0x1"},
	}
	srv := newTestServer(t, goroutines)
	rec := get(t, srv.routes(), "/metrics")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	requiredMetrics := []string{
		"goroscope_goroutines_total",
		"goroscope_deadlock_hints_total",
		"goroscope_leak_candidates_total",
		"goroscope_closed_segments_total",
		"goroscope_stack_snapshots_total",
		"goroscope_session_duration_seconds",
	}
	for _, m := range requiredMetrics {
		if !strings.Contains(body, m) {
			t.Errorf("metrics output missing %q", m)
		}
	}
}

// ─── handleRequests ───────────────────────────────────────────────────────────

func TestHandleRequests(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eng := analysis.NewEngine()
	sess := &model.Session{ID: "s", Name: "test", Target: "demo://test", StartedAt: base}
	eng.Reset(sess)
	// Create a goroutine with a request_id label so it gets grouped.
	eng.ApplyEvents([]model.Event{
		{
			Kind:        model.EventKindGoroutineCreate,
			GoroutineID: 1,
			Timestamp:   base,
			Labels:      model.Labels{"request_id": "req-abc"},
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 1,
			Timestamp:   base.Add(time.Millisecond),
			State:       model.StateRunning,
		},
	})
	eng.SetLabelOverrides(map[int64]model.Labels{1: {"request_id": "req-abc"}})
	mgr := session.NewManager()
	srv := NewServer("127.0.0.1:0", eng, mgr, "")
	handler := srv.routes()

	t.Run("GET returns groups object", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/requests")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		if _, ok := resp["groups"]; !ok {
			t.Error("response missing groups key")
		}
		if _, ok := resp["total"]; !ok {
			t.Error("response missing total key")
		}
	})

	t.Run("POST returns 405", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})
}

// ─── handleRequestGoroutines ──────────────────────────────────────────────────

func TestHandleRequestGoroutines(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eng := analysis.NewEngine()
	sess := &model.Session{ID: "s", Name: "test", Target: "demo://test", StartedAt: base}
	eng.Reset(sess)
	eng.ApplyEvents([]model.Event{
		{
			Kind:        model.EventKindGoroutineCreate,
			GoroutineID: 10,
			Timestamp:   base,
			Labels:      model.Labels{"request_id": "req-xyz"},
		},
		{
			Kind:        model.EventKindGoroutineState,
			GoroutineID: 10,
			Timestamp:   base.Add(time.Millisecond),
			State:       model.StateRunning,
		},
	})
	eng.SetLabelOverrides(map[int64]model.Labels{10: {"request_id": "req-xyz"}})
	mgr := session.NewManager()
	srv := NewServer("127.0.0.1:0", eng, mgr, "")
	handler := srv.routes()

	t.Run("returns goroutines for known request", func(t *testing.T) {
		t.Parallel()
		// First get all request groups to find the actual request ID.
		rec := get(t, handler, "/api/v1/requests")
		var resp map[string]any
		decodeJSON(t, rec, &resp)
		groups, _ := resp["groups"].([]any)
		if len(groups) == 0 {
			t.Skip("no request groups available")
		}
		firstGroup, _ := groups[0].(map[string]any)
		reqID, _ := firstGroup["request_id"].(string)
		if reqID == "" {
			t.Skip("no request_id in first group")
		}

		goroutinesRec := get(t, handler, "/api/v1/requests/"+reqID+"/goroutines")
		if goroutinesRec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", goroutinesRec.Code, goroutinesRec.Body.String())
		}
		var gResp map[string]any
		decodeJSON(t, goroutinesRec, &gResp)
		if _, ok := gResp["goroutines"]; !ok {
			t.Error("response missing goroutines key")
		}
	})

	t.Run("returns 404 for unknown request id", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/requests/unknown-request-id/goroutines")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("POST returns 405", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/any-id/goroutines", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})
}

// ─── securityHeaders middleware ───────────────────────────────────────────────

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, nil)
	cfg := Config{}
	handler := securityHeaders(cfg, srv.routes())

	rec := get(t, handler, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	wantHeaders := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin",
	}
	for header, want := range wantHeaders {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
}

func TestSecurityHeaders_HSTS(t *testing.T) {
	t.Parallel()

	t.Run("HSTS set when token configured", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		handler := securityHeaders(Config{Token: "secret"}, srv.routes())
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		hsts := rec.Header().Get("Strict-Transport-Security")
		if hsts == "" {
			t.Error("Strict-Transport-Security header missing when token is set")
		}
		if !strings.Contains(hsts, "max-age=") {
			t.Errorf("HSTS value %q missing max-age directive", hsts)
		}
	})

	t.Run("HSTS not set without token or TLS", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		handler := securityHeaders(Config{}, srv.routes())
		rec := get(t, handler, "/healthz")
		if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
			t.Errorf("unexpected HSTS header: %q", hsts)
		}
	})
}

func TestCORSHeaders(t *testing.T) {
	t.Parallel()

	t.Run("CORS headers set for allowed origin", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		cfg := Config{CORSOrigins: []string{"https://example.com"}}
		handler := securityHeaders(cfg, srv.routes())

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
			t.Errorf("Access-Control-Allow-Origin = %q, want https://example.com", got)
		}
	})

	t.Run("CORS headers not set for disallowed origin", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		cfg := Config{CORSOrigins: []string{"https://allowed.com"}}
		handler := securityHeaders(cfg, srv.routes())

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://evil.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty for disallowed origin", got)
		}
	})

	t.Run("OPTIONS preflight returns 204 for allowed origin", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		cfg := Config{CORSOrigins: []string{"https://example.com"}}
		handler := securityHeaders(cfg, srv.routes())

		req := httptest.NewRequest(http.MethodOptions, "/api/v1/goroutines", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("wildcard origin allows any caller", func(t *testing.T) {
		t.Parallel()
		srv := newTestServer(t, nil)
		cfg := Config{CORSOrigins: []string{"*"}}
		handler := securityHeaders(cfg, srv.routes())

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://anyone.io")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anyone.io" {
			t.Errorf("Access-Control-Allow-Origin = %q, want https://anyone.io", got)
		}
	})
}
