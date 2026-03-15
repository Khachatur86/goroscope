package analysis

import "github.com/Khachatur86/goroscope/internal/model"

// BuildResourceEdges derives resource dependency edges from a sequence of events.
func BuildResourceEdges(events []model.Event) []model.ResourceEdge {
	edges := make([]model.ResourceEdge, 0, len(events))

	for _, event := range events {
		if event.Kind != model.EventKindResourceEdge || event.ResourceID == "" {
			continue
		}

		edges = append(edges, model.ResourceEdge{
			FromGoroutineID: event.GoroutineID,
			ResourceID:      event.ResourceID,
			Kind:            "unknown",
		})
	}

	return edges
}
