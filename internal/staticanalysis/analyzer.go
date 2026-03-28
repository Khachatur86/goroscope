package staticanalysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// AnalyzeInput configures a single analysis run.
type AnalyzeInput struct {
	// Dirs are the directories to scan (non-recursive). Use "." for current dir.
	Dirs []string
	// Recursive enables recursive directory walking.
	Recursive bool
	// Rules is the set of rule IDs to run. Empty means all rules.
	Rules []RuleID
}

// Analyze runs all enabled detectors over the Go source files in the given
// directories and returns a consolidated Report.
func Analyze(input AnalyzeInput) (*Report, error) {
	enabled := buildRuleSet(input.Rules)
	detectors := allDetectors(enabled)

	fset := token.NewFileSet()
	report := &Report{}

	dirs, err := expandDirs(input.Dirs, input.Recursive)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	for _, dir := range dirs {
		pkgs, err := parser.ParseDir(fset, dir, isGoFile, parser.ParseComments)
		if err != nil {
			// Best-effort: skip unparseable packages.
			continue
		}
		for pkgPath, pkg := range pkgs {
			if seen[pkgPath] {
				continue
			}
			seen[pkgPath] = true
			report.Packages = append(report.Packages, pkgPath)
			report.Stats.PackagesScanned++

			for _, file := range pkg.Files {
				report.Stats.FilesScanned++
				for _, d := range detectors {
					findings := d.Detect(fset, file)
					report.Findings = append(report.Findings, findings...)
				}
			}
		}
	}

	sortFindings(report.Findings)
	countStats(&report.Stats, report.Findings)
	return report, nil
}

// detector is implemented by each concurrency rule.
type detector interface {
	Detect(fset *token.FileSet, file *ast.File) []Finding
}

func buildRuleSet(rules []RuleID) map[RuleID]bool {
	if len(rules) == 0 {
		return nil // nil = all enabled
	}
	m := make(map[RuleID]bool, len(rules))
	for _, r := range rules {
		m[r] = true
	}
	return m
}

func allDetectors(enabled map[RuleID]bool) []detector {
	all := []struct {
		id RuleID
		d  detector
	}{
		{RuleLockWithoutDefer, &lockWithoutDeferDetector{}},
		{RuleLoopClosure, &loopClosureDetector{}},
		{RuleWaitGroupAfterGo, &waitGroupAfterGoDetector{}},
		{RuleMutexByValue, &mutexByValueDetector{}},
		{RuleUnbufferedChanSend, &unbufferedChanSendDetector{}},
		{RuleDoubleLock, &doubleLockDetector{}},
		{RuleSleepNoContext, &sleepNoContextDetector{}},
	}
	var out []detector
	for _, e := range all {
		if enabled == nil || enabled[e.id] {
			out = append(out, e.d)
		}
	}
	return out
}

func isGoFile(info fs.FileInfo) bool {
	name := info.Name()
	return !info.IsDir() &&
		strings.HasSuffix(name, ".go") &&
		!strings.HasSuffix(name, "_test.go")
}

func expandDirs(dirs []string, recursive bool) ([]string, error) {
	if !recursive {
		return dirs, nil
	}
	var out []string
	for _, root := range dirs {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable paths
			}
			if d.IsDir() {
				// Skip hidden and vendor directories.
				name := d.Name()
				if name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
					return filepath.SkipDir
				}
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity // lower int = higher severity
		}
		if a.Location.File != b.Location.File {
			return a.Location.File < b.Location.File
		}
		return a.Location.Line < b.Location.Line
	})
}

func countStats(s *ReportStats, findings []Finding) {
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			s.Critical++
		case SeverityHigh:
			s.High++
		case SeverityMedium:
			s.Medium++
		default:
			s.Info++
		}
	}
}
