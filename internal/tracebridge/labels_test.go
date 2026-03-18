package tracebridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLabelsFile_NotExist(t *testing.T) {
	t.Parallel()
	out, err := ReadLabelsFile("/nonexistent/path.labels")
	if err != nil {
		t.Fatalf("ReadLabelsFile(not exist) = %v, want nil", err)
	}
	if out != nil {
		t.Errorf("ReadLabelsFile(not exist) = %v, want nil", out)
	}
}

func TestReadLabelsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.out.labels")
	content := `{"goroutine_id": 5, "labels": {"request_id": "req-abc"}}
{"goroutine_id": 7, "labels": {"request_id": "req-xyz"}}
{"goroutine_id": 5, "labels": {"trace_id": "trace-123"}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write labels file: %v", err)
	}
	out, err := ReadLabelsFile(path)
	if err != nil {
		t.Fatalf("ReadLabelsFile: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (goroutines 5 and 7)", len(out))
	}
	if got := out[5]["request_id"]; got != "req-abc" {
		t.Errorf("out[5][request_id] = %q, want req-abc", got)
	}
	if got := out[5]["trace_id"]; got != "trace-123" {
		t.Errorf("out[5][trace_id] = %q, want trace-123 (merged from second line)", got)
	}
	if got := out[7]["request_id"]; got != "req-xyz" {
		t.Errorf("out[7][request_id] = %q, want req-xyz", got)
	}
}
