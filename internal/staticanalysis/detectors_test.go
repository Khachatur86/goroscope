package staticanalysis

import (
	"go/parser"
	"go/token"
	"testing"
)

func analyzeSnippet(t *testing.T, src string) []Finding {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	detectors := allDetectors(nil)
	var out []Finding
	for _, d := range detectors {
		out = append(out, d.Detect(fset, file)...)
	}
	return out
}

func findingsFor(findings []Finding, rule RuleID) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// SA-1 ─────────────────────────────────────────────────────────────────────────

func TestSA1_LockWithoutDefer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "lock without defer",
			src: `package p
import "sync"
func bad() {
	var mu sync.Mutex
	mu.Lock()
	_ = mu
}`,
			wantHit: true,
		},
		{
			name: "lock with defer",
			src: `package p
import "sync"
func good() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleLockWithoutDefer)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-1 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-1 findings: %v", findings)
			}
		})
	}
}

// SA-2 ─────────────────────────────────────────────────────────────────────────

func TestSA2_LoopClosure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "captures loop var",
			src: `package p
func bad() {
	s := []int{1, 2, 3}
	for _, v := range s {
		go func() { _ = v }()
	}
}`,
			wantHit: true,
		},
		{
			name: "passes loop var as arg",
			src: `package p
func good() {
	s := []int{1, 2, 3}
	for _, v := range s {
		go func(v int) { _ = v }(v)
	}
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleLoopClosure)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-2 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-2 findings: %v", findings)
			}
		})
	}
}

// SA-3 ─────────────────────────────────────────────────────────────────────────

func TestSA3_WaitGroupAfterGo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "Add after go",
			src: `package p
import "sync"
func bad() {
	var wg sync.WaitGroup
	go func() {}()
	wg.Add(1)
	wg.Wait()
}`,
			wantHit: true,
		},
		{
			name: "Add before go",
			src: `package p
import "sync"
func good() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { wg.Done() }()
	wg.Wait()
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleWaitGroupAfterGo)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-3 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-3 findings: %v", findings)
			}
		})
	}
}

// SA-4 ─────────────────────────────────────────────────────────────────────────

func TestSA4_MutexByValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "Mutex by value param",
			src: `package p
import "sync"
func bad(mu sync.Mutex) { mu.Lock() }`,
			wantHit: true,
		},
		{
			name: "Mutex by pointer param",
			src: `package p
import "sync"
func good(mu *sync.Mutex) { mu.Lock() }`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleMutexByValue)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-4 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-4 findings: %v", findings)
			}
		})
	}
}

// SA-5 ─────────────────────────────────────────────────────────────────────────

func TestSA5_UnbufferedChanSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "unbuffered send",
			src: `package p
func bad() {
	ch := make(chan int)
	ch <- 1
}`,
			wantHit: true,
		},
		{
			name: "buffered send",
			src: `package p
func good() {
	ch := make(chan int, 1)
	ch <- 1
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleUnbufferedChanSend)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-5 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-5 findings: %v", findings)
			}
		})
	}
}

// SA-7 ─────────────────────────────────────────────────────────────────────────

func TestSA7_DoubleLock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "double lock same mutex",
			src: `package p
import "sync"
func bad() {
	var mu sync.Mutex
	mu.Lock()
	mu.Lock()
}`,
			wantHit: true,
		},
		{
			name: "lock then unlock then lock",
			src: `package p
import "sync"
func good() {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	mu.Lock()
	mu.Unlock()
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleDoubleLock)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-7 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-7 findings: %v", findings)
			}
		})
	}
}

// SA-8 ─────────────────────────────────────────────────────────────────────────

func TestSA8_SleepNoContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		wantHit bool
	}{
		{
			name: "sleep without ctx",
			src: `package p
import "time"
func bad() {
	go func() {
		time.Sleep(time.Second)
	}()
}`,
			wantHit: true,
		},
		{
			name: "sleep with ctx.Done",
			src: `package p
import (
	"context"
	"time"
)
func good(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}()
}`,
			wantHit: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := findingsFor(analyzeSnippet(t, tc.src), RuleSleepNoContext)
			if tc.wantHit && len(findings) == 0 {
				t.Error("expected SA-8 finding, got none")
			}
			if !tc.wantHit && len(findings) > 0 {
				t.Errorf("unexpected SA-8 findings: %v", findings)
			}
		})
	}
}

// Analyze integration ──────────────────────────────────────────────────────────

func TestAnalyze_EmptyDir(t *testing.T) {
	t.Parallel()
	report, err := Analyze(AnalyzeInput{Dirs: []string{t.TempDir()}})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("expected no findings in empty dir, got %d", len(report.Findings))
	}
}
