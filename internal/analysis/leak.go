package analysis

import "github.com/Khachatur86/goroscope/internal/model"

// LeakCandidates returns goroutines that are potential leaks: in WAITING or
// BLOCKED state for longer than thresholdNS with no state change. Such
// goroutines may never complete and warrant investigation.
func LeakCandidates(goroutines []model.Goroutine, thresholdNS int64) []model.Goroutine {
	if thresholdNS <= 0 {
		return nil
	}
	var out []model.Goroutine
	for _, g := range goroutines {
		if !isLeakCandidateState(g.State) {
			continue
		}
		if g.WaitNS < thresholdNS {
			continue
		}
		out = append(out, g)
	}
	return out
}

func isLeakCandidateState(s model.GoroutineState) bool {
	switch s {
	case model.StateWaiting, model.StateBlocked:
		return true
	default:
		return false
	}
}
