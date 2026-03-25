package agent

import (
	"context"
	"runtime/pprof"
)

// OTelLabelTraceID is the goroutine label key for the OTel trace ID.
const OTelLabelTraceID = "otel.trace_id"

// OTelLabelSpanID is the goroutine label key for the OTel span ID.
const OTelLabelSpanID = "otel.span_id"

// WithOTelSpan attaches OpenTelemetry span context to the current goroutine via
// pprof labels and the goroscope label sidecar.
//
// Call this at the start of any function running inside an OTel span:
//
//	func handleRequest(ctx context.Context, span oteltrace.Span) {
//	    ctx = agent.WithOTelSpan(ctx, span.SpanContext().TraceID().String(),
//	                                   span.SpanContext().SpanID().String())
//	    // ...
//	}
//
// The traceID and spanID are hex-encoded strings as returned by the OTel SDK
// (e.g. "4bf92f3577b34da6a3ce929d0e0e4736" and "00f067aa0ba902b7").
// goroscope does not import any OTel packages — the caller bridges them.
//
// The labels propagate to child goroutines spawned with the returned ctx.
func WithOTelSpan(ctx context.Context, traceID, spanID string) context.Context {
	if traceID == "" && spanID == "" {
		return ctx
	}
	var pairs []string
	if traceID != "" {
		pairs = append(pairs, OTelLabelTraceID, traceID)
	}
	if spanID != "" {
		pairs = append(pairs, OTelLabelSpanID, spanID)
	}
	ctx = pprof.WithLabels(ctx, pprof.Labels(pairs...))
	pprof.SetGoroutineLabels(ctx)

	tracePath := traceFilePath()
	if tracePath != "" && traceID != "" {
		writeLabelToSidecar(tracePath+".labels", OTelLabelTraceID, traceID)
	}
	if tracePath != "" && spanID != "" {
		writeLabelToSidecar(tracePath+".labels", OTelLabelSpanID, spanID)
	}
	return ctx
}

// OTelSpanFromLabels extracts the OTel trace_id and span_id from a goroutine
// label map (as returned by the goroscope API).
// Returns empty strings when the labels are absent.
func OTelSpanFromLabels(labels map[string]string) (traceID, spanID string) {
	if labels == nil {
		return "", ""
	}
	return labels[OTelLabelTraceID], labels[OTelLabelSpanID]
}
