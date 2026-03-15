package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTestServer builds a Server with an Engine pre-loaded with the supplied
// goroutines. It wires up a minimal session so the current-session endpoint
// also works.
func newTestServer(t *testing.T, goroutines []model.Goroutine) *Server {
	t.Helper()

	eng := analysis.NewEngine()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	sess := &model.Session{
		ID:        "sess_test",
		Name:      "test",
		Target:    "demo://test",
		Status:    model.SessionStatusRunning,
		StartedAt: base,
	}
	eng.Reset(sess)

	// Build create + state events for each goroutine.
	events := make([]model.Event, 0, len(goroutines)*2)
	for _, g := range goroutines {
		events = append(events,
			model.Event{
				Kind:        model.EventKindGoroutineCreate,
				GoroutineID: g.ID,
				ParentID:    g.ParentID,
				Timestamp:   base,
				Labels:      g.Labels,
			},
			model.Event{
				Kind:        model.EventKindGoroutineState,
				GoroutineID: g.ID,
				Timestamp:   base.Add(time.Millisecond),
				State:       g.State,
				Reason:      g.Reason,
				ResourceID:  g.ResourceID,
			},
		)
	}
	eng.ApplyEvents(events)

	// Apply WaitNS by adding a second state event shifted in time for blocked goroutines.
	for _, g := range goroutines {
		if g.WaitNS > 0 {
			eng.ApplyEvents([]model.Event{{
				Kind:        model.EventKindGoroutineState,
				GoroutineID: g.ID,
				Timestamp:   base.Add(time.Millisecond + time.Duration(g.WaitNS)),
				State:       g.State,
				Reason:      g.Reason,
				ResourceID:  g.ResourceID,
			}})
		}
	}

	mgr := session.NewManager()
	return NewServer("127.0.0.1:0", eng, mgr, "")
}

// newTestServerWithResources is like newTestServer but also sets resource edges.
func newTestServerWithResources(t *testing.T, goroutines []model.Goroutine, edges []model.ResourceEdge) *Server {
	t.Helper()
	eng := analysis.NewEngine()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sess := &model.Session{ID: "sess_test", Name: "test", Target: "demo://test", Status: model.SessionStatusRunning, StartedAt: base}
	eng.Reset(sess)
	events := make([]model.Event, 0, len(goroutines)*2)
	for _, g := range goroutines {
		events = append(events,
			model.Event{Kind: model.EventKindGoroutineCreate, GoroutineID: g.ID, Timestamp: base},
			model.Event{Kind: model.EventKindGoroutineState, GoroutineID: g.ID, Timestamp: base.Add(time.Millisecond), State: g.State, Reason: g.Reason, ResourceID: g.ResourceID},
		)
	}
	eng.ApplyEvents(events)
	eng.SetResourceGraph(edges)
	mgr := session.NewManager()
	return NewServer("127.0.0.1:0", eng, mgr, "")
}

// decodeJSON decodes the response body into dst and fails the test on error.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, rec.Body.String())
	}
}

// get is a thin helper for GET requests against a handler.
func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ─── filterGoroutines ─────────────────────────────────────────────────────────

func TestFilterGoroutines(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateBlocked, Reason: model.ReasonChanRecv, WaitNS: int64(2 * time.Second)},
		{ID: 3, State: model.StateWaiting, Reason: model.ReasonMutexLock, WaitNS: int64(500 * time.Millisecond)},
		{ID: 4, State: model.StateSyscall, WaitNS: int64(3 * time.Second)},
		{
			ID:     5,
			State:  model.StateRunning,
			Labels: map[string]string{"function": "main.workerPool"},
			LastStack: &model.StackSnapshot{
				Frames: []model.StackFrame{
					{Func: "main.workerPool", File: "/app/worker.go", Line: 42},
				},
			},
		},
	}

	tests := []struct {
		name    string
		params  goroutineListParams
		wantIDs []int64
	}{
		{
			name:    "no filter returns all",
			params:  goroutineListParams{},
			wantIDs: []int64{1, 2, 3, 4, 5},
		},
		{
			name:    "filter by state running",
			params:  goroutineListParams{State: model.StateRunning},
			wantIDs: []int64{1, 5},
		},
		{
			name:    "filter by state blocked",
			params:  goroutineListParams{State: model.StateBlocked},
			wantIDs: []int64{2},
		},
		{
			name:    "filter by reason",
			params:  goroutineListParams{Reason: model.ReasonChanRecv},
			wantIDs: []int64{2},
		},
		{
			name:    "filter by search label",
			params:  goroutineListParams{Search: "workerpool"},
			wantIDs: []int64{5},
		},
		{
			name:    "filter by search stack func",
			params:  goroutineListParams{Search: "worker.go"},
			wantIDs: []int64{5},
		},
		{
			name:    "min_wait_ns filters non-wait states",
			params:  goroutineListParams{MinWaitNS: int64(time.Second)},
			wantIDs: []int64{2, 4},
		},
		{
			name:    "min_wait_ns threshold exact boundary",
			params:  goroutineListParams{MinWaitNS: int64(500 * time.Millisecond)},
			wantIDs: []int64{2, 3, 4},
		},
		{
			name:   "state and reason combined",
			params: goroutineListParams{State: model.StateBlocked, Reason: model.ReasonMutexLock},
			// G2 is blocked but chan_recv; G3 is waiting (not blocked) with mutex_lock
			wantIDs: nil,
		},
		{
			name:    "search with no match",
			params:  goroutineListParams{Search: "zzznomatch"},
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterGoroutines(goroutines, tt.params)

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("filterGoroutines() returned %d goroutines, want %d\ngot IDs: %v\nwant IDs: %v",
					len(got), len(tt.wantIDs), idsOf(got), tt.wantIDs)
			}
			for i, g := range got {
				if g.ID != tt.wantIDs[i] {
					t.Errorf("result[%d].ID = %d, want %d", i, g.ID, tt.wantIDs[i])
				}
			}
		})
	}
}

func idsOf(gs []model.Goroutine) []int64 {
	ids := make([]int64, len(gs))
	for i, g := range gs {
		ids[i] = g.ID
	}
	return ids
}

// ─── parseGoroutineListParams ─────────────────────────────────────────────────

func TestParseGoroutineListParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  goroutineListParams
	}{
		{
			name:  "empty query returns defaults",
			query: "",
			want:  goroutineListParams{Limit: -1},
		},
		{
			name:  "state param",
			query: "?state=BLOCKED",
			want:  goroutineListParams{State: model.StateBlocked, Limit: -1},
		},
		{
			name:  "reason param",
			query: "?reason=chan_recv",
			want:  goroutineListParams{Reason: model.ReasonChanRecv, Limit: -1},
		},
		{
			name:  "search is lowercased and trimmed",
			query: "?search=+Main.Worker+",
			want:  goroutineListParams{Search: "main.worker", Limit: -1},
		},
		{
			name:  "limit and offset",
			query: "?limit=10&offset=5",
			want:  goroutineListParams{Limit: 10, Offset: 5},
		},
		{
			name:  "negative limit is ignored",
			query: "?limit=-5",
			want:  goroutineListParams{Limit: -1},
		},
		{
			name:  "invalid limit is ignored",
			query: "?limit=abc",
			want:  goroutineListParams{Limit: -1},
		},
		{
			name:  "min_wait_ns parsed",
			query: "?min_wait_ns=1000000000",
			want:  goroutineListParams{MinWaitNS: int64(time.Second), Limit: -1},
		},
		{
			name:  "negative min_wait_ns is ignored",
			query: "?min_wait_ns=-1",
			want:  goroutineListParams{Limit: -1},
		},
		{
			name:  "all params combined",
			query: "?state=RUNNING&reason=mutex_lock&search=foo&limit=20&offset=2&min_wait_ns=500",
			want: goroutineListParams{
				State:     model.StateRunning,
				Reason:    model.ReasonMutexLock,
				Search:    "foo",
				MinWaitNS: 500,
				Limit:     20,
				Offset:    2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/goroutines"+tt.query, nil)
			got := parseGoroutineListParams(req)
			if got != tt.want {
				t.Errorf("parseGoroutineListParams() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

// ─── GET /healthz ─────────────────────────────────────────────────────────────

func TestHandleHealthz(t *testing.T) {
	t.Parallel()

	s := NewServer("127.0.0.1:0", analysis.NewEngine(), session.NewManager(), "")
	rec := get(t, s.routes(), "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	decodeJSON(t, rec, &body)

	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
	if _, ok := body["version"]; !ok {
		t.Error("version field missing from /healthz response")
	}
}

// ─── GET /api/v1/goroutines ───────────────────────────────────────────────────

func TestHandleGoroutines(t *testing.T) {
	t.Parallel()

	fixture := []model.Goroutine{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateBlocked, Reason: model.ReasonChanRecv, WaitNS: int64(2 * time.Second)},
		{ID: 3, State: model.StateWaiting, Reason: model.ReasonMutexLock, WaitNS: int64(500 * time.Millisecond)},
	}
	s := newTestServer(t, fixture)
	handler := s.routes()

	t.Run("returns all goroutines without filter", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body []map[string]any
		decodeJSON(t, rec, &body)
		if len(body) != 3 {
			t.Errorf("got %d goroutines, want 3", len(body))
		}
	})

	t.Run("filter by state=BLOCKED returns one goroutine", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines?state=BLOCKED")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body []map[string]any
		decodeJSON(t, rec, &body)
		if len(body) != 1 {
			t.Errorf("got %d goroutines, want 1", len(body))
		}
	})

	t.Run("pagination limit and offset", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines?limit=2&offset=0")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body goroutineListResponse
		decodeJSON(t, rec, &body)
		if body.Total != 3 {
			t.Errorf("total = %d, want 3", body.Total)
		}
		if len(body.Goroutines) != 2 {
			t.Errorf("returned %d goroutines, want 2", len(body.Goroutines))
		}
		if rec.Header().Get("X-Total-Count") != "3" {
			t.Errorf("X-Total-Count = %q, want 3", rec.Header().Get("X-Total-Count"))
		}
	})

	t.Run("offset beyond total returns empty slice", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines?limit=10&offset=100")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body goroutineListResponse
		decodeJSON(t, rec, &body)
		if len(body.Goroutines) != 0 {
			t.Errorf("expected empty slice, got %d goroutines", len(body.Goroutines))
		}
		if body.Total != 3 {
			t.Errorf("total = %d, want 3", body.Total)
		}
	})
}

// ─── GET /api/v1/goroutines/{id} ──────────────────────────────────────────────

func TestHandleGoroutineByID(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, []model.Goroutine{
		{ID: 42, State: model.StateRunning},
	})
	handler := s.routes()

	t.Run("existing goroutine returns 200", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/42")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		decodeJSON(t, rec, &body)
		if id, _ := body["goroutine_id"].(float64); int64(id) != 42 {
			t.Errorf("goroutine_id = %v, want 42", body["goroutine_id"])
		}
	})

	t.Run("missing goroutine returns 404", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/9999")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/notanumber")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// ─── GET /api/v1/goroutines/{id}/children ─────────────────────────────────────

func TestHandleGoroutineChildren(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, []model.Goroutine{
		{ID: 1, State: model.StateRunning},
		{ID: 2, State: model.StateRunning, ParentID: 1},
		{ID: 3, State: model.StateBlocked, Reason: model.ReasonChanRecv, ParentID: 1},
		{ID: 4, State: model.StateRunning, ParentID: 2},
	})
	handler := s.routes()

	t.Run("returns direct children only", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/1/children")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body []map[string]any
		decodeJSON(t, rec, &body)
		if len(body) != 2 {
			t.Errorf("got %d children, want 2", len(body))
		}
	})

	t.Run("goroutine with no children returns empty array", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/4/children")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body []map[string]any
		decodeJSON(t, rec, &body)
		if len(body) != 0 {
			t.Errorf("got %d children, want 0", len(body))
		}
	})

	t.Run("invalid parent id returns 400", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/goroutines/bad/children")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// ─── GET /api/v1/deadlock-hints ──────────────────────────────────────────────

func TestHandleDeadlockHints(t *testing.T) {
	t.Parallel()

	goroutines := []model.Goroutine{
		{ID: 1, State: model.StateBlocked, ResourceID: "chan:0x1"},
		{ID: 2, State: model.StateBlocked, ResourceID: "chan:0x1"},
		{ID: 3, State: model.StateBlocked, ResourceID: "mutex:0x2"},
	}
	edges := []model.ResourceEdge{
		{FromGoroutineID: 1, ToGoroutineID: 2, ResourceID: "chan:0x1"},
		{FromGoroutineID: 2, ToGoroutineID: 3, ResourceID: "mutex:0x2"},
		{FromGoroutineID: 3, ToGoroutineID: 1, ResourceID: "chan:0x3"},
	}

	s := newTestServerWithResources(t, goroutines, edges)
	handler := s.routes()

	rec := get(t, handler, "/api/v1/deadlock-hints")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Hints []struct {
			GoroutineIDs []int64  `json:"goroutine_ids"`
			ResourceIDs  []string `json:"resource_ids"`
		} `json:"hints"`
	}
	decodeJSON(t, rec, &body)
	if len(body.Hints) == 0 {
		t.Error("expected at least one deadlock hint for cycle 1-2-3")
	}
}

// ─── GET /api/v1/insights ─────────────────────────────────────────────────────

func TestHandleInsights(t *testing.T) {
	t.Parallel()

	// Build 25 blocked goroutines with varying wait times to test the top-20 cap
	// and sort order.
	fixture := make([]model.Goroutine, 25)
	for i := range fixture {
		fixture[i] = model.Goroutine{
			ID:     int64(i + 1),
			State:  model.StateBlocked,
			Reason: model.ReasonChanRecv,
			WaitNS: int64((i + 1)) * int64(time.Second), // G1=1s, G2=2s … G25=25s
		}
	}
	// Add one RUNNING goroutine that should never appear in insights.
	fixture = append(fixture, model.Goroutine{ID: 99, State: model.StateRunning})

	s := newTestServer(t, fixture)
	handler := s.routes()

	t.Run("default min_wait_ns=1s caps at 20 and sorts descending", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/insights")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body insightsResponse
		decodeJSON(t, rec, &body)

		if body.LongBlockedCount != 25 {
			t.Errorf("long_blocked_count = %d, want 25", body.LongBlockedCount)
		}
		if len(body.LongBlocked) != 20 {
			t.Errorf("len(long_blocked) = %d, want 20 (cap)", len(body.LongBlocked))
		}
		// First result must have the highest wait time (G25 = 25s).
		if body.LongBlocked[0].WaitNS <= body.LongBlocked[1].WaitNS {
			t.Errorf("results not sorted descending: first=%d, second=%d",
				body.LongBlocked[0].WaitNS, body.LongBlocked[1].WaitNS)
		}
		if body.MinWaitNS != int64(time.Second) {
			t.Errorf("min_wait_ns = %d, want %d", body.MinWaitNS, int64(time.Second))
		}
	})

	t.Run("custom min_wait_ns=10s filters correctly", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, fmt.Sprintf("/api/v1/insights?min_wait_ns=%d", 10*int64(time.Second)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body insightsResponse
		decodeJSON(t, rec, &body)

		// G10…G25 qualify (wait >= 10s) = 16 goroutines.
		if body.LongBlockedCount != 16 {
			t.Errorf("long_blocked_count = %d, want 16", body.LongBlockedCount)
		}
		if body.MinWaitNS != 10*int64(time.Second) {
			t.Errorf("min_wait_ns = %d, want %d", body.MinWaitNS, 10*int64(time.Second))
		}
	})

	t.Run("running goroutine never appears in insights", func(t *testing.T) {
		t.Parallel()
		rec := get(t, handler, "/api/v1/insights?min_wait_ns=0")
		var body insightsResponse
		decodeJSON(t, rec, &body)
		for _, g := range body.LongBlocked {
			if g.ID == 99 {
				t.Error("running goroutine (ID=99) appeared in insights; should not")
			}
		}
	})
}

// ─── pprof ────────────────────────────────────────────────────────────────────

func TestIsLocalhostAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr   string
		expect bool
	}{
		{"127.0.0.1:7070", true},
		{"[::1]:7070", true},
		{"localhost:7070", true},
		{"0.0.0.0:7070", false},
		{"192.168.1.1:7070", false},
		{"", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			got := isLocalhostAddr(tt.addr)
			if got != tt.expect {
				t.Errorf("isLocalhostAddr(%q) = %v, want %v", tt.addr, got, tt.expect)
			}
		})
	}
}

func TestPprofOnlyWhenLocalhost(t *testing.T) {
	t.Parallel()
	s := NewServer("127.0.0.1:0", nil, nil, "")
	rec := get(t, s.routes(), "/debug/pprof/")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /debug/pprof/ on localhost: got %d, want 200", rec.Code)
	}
}

func TestPprofDisabledWhenNotLocalhost(t *testing.T) {
	t.Parallel()
	s := NewServer("0.0.0.0:7070", nil, nil, "")
	rec := get(t, s.routes(), "/debug/pprof/")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /debug/pprof/ on 0.0.0.0: got %d, want 404", rec.Code)
	}
}
