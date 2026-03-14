package tracebridge

import (
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

type RawEvent struct {
	Name        string
	At          time.Time
	GoroutineID int64
	Metadata    map[string]string
}

type Decoder struct{}

func (Decoder) Decode(raw RawEvent) model.Event {
	return model.Event{
		Timestamp:   raw.At,
		GoroutineID: raw.GoroutineID,
		Kind:        model.EventKindGoroutineState,
		Labels:      raw.Metadata,
	}
}
