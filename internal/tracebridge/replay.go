package tracebridge

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Khachatur86/goroscope/internal/model"
)

//go:embed fixtures/demo.gtrace
var fixtureFS embed.FS

// LoadDemoCapture returns the bundled demo capture.
func LoadDemoCapture() (model.Capture, error) {
	data, err := fixtureFS.ReadFile("fixtures/demo.gtrace")
	if err != nil {
		return model.Capture{}, fmt.Errorf("read embedded demo capture: %w", err)
	}

	return decodeCapture(data)
}

// LoadCaptureFile reads and deserialises a capture from a .gtrace file.
func LoadCaptureFile(path string) (model.Capture, error) {
	//nolint:gosec // path validated by caller
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Capture{}, fmt.Errorf("read capture file %q: %w", path, err)
	}

	return decodeCapture(data)
}

// LoadCaptureFromBytes decodes a capture from raw JSON bytes (e.g. from an upload).
func LoadCaptureFromBytes(data []byte) (model.Capture, error) {
	return decodeCapture(data)
}

// BindCaptureSession reassigns all events in capture to the given session ID.
func BindCaptureSession(capture model.Capture, sessionID string) model.Capture {
	bound := capture

	if len(capture.Events) > 0 {
		bound.Events = make([]model.Event, len(capture.Events))
		copy(bound.Events, capture.Events)
		for idx := range bound.Events {
			bound.Events[idx].SessionID = sessionID
		}
	}

	if len(capture.Stacks) > 0 {
		bound.Stacks = make([]model.StackSnapshot, len(capture.Stacks))
		copy(bound.Stacks, capture.Stacks)
		for idx := range bound.Stacks {
			bound.Stacks[idx].SessionID = sessionID
			bound.Stacks[idx].Frames = append([]model.StackFrame(nil), capture.Stacks[idx].Frames...)
		}
	}

	if len(capture.Resources) > 0 {
		bound.Resources = make([]model.ResourceEdge, len(capture.Resources))
		copy(bound.Resources, capture.Resources)
	}

	if len(capture.ParentIDs) > 0 {
		bound.ParentIDs = make(map[int64]int64, len(capture.ParentIDs))
		for goID, parentID := range capture.ParentIDs {
			bound.ParentIDs[goID] = parentID
		}
	}

	if len(capture.LabelOverrides) > 0 {
		bound.LabelOverrides = make(map[int64]model.Labels, len(capture.LabelOverrides))
		for goID, labels := range capture.LabelOverrides {
			cp := make(model.Labels, len(labels))
			for k, v := range labels {
				cp[k] = v
			}
			bound.LabelOverrides[goID] = cp
		}
	}

	return bound
}

func decodeCapture(data []byte) (model.Capture, error) {
	var capture model.Capture
	if err := json.Unmarshal(data, &capture); err != nil {
		return model.Capture{}, fmt.Errorf("decode capture JSON: %w", err)
	}

	if capture.Events == nil {
		return model.Capture{}, fmt.Errorf("capture events is nil")
	}
	if len(capture.Events) == 0 {
		return model.Capture{}, fmt.Errorf("capture contains no events")
	}

	return capture, nil
}

// SaveCaptureFile writes a capture to a JSON file for later replay.
func SaveCaptureFile(path string, capture model.Capture) error {
	data, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write capture file %q: %w", path, err)
	}

	return nil
}
