package tracebridge

import "github.com/Khachatur86/goroscope/internal/model"

type Normalizer struct{}

func (Normalizer) Normalize(event model.Event) model.Event {
	if event.Kind == model.EventKindGoroutineState && event.State == "" {
		event.State = model.StateWaiting
	}
	if event.Reason == "" {
		event.Reason = model.ReasonUnknown
	}

	return event
}
