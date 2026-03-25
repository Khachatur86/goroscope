package otlp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

func makeGoroutine(id, parentID int64, state model.GoroutineState, waitNS int64) model.Goroutine {
	base := time.Unix(0, 1_000_000_000)
	return model.Goroutine{
		ID:         id,
		ParentID:   parentID,
		State:      state,
		WaitNS:     waitNS,
		CreatedAt:  base,
		LastSeenAt: base.Add(time.Duration(waitNS) + time.Second),
	}
}

func makeSegment(goroutineID int64, state model.GoroutineState, startNS, endNS int64) model.TimelineSegment {
	return model.TimelineSegment{
		GoroutineID: goroutineID,
		State:       state,
		StartNS:     startNS,
		EndNS:       endNS,
	}
}

func TestBuildPayload_ValidJSON(t *testing.T) {
	t.Parallel()
	in := ExportInput{
		Target: "test-target",
		Goroutines: []model.Goroutine{
			makeGoroutine(1, 0, model.StateRunning, 0),
			makeGoroutine(2, 1, model.StateBlocked, 2_000_000_000),
		},
		Segments: []model.TimelineSegment{
			makeSegment(1, model.StateRunning, 1_000_000_000, 2_000_000_000),
			makeSegment(2, model.StateBlocked, 1_500_000_000, 3_500_000_000),
		},
	}

	payload, err := BuildPayload(in)
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}

	var out otlpExportRequest
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(out.ResourceSpans) != 1 {
		t.Fatalf("expected 1 resourceSpan, got %d", len(out.ResourceSpans))
	}
	rs := out.ResourceSpans[0]
	if len(rs.ScopeSpans) != 1 {
		t.Fatalf("expected 1 scopeSpan, got %d", len(rs.ScopeSpans))
	}
	spans := rs.ScopeSpans[0].Spans
	// 2 root spans + 2 segment spans = 4
	if len(spans) != 4 {
		t.Errorf("expected 4 spans, got %d", len(spans))
	}
}

func TestBuildPayload_TraceIDDeterministic(t *testing.T) {
	t.Parallel()
	in := ExportInput{
		Target:     "my-app",
		Goroutines: []model.Goroutine{makeGoroutine(1, 0, model.StateRunning, 0)},
	}
	p1, _ := BuildPayload(in)
	p2, _ := BuildPayload(in)
	if string(p1) != string(p2) {
		t.Error("BuildPayload should be deterministic for same input")
	}
}

func TestBuildPayload_ParentChildSpanIDs(t *testing.T) {
	t.Parallel()
	in := ExportInput{
		Target: "t",
		Goroutines: []model.Goroutine{
			makeGoroutine(1, 0, model.StateRunning, 0),
			makeGoroutine(42, 1, model.StateBlocked, 0),
		},
	}
	payload, _ := BuildPayload(in)
	var out otlpExportRequest
	_ = json.Unmarshal(payload, &out)

	spans := out.ResourceSpans[0].ScopeSpans[0].Spans
	byName := make(map[string]otlpSpan, len(spans))
	for _, s := range spans {
		byName[s.Name] = s
	}

	g1 := byName["G1"]
	g42 := byName["G42"]

	if g1.ParentSpanID != "" {
		t.Errorf("G1 root span should have no parentSpanId, got %q", g1.ParentSpanID)
	}
	if g42.ParentSpanID != g1.SpanID {
		t.Errorf("G42 parentSpanId should equal G1 spanId: got %q want %q", g42.ParentSpanID, g1.SpanID)
	}
}

func TestBuildPayload_SpanIDLength(t *testing.T) {
	t.Parallel()
	in := ExportInput{
		Target:     "t",
		Goroutines: []model.Goroutine{makeGoroutine(99, 0, model.StateRunning, 0)},
		Segments:   []model.TimelineSegment{makeSegment(99, model.StateRunning, 1e9, 2e9)},
	}
	payload, _ := BuildPayload(in)
	var out otlpExportRequest
	_ = json.Unmarshal(payload, &out)

	for _, span := range out.ResourceSpans[0].ScopeSpans[0].Spans {
		if len(span.TraceID) != 32 {
			t.Errorf("traceId should be 32 hex chars, got %d in %q", len(span.TraceID), span.TraceID)
		}
		if len(span.SpanID) != 16 {
			t.Errorf("spanId should be 16 hex chars, got %d in %q", len(span.SpanID), span.SpanID)
		}
	}
}

func TestBuildPayload_BlockedSegmentIsError(t *testing.T) {
	t.Parallel()
	in := ExportInput{
		Target:     "t",
		Goroutines: []model.Goroutine{makeGoroutine(1, 0, model.StateBlocked, 5e9)},
		Segments:   []model.TimelineSegment{makeSegment(1, model.StateBlocked, 1e9, 6e9)},
	}
	payload, _ := BuildPayload(in)
	var out otlpExportRequest
	_ = json.Unmarshal(payload, &out)

	for _, span := range out.ResourceSpans[0].ScopeSpans[0].Spans {
		if span.Name == "G1 BLOCKED" && span.Status.Code != 2 {
			t.Errorf("BLOCKED segment span should have status code 2 (error), got %d", span.Status.Code)
		}
	}
}

func TestNormaliseEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"localhost:4318", "http://localhost:4318/v1/traces"},
		{"localhost:4317", "http://localhost:4317/v1/traces"},
		{"http://my-collector:4318/v1/traces", "http://my-collector:4318/v1/traces"},
		{"https://otel.example.com/v1/traces", "https://otel.example.com/v1/traces"},
	}
	for _, tc := range tests {
		if got := normaliseEndpoint(tc.in); got != tc.want {
			t.Errorf("normaliseEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSend_Success(t *testing.T) {
	t.Parallel()
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := []byte(`{"resourceSpans":[]}`)
	if err := Send(t.Context(), SendInput{Endpoint: srv.URL, Payload: payload}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(received) != string(payload) {
		t.Errorf("server received wrong payload: %s", received)
	}
}

func TestSend_NonOKStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	err := Send(t.Context(), SendInput{Endpoint: srv.URL, Payload: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention 400, got: %v", err)
	}
}
