package agent

import (
	"os"
	"runtime/trace"
)

const traceFileEnv = "GOROSCOPE_TRACE_FILE"

// StartFromEnv enables runtime tracing when Goroscope provides a trace path.
// If the environment is not configured, it returns a no-op cleanup.
func StartFromEnv() (func() error, error) {
	tracePath := os.Getenv(traceFileEnv)
	if tracePath == "" {
		return func() error { return nil }, nil
	}

	file, err := os.Create(tracePath)
	if err != nil {
		return nil, err
	}

	if err := trace.Start(file); err != nil {
		_ = file.Close()
		return nil, err
	}

	return func() error {
		trace.Stop()
		return file.Close()
	}, nil
}
