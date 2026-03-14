package collector

import (
	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
)

type Collector struct {
	buffer *Buffer
	engine *analysis.Engine
}

func New(engine *analysis.Engine, maxEvents int) *Collector {
	return &Collector{
		buffer: NewBuffer(maxEvents),
		engine: engine,
	}
}

func (c *Collector) Ingest(event model.Event) {
	c.buffer.Add(event)
}

func (c *Collector) Snapshot() []model.Event {
	return c.buffer.List()
}
