package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

func TestRun_Version(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run version: %v", err)
	}
	out := stdout.String()
	if out == "" {
		t.Error("expected version output, got empty")
	}
	if strings.Contains(out, "\n\n") {
		t.Errorf("version should be single line, got: %q", out)
	}
}

func TestRun_Check_NoHints(t *testing.T) {
	t.Parallel()

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		t.Fatalf("load demo capture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.gtrace")
	if err := tracebridge.SaveCaptureFile(path, capture); err != nil {
		t.Fatalf("save capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run(context.Background(), []string{"check", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run check (no hints expected): %v", err)
	}
	if !strings.Contains(stdout.String(), "No deadlock hints") {
		t.Errorf("expected 'No deadlock hints' in stdout, got: %s", stdout.String())
	}
}

func TestRun_Check_WithHints(t *testing.T) {
	t.Parallel()

	// Capture with resource cycle: G1->G2->G3->G1
	content := `{
  "name": "deadlock-test",
  "events": [
    {"seq": 1, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 1},
    {"seq": 2, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 1, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x1"},
    {"seq": 3, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 2},
    {"seq": 4, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 2, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x2"},
    {"seq": 5, "timestamp": "2026-01-01T00:00:00Z", "kind": "goroutine.create", "goroutine_id": 3},
    {"seq": 6, "timestamp": "2026-01-01T00:00:01Z", "kind": "goroutine.state", "goroutine_id": 3, "state": "BLOCKED", "reason": "chan_recv", "resource_id": "chan:0x3"}
  ],
  "resources": [
    {"from_goroutine_id": 1, "to_goroutine_id": 2, "resource_id": "chan:0x1"},
    {"from_goroutine_id": 2, "to_goroutine_id": 3, "resource_id": "chan:0x2"},
    {"from_goroutine_id": 3, "to_goroutine_id": 1, "resource_id": "chan:0x3"}
  ]
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "deadlock.gtrace")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"check", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected check to fail with deadlock hints, got nil")
	}
	if !strings.Contains(err.Error(), "deadlock hints") {
		t.Errorf("expected 'deadlock hints' in error, got: %v", err)
	}
}

func TestRun_Check_MissingFile(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"check", "/nonexistent/path.gtrace"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRun_Replay_RawTrace(t *testing.T) {
	modRoot, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Skipf("cannot get module root: %v", err)
	}
	root := strings.TrimSpace(string(modRoot))
	if root == "" {
		t.Skip("module root is empty")
	}

	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.out")

	cmd := exec.Command("go", "test", "-trace="+tracePath, "-count=1", "./testdata/tracepkg")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go test -trace failed: %v\n%s", err, out)
	}

	var stdout, stderr bytes.Buffer
	// Use export (not replay) — same LoadCaptureFromPath path, and replay would block on HTTP server.
	err = Run(context.Background(), []string{"export", "--format=csv", tracePath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("goroscope export trace.out: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "goroutine_id") {
		t.Errorf("expected CSV header in output, got: %s", stdout.String())
	}
}

func TestRun_Export_CSV(t *testing.T) {
	t.Parallel()

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		t.Fatalf("load demo capture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.gtrace")
	if err := tracebridge.SaveCaptureFile(path, capture); err != nil {
		t.Fatalf("save capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run(context.Background(), []string{"export", "--format=csv", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run export csv: %v", err)
	}

	r := csv.NewReader(strings.NewReader(stdout.String()))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header + at least 1 row, got %d rows", len(rows))
	}
	header := rows[0]
	wantCols := []string{"goroutine_id", "state", "start_ns", "end_ns", "reason", "resource_id"}
	for _, w := range wantCols {
		found := false
		for _, h := range header {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected column %q in header %v", w, header)
		}
	}
}

// onMatchWriter forwards writes to buf and calls fn (at most once) when
// the written bytes contain pattern. Used to cancel a context as soon as
// the HTTP server announces it is ready.
type onMatchWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	pattern string
	fn      func()
	once    sync.Once
}

func (w *onMatchWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()
	if strings.Contains(string(p), w.pattern) {
		w.once.Do(w.fn)
	}
	return n, err
}

func (w *onMatchWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestRun_Test_Basic verifies that "goroscope test" runs go test with tracing,
// loads the resulting trace, and starts the HTTP server. The context is
// cancelled as soon as the server is ready so the test does not block.
func TestRun_Test_Basic(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH")
	}

	// Resolve the module root so that the relative package path ./testdata/tracepkg works.
	modRootBytes, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Skipf("cannot determine module root: %v", err)
	}
	modRoot := strings.TrimSpace(string(modRootBytes))
	if modRoot == "" {
		t.Skip("module root is empty")
	}

	// os.Chdir affects the whole process; do not run in parallel.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(modRoot); err != nil {
		t.Fatalf("chdir to module root %q: %v", modRoot, err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &onMatchWriter{pattern: "serving", fn: cancel}
	var stderr bytes.Buffer

	err = Run(ctx, []string{
		"test",
		"-addr=127.0.0.1:17070",
		"./testdata/tracepkg",
		"-count=1",
	}, out, &stderr)

	// context.Canceled is expected: we cancel as soon as the server is ready.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), "loading trace") {
		t.Errorf("expected 'loading trace' in output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "serving") {
		t.Errorf("expected 'serving' in output, got:\n%s", out.String())
	}
}

// TestRun_Test_Help verifies the -help flag prints usage without error.
func TestRun_Test_Help(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"test", "-help"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected nil error for -help, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "go test") {
		t.Errorf("expected usage mentioning 'go test' in stderr, got:\n%s", stderr.String())
	}
}

// TestRun_Test_UnknownPackage verifies that "goroscope test" propagates go test
// failures when the package does not exist.
func TestRun_Test_UnknownPackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH")
	}

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"test",
		"./nonexistent/pkg/that/does/not/exist",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for nonexistent package, got nil")
	}
}

// TestExtractRunFilter verifies that extractRunFilter correctly parses -run
// values from go test argument slices.
func TestExtractRunFilter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty", args: nil, want: ""},
		{name: "no run flag", args: []string{"./pkg/...", "-count=1"}, want: ""},
		{name: "dash run equals", args: []string{"-run=TestWorkerPool", "./..."}, want: "TestWorkerPool"},
		{name: "dash dash run equals", args: []string{"--run=TestWorkerPool"}, want: "TestWorkerPool"},
		{name: "dash run space", args: []string{"-run", "TestWorkerPool", "./..."}, want: "TestWorkerPool"},
		{name: "dash dash run space", args: []string{"--run", "TestWorkerPool"}, want: "TestWorkerPool"},
		{name: "run flag last no value", args: []string{"./pkg", "-run"}, want: ""},
		{name: "regex run value", args: []string{"-run=TestWorker|TestPool"}, want: "TestWorker|TestPool"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractRunFilter(tc.args)
			if got != tc.want {
				t.Errorf("extractRunFilter(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestRun_Test_FilterFlag verifies that "goroscope test --filter=TestFoo"
// runs go test and logs the expected messages. The context is cancelled as
// soon as the server starts so the test does not block.
func TestRun_Test_FilterFlag(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH")
	}

	modRootBytes, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Skipf("cannot determine module root: %v", err)
	}
	modRoot := strings.TrimSpace(string(modRootBytes))
	if modRoot == "" {
		t.Skip("module root is empty")
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(modRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &onMatchWriter{pattern: "serving", fn: cancel}
	var stderr bytes.Buffer

	err = Run(ctx, []string{
		"test",
		"-addr=127.0.0.1:17071",
		"-filter=TestDummy",
		"./testdata/tracepkg",
		"-run=TestDummy",
	}, out, &stderr)

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v\nstdout: %s\nstderr: %s", err, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), "loading trace") {
		t.Errorf("expected 'loading trace' in output, got:\n%s", out.String())
	}
}

func TestRun_Export_JSON(t *testing.T) {
	t.Parallel()

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		t.Fatalf("load demo capture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.gtrace")
	if err := tracebridge.SaveCaptureFile(path, capture); err != nil {
		t.Fatalf("save capture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = Run(context.Background(), []string{"export", "--format=json", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run export json: %v", err)
	}

	var body struct {
		Segments []map[string]any `json:"segments"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body.Segments == nil {
		t.Error("expected segments array, got nil")
	}
	if len(body.Segments) > 0 {
		seg := body.Segments[0]
		for _, key := range []string{"goroutine_id", "state", "start_ns", "end_ns"} {
			if _, ok := seg[key]; !ok {
				t.Errorf("expected segment to have %q, got %v", key, seg)
			}
		}
	}
}
