// Package otlp converts goroscope goroutine timelines to OpenTelemetry spans
// and ships them via OTLP/HTTP+JSON (no external dependencies).
//
// Mapping:
//   - One OTel trace per capture (shared traceId).
//   - One "root" span per goroutine that covers its full lifetime.
//   - One child span per timeline segment (RUNNING, WAITING, BLOCKED, …).
//   - Parent-child span hierarchy mirrors the goroutine spawn tree.
package otlp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// ExportInput contains everything needed to build an OTLP payload.
type ExportInput struct {
	// Target is the capture source (binary path, URL, or file name).
	Target string
	// Goroutines is the full goroutine list from the engine.
	Goroutines []model.Goroutine
	// Segments is the full timeline from the engine.
	Segments []model.TimelineSegment
	// InstrumentationScope is the name embedded in the OTel scope.
	// Defaults to "goroscope" when empty.
	InstrumentationScope string
	// InstrumentationVersion is the scope version. Defaults to "dev".
	InstrumentationVersion string
}

// SendInput holds all parameters for Send (CS-5: input struct for >2 args).
type SendInput struct {
	// Endpoint is the OTLP/HTTP+JSON URL, e.g. "http://localhost:4318/v1/traces".
	// A bare "host:port" is expanded to "http://host:port/v1/traces".
	Endpoint string
	// Payload is the JSON bytes produced by BuildPayload.
	Payload []byte
	// Timeout for the HTTP request; 0 uses 30 s.
	Timeout time.Duration
}

// ── OTLP JSON structs ─────────────────────────────────────────────────────────

type otlpExportRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKV `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// otlpSpan mirrors the OTLP protobuf Span message.
// Int64 nano-times are serialised as JSON strings per the OTLP JSON spec.
type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpKV   `json:"attributes,omitempty"`
	Status            otlpStatus `json:"status"`
}

type otlpKV struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	// Only one field will be set; the others will be omitted.
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"` // int64 as string per OTLP JSON spec
}

type otlpStatus struct {
	// Code: 0=unset, 1=ok, 2=error
	Code int `json:"code"`
}

// ── Span kind constants (OTLP spec) ──────────────────────────────────────────

const (
	spanKindInternal = 1
)

// ── Public API ────────────────────────────────────────────────────────────────

// BuildPayload converts a goroscope capture to an OTLP/JSON byte slice.
func BuildPayload(in ExportInput) ([]byte, error) {
	scope := in.InstrumentationScope
	if scope == "" {
		scope = "goroscope"
	}
	ver := in.InstrumentationVersion
	if ver == "" {
		ver = "dev"
	}

	traceID := captureTraceID(in.Target)

	// Index goroutines by ID for fast parent lookup.
	byID := make(map[int64]model.Goroutine, len(in.Goroutines))
	for _, g := range in.Goroutines {
		byID[g.ID] = g
	}

	// Index segments by goroutine ID.
	segsByG := make(map[int64][]model.TimelineSegment, len(in.Goroutines))
	for _, s := range in.Segments {
		segsByG[s.GoroutineID] = append(segsByG[s.GoroutineID], s)
	}

	var spans []otlpSpan

	for _, g := range in.Goroutines {
		rootSpanID := goroutineSpanID(g.ID)
		parentSpanID := ""
		if g.ParentID != 0 {
			if _, ok := byID[g.ParentID]; ok {
				parentSpanID = goroutineSpanID(g.ParentID)
			}
		}

		startNS := g.CreatedAt.UnixNano()
		endNS := g.LastSeenAt.UnixNano()
		if startNS <= 0 {
			startNS = endNS
		}
		if endNS <= startNS {
			endNS = startNS + 1
		}

		// Root span — covers full goroutine lifetime.
		rootSpan := otlpSpan{
			TraceID:           traceID,
			SpanID:            rootSpanID,
			ParentSpanID:      parentSpanID,
			Name:              fmt.Sprintf("G%d", g.ID),
			Kind:              spanKindInternal,
			StartTimeUnixNano: fmt.Sprintf("%d", startNS),
			EndTimeUnixNano:   fmt.Sprintf("%d", endNS),
			Status:            otlpStatus{Code: 1},
			Attributes:        goroutineAttributes(g),
		}
		spans = append(spans, rootSpan)

		// Child spans — one per timeline segment.
		for i, seg := range segsByG[g.ID] {
			segEnd := seg.EndNS
			if segEnd <= seg.StartNS {
				segEnd = seg.StartNS + 1
			}
			childSpan := otlpSpan{
				TraceID:           traceID,
				SpanID:            segmentSpanID(g.ID, seg.StartNS, i),
				ParentSpanID:      rootSpanID,
				Name:              fmt.Sprintf("G%d %s", g.ID, seg.State),
				Kind:              spanKindInternal,
				StartTimeUnixNano: fmt.Sprintf("%d", seg.StartNS),
				EndTimeUnixNano:   fmt.Sprintf("%d", segEnd),
				Status:            segmentStatus(seg.State),
				Attributes:        segmentAttributes(seg),
			}
			spans = append(spans, childSpan)
		}
	}

	req := otlpExportRequest{
		ResourceSpans: []otlpResourceSpans{
			{
				Resource: otlpResource{
					Attributes: []otlpKV{
						strKV("service.name", "goroscope"),
						strKV("goroscope.target", in.Target),
					},
				},
				ScopeSpans: []otlpScopeSpans{
					{
						Scope: otlpScope{Name: scope, Version: ver},
						Spans: spans,
					},
				},
			},
		},
	}

	return json.Marshal(req)
}

// Send POST the payload to the OTLP/HTTP+JSON endpoint.
// A bare "host:port" endpoint is expanded to "http://host:port/v1/traces".
func Send(ctx context.Context, in SendInput) error {
	endpoint := normaliseEndpoint(in.Endpoint)
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(in.Payload))
	if err != nil {
		return fmt.Errorf("build OTLP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send OTLP to %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("OTLP collector returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// captureTraceID derives a deterministic 128-bit trace ID from the capture target.
func captureTraceID(target string) string {
	sum := sha256.Sum256([]byte(target))
	return hex.EncodeToString(sum[:16])
}

// goroutineSpanID encodes a goroutine ID as a deterministic 64-bit span ID.
func goroutineSpanID(id int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(id)) //nolint:gosec // G115: id is a non-negative goroutine ID
	return hex.EncodeToString(buf[:])
}

// segmentSpanID produces a unique span ID for one segment of a goroutine.
// It XORs the goroutine ID with (startNS ^ index) to avoid collisions.
func segmentSpanID(goroutineID, startNS int64, idx int) string {
	var buf [8]byte
	v := uint64(goroutineID)<<32 ^ uint64(startNS) ^ uint64(idx) //nolint:gosec
	binary.BigEndian.PutUint64(buf[:], v)
	return hex.EncodeToString(buf[:])
}

// normaliseEndpoint turns a bare "host:port" into an OTLP/HTTP URL.
func normaliseEndpoint(s string) string {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	return "http://" + s + "/v1/traces"
}

func goroutineAttributes(g model.Goroutine) []otlpKV {
	kvs := []otlpKV{
		intKV("goroutine.id", g.ID),
		strKV("goroutine.state", string(g.State)),
	}
	if g.ParentID != 0 {
		kvs = append(kvs, intKV("goroutine.parent_id", g.ParentID))
	}
	if g.WaitNS > 0 {
		kvs = append(kvs, intKV("goroutine.wait_ns", g.WaitNS))
	}
	for k, v := range g.Labels {
		v := v
		kvs = append(kvs, strKV("goroutine.label."+k, v))
	}
	return kvs
}

func segmentAttributes(s model.TimelineSegment) []otlpKV {
	kvs := []otlpKV{
		intKV("goroutine.id", s.GoroutineID),
		strKV("goroutine.state", string(s.State)),
	}
	if s.Reason != "" {
		kvs = append(kvs, strKV("goroutine.reason", string(s.Reason)))
	}
	if s.ResourceID != "" {
		kvs = append(kvs, strKV("goroutine.resource_id", s.ResourceID))
	}
	durationNS := s.EndNS - s.StartNS
	if durationNS > 0 {
		kvs = append(kvs, intKV("goroutine.duration_ns", durationNS))
	}
	return kvs
}

// segmentStatus maps a goroutine state to an OTLP status code.
// BLOCKED/WAITING are marked as Error (2) to make anomalies visible in trace UIs.
func segmentStatus(state model.GoroutineState) otlpStatus {
	switch state {
	case model.StateBlocked, model.StateWaiting:
		return otlpStatus{Code: 2} // Error
	default:
		return otlpStatus{Code: 1} // Ok
	}
}

func strKV(key, value string) otlpKV {
	v := value
	return otlpKV{Key: key, Value: otlpValue{StringValue: &v}}
}

func intKV(key string, value int64) otlpKV {
	v := fmt.Sprintf("%d", value)
	return otlpKV{Key: key, Value: otlpValue{IntValue: &v}}
}
