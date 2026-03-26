package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

type diffInput struct {
	baselinePath string
	comparePath  string
	format       string
	thresholdPct float64
}

func diffCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope diff [flags] <baseline.gtrace> <compare.gtrace>\n\n")
		_, _ = fmt.Fprintf(stderr, "Compare two .gtrace captures and report goroutine state changes.\n")
		_, _ = fmt.Fprintf(stderr, "Exits with code 1 when regression percentage exceeds --threshold.\n\n")
		fs.PrintDefaults()
	}
	format := fs.String("format", "text", "Output format: text or json")
	threshold := fs.Float64("threshold", 0, "Exit 1 if regression % of goroutines exceeds this value (0 = disabled)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() < 2 {
		fs.Usage()
		return fmt.Errorf("two capture files required")
	}

	return runDiff(ctx, diffInput{
		baselinePath: fs.Arg(0),
		comparePath:  fs.Arg(1),
		format:       *format,
		thresholdPct: *threshold,
	}, stdout, stderr)
}

func runDiff(ctx context.Context, in diffInput, stdout, stderr io.Writer) error {
	baselineGoroutines, baselineSegments, err := loadCaptureData(ctx, in.baselinePath)
	if err != nil {
		return fmt.Errorf("load baseline %q: %w", in.baselinePath, err)
	}
	compareGoroutines, compareSegments, err := loadCaptureData(ctx, in.comparePath)
	if err != nil {
		return fmt.Errorf("load compare %q: %w", in.comparePath, err)
	}

	diff := analysis.ComputeCaptureDiff(baselineGoroutines, baselineSegments, compareGoroutines, compareSegments)

	switch in.format {
	case "json":
		return outputDiffJSON(stdout, diff, in.baselinePath, in.comparePath)
	default:
		return outputDiffText(stdout, stderr, diff, in.baselinePath, in.comparePath, in.thresholdPct)
	}
}

func outputDiffJSON(w io.Writer, diff analysis.CaptureDiff, baseline, compare string) error {
	out := struct {
		Baseline string              `json:"baseline"`
		Compare  string              `json:"compare"`
		Diff     analysis.CaptureDiff `json:"diff"`
	}{
		Baseline: baseline,
		Compare:  compare,
		Diff:     diff,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func outputDiffText(w, stderr io.Writer, diff analysis.CaptureDiff, baseline, compare string, thresholdPct float64) error {
	_, _ = fmt.Fprintf(w, "=== goroscope diff ===\n")
	_, _ = fmt.Fprintf(w, "  baseline : %s\n", baseline)
	_, _ = fmt.Fprintf(w, "  compare  : %s\n\n", compare)

	improved, regressed, unchanged := 0, 0, 0
	for _, d := range diff.GoroutineDeltas {
		switch d.Status {
		case "improved":
			improved++
		case "regressed":
			regressed++
		default:
			unchanged++
		}
	}
	total := len(diff.GoroutineDeltas)

	_, _ = fmt.Fprintf(w, "Goroutines present in both captures: %d\n", total)
	_, _ = fmt.Fprintf(w, "  improved  : %d\n", improved)
	_, _ = fmt.Fprintf(w, "  regressed : %d\n", regressed)
	_, _ = fmt.Fprintf(w, "  unchanged : %d\n", unchanged)
	_, _ = fmt.Fprintf(w, "\nOnly in baseline : %d goroutine(s)\n", len(diff.OnlyInBaseline))
	_, _ = fmt.Fprintf(w, "Only in compare  : %d goroutine(s)\n\n", len(diff.OnlyInCompare))

	// Print top-10 regressions sorted by wait delta descending.
	type entry struct {
		id    int64
		delta analysis.GoroutineDelta
	}
	var regressions []entry
	for id, d := range diff.GoroutineDeltas {
		if d.Status == "regressed" {
			regressions = append(regressions, entry{id, d})
		}
	}
	sort.Slice(regressions, func(i, j int) bool {
		return regressions[i].delta.WaitDeltaNS > regressions[j].delta.WaitDeltaNS
	})
	if len(regressions) > 0 {
		_, _ = fmt.Fprintf(w, "Top regressions (by wait increase):\n")
		limit := 10
		if len(regressions) < limit {
			limit = len(regressions)
		}
		for _, e := range regressions[:limit] {
			_, _ = fmt.Fprintf(w, "  G%-8d  wait +%s  blocked +%s\n",
				e.id,
				fmtNS(e.delta.WaitDeltaNS),
				fmtNS(e.delta.BlockedDeltaNS),
			)
		}
	}

	// Threshold gate.
	if thresholdPct > 0 && total > 0 {
		pct := float64(regressed) / float64(total) * 100
		if pct > thresholdPct {
			_, _ = fmt.Fprintf(stderr, "\nFAIL: %.1f%% goroutines regressed (threshold %.1f%%)\n", pct, thresholdPct)
			return &exitError{code: 1}
		}
	}
	return nil
}

func fmtNS(ns int64) string {
	if ns < 0 {
		return "-" + fmtNS(-ns)
	}
	if ns >= 1_000_000_000 {
		return fmt.Sprintf("%.2fs", float64(ns)/1e9)
	}
	if ns >= 1_000_000 {
		return fmt.Sprintf("%.1fms", float64(ns)/1e6)
	}
	if ns >= 1_000 {
		return fmt.Sprintf("%dµs", ns/1000)
	}
	return fmt.Sprintf("%dns", ns)
}

// loadCaptureData loads a .gtrace file and returns goroutines + timeline segments.
func loadCaptureData(ctx context.Context, path string) ([]model.Goroutine, []model.TimelineSegment, error) {
	capture, err := tracebridge.LoadCaptureFromPath(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	eng := analysis.NewEngine()
	sessions := session.NewManager()
	sess := sessions.StartSession("diff", path)
	eng.LoadCapture(sess, tracebridge.BindCaptureSession(capture, sess.ID))
	return eng.ListGoroutines(), eng.Timeline(), nil
}

// exitError is returned when the diff command needs to signal a non-zero exit code.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitError) ExitCode() int { return e.code }
