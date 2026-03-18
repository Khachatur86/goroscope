package tracebridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Khachatur86/goroscope/internal/model"
)

// labelLine is one JSONL record from the agent labels sidecar.
type labelLine struct {
	GoroutineID int64        `json:"goroutine_id"`
	Labels      model.Labels `json:"labels"`
}

// ReadLabelsFile reads a .labels sidecar (JSONL: goroutine_id, labels) and
// returns merged label overrides per goroutine. Later entries for the same
// goroutine merge into the map (later wins per key).
func ReadLabelsFile(path string) (map[int64]model.Labels, error) {
	//nolint:gosec // path is from trace path, not user input
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open labels file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	out := make(map[int64]model.Labels)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var line labelLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.GoroutineID <= 0 || len(line.Labels) == 0 {
			continue
		}
		if out[line.GoroutineID] == nil {
			out[line.GoroutineID] = make(model.Labels, len(line.Labels))
		}
		for k, v := range line.Labels {
			out[line.GoroutineID][k] = v
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read labels file %q: %w", path, err)
	}
	return out, nil
}
