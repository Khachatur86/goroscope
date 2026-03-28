package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Khachatur86/goroscope/internal/staticanalysis"
)

// analyzeCommand implements `goroscope analyze [flags] [dirs...]`.
func analyzeCommand(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(stderr)

	format := fs.String("format", "text", "Output format: text or json")
	recursive := fs.Bool("recursive", false, "Recurse into subdirectories")
	rules := fs.String("rules", "", "Comma-separated rule IDs to enable (default: all); e.g. SA-1,SA-7")
	minSeverity := fs.String("min-severity", "", "Minimum severity to report: CRITICAL, HIGH, MEDIUM, INFO (default: INFO)")
	exitCode := fs.Bool("exit-code", false, "Exit 1 when any finding is emitted (useful for CI)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	dirs := fs.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var ruleIDs []staticanalysis.RuleID
	if *rules != "" {
		for _, r := range splitCSV(*rules) {
			ruleIDs = append(ruleIDs, staticanalysis.RuleID(strings.TrimSpace(r)))
		}
	}

	report, err := staticanalysis.Analyze(staticanalysis.AnalyzeInput{
		Dirs:      dirs,
		Recursive: *recursive,
		Rules:     ruleIDs,
	})
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	// Filter by minimum severity.
	minSev := parseSeverity(*minSeverity)
	filtered := report.Findings[:0]
	for _, f := range report.Findings {
		if f.Severity <= minSev {
			filtered = append(filtered, f)
		}
	}
	report.Findings = filtered

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	default:
		printAnalysisReport(stdout, report)
	}

	if *exitCode && len(report.Findings) > 0 {
		os.Exit(1)
	}
	return nil
}

// parseSeverity converts a string to Severity; defaults to Info (least severe).
func parseSeverity(s string) staticanalysis.Severity {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CRITICAL":
		return staticanalysis.SeverityCritical
	case "HIGH":
		return staticanalysis.SeverityHigh
	case "MEDIUM":
		return staticanalysis.SeverityMedium
	default:
		return staticanalysis.SeverityInfo
	}
}

func printAnalysisReport(w io.Writer, report *staticanalysis.Report) {
	stats := report.Stats
	_, _ = fmt.Fprintf(w, "Scanned %d file(s) in %d package(s)\n\n",
		stats.FilesScanned, stats.PackagesScanned)

	if len(report.Findings) == 0 {
		_, _ = fmt.Fprintln(w, "No findings.")
		return
	}

	for _, f := range report.Findings {
		_, _ = fmt.Fprintf(w, "[%s] %s  %s\n", f.Severity, f.Rule, f.Location)
		_, _ = fmt.Fprintf(w, "  %s\n", f.Message)
		if f.Suggestion != "" {
			_, _ = fmt.Fprintf(w, "  Suggestion: %s\n", f.Suggestion)
		}
		if f.RuntimeEvidence != nil && len(f.RuntimeEvidence.GoroutineIDs) > 0 {
			_, _ = fmt.Fprintf(w, "  Runtime evidence: goroutines %v (max block %dns)\n",
				f.RuntimeEvidence.GoroutineIDs, f.RuntimeEvidence.MaxBlockNS)
		}
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "Summary — critical:%d  high:%d  medium:%d  info:%d\n",
		stats.Critical, stats.High, stats.Medium, stats.Info)
}
