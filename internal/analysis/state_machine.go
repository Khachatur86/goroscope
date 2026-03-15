package analysis

import (
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// StateMachine applies goroutine events to produce updated goroutine state.
type StateMachine struct{}

// NewStateMachine returns a new StateMachine.
func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

// Apply returns the next goroutine state after applying the given event.
func (s *StateMachine) Apply(current model.Goroutine, event model.Event) model.Goroutine {
	next := current

	if next.ID == 0 && event.GoroutineID != 0 {
		next.ID = event.GoroutineID
	}
	if next.CreatedAt.IsZero() && !event.Timestamp.IsZero() {
		next.CreatedAt = event.Timestamp
	}
	if !event.Timestamp.IsZero() {
		next.LastSeenAt = event.Timestamp
	}
	if next.Labels == nil && len(event.Labels) > 0 {
		next.Labels = make(map[string]string, len(event.Labels))
	}
	for key, value := range event.Labels {
		next.Labels[key] = value
	}

	nextState := resolveNextState(current, event)
	continuingWait := isWaitState(current.State) &&
		isWaitState(nextState) &&
		current.State == nextState &&
		reasonMatches(current.Reason, event.Reason) &&
		resourceMatches(current.ResourceID, event.ResourceID)

	if continuingWait {
		next.WaitNS = current.WaitNS + elapsedNS(current.LastSeenAt, event.Timestamp)
		if event.Reason != "" {
			next.Reason = event.Reason
		}
		if event.ResourceID != "" {
			next.ResourceID = event.ResourceID
		}
		next.State = nextState
		return next
	}

	next.State = nextState
	if isWaitState(nextState) {
		next.WaitNS = 0
		if event.Reason != "" {
			next.Reason = event.Reason
		} else {
			next.Reason = model.ReasonUnknown
		}
		next.ResourceID = event.ResourceID
		return next
	}

	next.WaitNS = 0
	next.Reason = ""
	next.ResourceID = ""
	return next
}

func resolveNextState(current model.Goroutine, event model.Event) model.GoroutineState {
	switch event.Kind {
	case model.EventKindGoroutineCreate:
		if event.State != "" {
			return event.State
		}
		return model.StateRunnable
	case model.EventKindGoroutineStart:
		if event.State != "" {
			return event.State
		}
		return model.StateRunning
	case model.EventKindGoroutineEnd:
		return model.StateDone
	case model.EventKindGoroutineState:
		if event.State != "" {
			return event.State
		}
		return current.State
	default:
		if event.State != "" {
			return event.State
		}
		return current.State
	}
}

func isWaitState(state model.GoroutineState) bool {
	switch state {
	case model.StateWaiting, model.StateBlocked, model.StateSyscall:
		return true
	default:
		return false
	}
}

func reasonMatches(current, incoming model.BlockingReason) bool {
	return incoming == "" || incoming == current
}

func resourceMatches(current, incoming string) bool {
	return incoming == "" || incoming == current
}

func elapsedNS(from, to time.Time) int64 {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return 0
	}

	return to.Sub(from).Nanoseconds()
}
