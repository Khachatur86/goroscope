package tracebridge

import "sync"

// runtime/trace is process-global; tests that start tracing must serialize it.
var runtimeTraceMu sync.Mutex
