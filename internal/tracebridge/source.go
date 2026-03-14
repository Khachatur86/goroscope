package tracebridge

import (
	"context"
	"time"
)

type Config struct {
	SampleStacksInterval time.Duration
	BufferMB             int
}

type Source interface {
	Start(ctx context.Context, target string) error
}

type StubSource struct{}

func (StubSource) Start(ctx context.Context, target string) error {
	<-ctx.Done()
	return nil
}
