package tracebridge

import (
	"context"
	"time"
)

// Config holds configuration for a trace source.
type Config struct {
	SampleStacksInterval time.Duration
	BufferMB             int
}

// Source is the interface for starting a trace collection from a target.
type Source interface {
	Start(ctx context.Context, target string) error
}

// StubSource is a no-op Source used for testing and local UI mode.
type StubSource struct{}

// Start blocks until ctx is cancelled (no-op implementation).
func (StubSource) Start(ctx context.Context, _ string) error {
	<-ctx.Done()
	return nil
}
