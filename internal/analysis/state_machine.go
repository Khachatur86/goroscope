package analysis

import "github.com/Khachatur86/goroscope/internal/model"

type StateMachine struct{}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (s *StateMachine) Apply(current model.Goroutine, event model.Event) model.Goroutine {
	next := current
	next.LastSeenAt = event.Timestamp

	if event.State != "" {
		next.State = event.State
	}
	if event.Reason != "" {
		next.Reason = event.Reason
	}
	if event.ResourceID != "" {
		next.ResourceID = event.ResourceID
	}

	if next.State == model.StateBlocked || next.State == model.StateWaiting {
		next.WaitNS = 1
	}

	return next
}
