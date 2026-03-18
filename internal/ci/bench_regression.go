package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Example line (go test -bench):
// BenchmarkEngineLoadCapture/goroutines=100-8         1.234 ns/op
//
// We parse: benchmark name, numeric value, and unit.
var benchLineRE = regexp.MustCompile(`^(Benchmark\S+)\s+\d+\s+([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Zµμ/]+)\/op`)

func normalizeUnit(unit string) (string, error) {
	u := strings.TrimSpace(strings.ToLower(unit))
	u = strings.ReplaceAll(u, "μ", "µ")
	switch u {
	case "ns":
		return "ns", nil
	case "us", "µs":
		return "us", nil
	case "ms":
		return "ms", nil
	case "s":
		return "s", nil
	default:
		// Be tolerant for slightly different notations.
		if strings.Contains(u, "ns") {
			return "ns", nil
		}
		if strings.Contains(u, "ms") {
			return "ms", nil
		}
		if strings.Contains(u, "us") || strings.Contains(u, "µs") {
			return "us", nil
		}
		return "", fmt.Errorf("unknown unit %q", unit)
	}
}

func toNS(value float64, unit string) (float64, error) {
	n, err := normalizeUnit(unit)
	if err != nil {
		return 0, err
	}
	switch n {
	case "ns":
		return value, nil
	case "us":
		return value * 1_000.0, nil
	case "ms":
		return value * 1_000_000.0, nil
	case "s":
		return value * 1_000_000_000.0, nil
	default:
		return 0, errors.New("unreachable")
	}
}

type benchMetric struct {
	nsPerOp float64
}

func parseBenchFile(path string) (map[string]benchMetric, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]benchMetric)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		m := benchLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		// go test appends "-<procs>" to the benchmark name (e.g. ".../goroutines=100-8").
		// Normalize it away so baseline/head match reliably.
		if idx := strings.LastIndex(name, "-"); idx > 0 {
			suf := name[idx+1:]
			if _, err := strconv.Atoi(suf); err == nil {
				name = name[:idx]
			}
		}

		val, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		unit := m[3]
		ns, err := toNS(val, unit)
		if err != nil {
			continue
		}
		out[name] = benchMetric{nsPerOp: ns}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func formatPct(x float64) string {
	sign := "+"
	if x < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, x)
}

func main() {
	var (
		baselinePath string
		headPath     string
		threshold    float64
		outPath      string
	)
	flag.StringVar(&baselinePath, "baseline", "", "baseline benchmark output")
	flag.StringVar(&headPath, "head", "", "head benchmark output")
	flag.Float64Var(&threshold, "threshold", 0.10, "regression threshold fraction (0.10=10%)")
	flag.StringVar(&outPath, "out", "bench_regression_report.txt", "report file")
	flag.Parse()

	args := flag.Args()
	// Support both:
	// 1) positional: bench_regression.go baseline.txt head.txt --threshold ...
	// 2) flags: --baseline baseline.txt --head head.txt ...
	if baselinePath == "" && headPath == "" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bench_regression.go <baseline.txt> <head.txt> [--threshold=0.10] [--out=report.txt]")
			os.Exit(2)
		}
		baselinePath = args[0]
		headPath = args[1]
	}

	base, err := parseBenchFile(baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse baseline:", err)
		os.Exit(2)
	}
	head, err := parseBenchFile(headPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse head:", err)
		os.Exit(2)
	}
	if len(base) == 0 || len(head) == 0 {
		fmt.Fprintln(os.Stderr, "failed to parse baseline/head benchmarks")
		os.Exit(2)
	}

	thresholdPct := threshold * 100.0

	type regression struct {
		name     string
		baseNS   float64
		headNS   float64
		deltaPct float64
	}

	var regressions []regression
	for name, bmBase := range base {
		hm, ok := head[name]
		if !ok {
			continue
		}
		if bmBase.nsPerOp <= 0 {
			continue
		}
		deltaPct := ((hm.nsPerOp - bmBase.nsPerOp) / bmBase.nsPerOp) * 100.0
		if deltaPct > thresholdPct {
			regressions = append(regressions, regression{
				name:     name,
				baseNS:   bmBase.nsPerOp,
				headNS:   hm.nsPerOp,
				deltaPct: deltaPct,
			})
		}
	}

	// Stable ordering by largest delta first.
	// (bubble sort is fine for tiny benchmark sets, but we'll use a simple insertion.)
	for i := 1; i < len(regressions); i++ {
		for j := i; j > 0; j-- {
			if regressions[j-1].deltaPct < regressions[j].deltaPct {
				regressions[j-1], regressions[j] = regressions[j], regressions[j-1]
			}
		}
	}

	reportLines := []string{
		"Bench regression check (ns/op, go test -bench)",
		fmt.Sprintf("Baseline: %s", baselinePath),
		fmt.Sprintf("Head: %s", headPath),
		fmt.Sprintf("Threshold: %s", formatPct(thresholdPct)),
		"",
		fmt.Sprintf("Parsed benchmarks: baseline=%d, head=%d", len(base), len(head)),
		"",
	}

	compared := 0
	for name := range base {
		if _, ok := head[name]; ok {
			compared++
		}
	}
	reportLines = append(reportLines, fmt.Sprintf("Compared benchmarks: %d", compared), "")

	if len(regressions) == 0 {
		reportLines = append(reportLines, "No regressions detected.", "")
		_ = os.WriteFile(outPath, []byte(strings.Join(reportLines, "\n")), 0o644)
		return
	}

	reportLines = append(reportLines, "Regressions detected:")
	for _, r := range regressions {
		reportLines = append(reportLines,
			fmt.Sprintf("- %s: base=%.3g ns/op, head=%.3g ns/op, delta=%s", r.name, r.baseNS, r.headNS, formatPct(r.deltaPct)),
		)
	}
	reportLines = append(reportLines, "", fmt.Sprintf("Failing because regression exceeds threshold (> %s).", formatPct(thresholdPct)))

	absOut, _ := filepath.Abs(outPath)
	_ = os.WriteFile(absOut, []byte(strings.Join(reportLines, "\n")+"\n"), 0o644)

	fmt.Fprintln(os.Stderr, strings.Join(reportLines[len(reportLines)-3:], "\n"))
	os.Exit(1)
}

