package agent

import (
	"context"
	"runtime/pprof"
	"testing"
)

func TestWithOTelSpan_SetsLabels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const (
		wantTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
		wantSpan  = "00f067aa0ba902b7"
	)
	ctx = WithOTelSpan(ctx, wantTrace, wantSpan)

	var gotTrace, gotSpan string
	pprof.ForLabels(ctx, func(k, v string) bool {
		switch k {
		case OTelLabelTraceID:
			gotTrace = v
		case OTelLabelSpanID:
			gotSpan = v
		}
		return true
	})

	if gotTrace != wantTrace {
		t.Errorf("trace_id label = %q, want %q", gotTrace, wantTrace)
	}
	if gotSpan != wantSpan {
		t.Errorf("span_id label = %q, want %q", gotSpan, wantSpan)
	}
}

func TestWithOTelSpan_NoopOnEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got := WithOTelSpan(ctx, "", "")
	if got != ctx {
		t.Error("expected same context when both IDs are empty")
	}
}

func TestWithOTelSpan_TraceIDOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = WithOTelSpan(ctx, "abc123", "")

	var gotTrace string
	pprof.ForLabels(ctx, func(k, v string) bool {
		if k == OTelLabelTraceID {
			gotTrace = v
		}
		return true
	})
	if gotTrace != "abc123" {
		t.Errorf("trace_id = %q, want abc123", gotTrace)
	}
}

func TestOTelSpanFromLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		labels    map[string]string
		wantTrace string
		wantSpan  string
	}{
		{
			name:      "nil labels",
			labels:    nil,
			wantTrace: "",
			wantSpan:  "",
		},
		{
			name:      "empty labels",
			labels:    map[string]string{},
			wantTrace: "",
			wantSpan:  "",
		},
		{
			name:      "both present",
			labels:    map[string]string{OTelLabelTraceID: "trace1", OTelLabelSpanID: "span1"},
			wantTrace: "trace1",
			wantSpan:  "span1",
		},
		{
			name:      "trace only",
			labels:    map[string]string{OTelLabelTraceID: "trace1"},
			wantTrace: "trace1",
			wantSpan:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTrace, gotSpan := OTelSpanFromLabels(tt.labels)
			if gotTrace != tt.wantTrace {
				t.Errorf("traceID = %q, want %q", gotTrace, tt.wantTrace)
			}
			if gotSpan != tt.wantSpan {
				t.Errorf("spanID = %q, want %q", gotSpan, tt.wantSpan)
			}
		})
	}
}
