package cli

import (
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
)

func TestBuildDoctorReport_ContainsExpectedSections(t *testing.T) {
	t.Parallel()
	d := doctorReportData{
		CaptureFile: "test.gtrace",
		GeneratedAt: "Thu, 01 Jan 2026 00:00:00 UTC",
		Goroutines: []model.Goroutine{
			{ID: 1, State: model.StateRunning},
			{ID: 2, State: model.StateBlocked, Reason: model.ReasonMutexLock},
		},
		Insights: []analysis.Insight{
			{Kind: "leak", Severity: analysis.SeverityWarning, Title: "potential leak", GoroutineIDs: []int64{2}},
		},
		Hints:          nil,
		Contention:     nil,
		FlamegraphJSON: `{"name":"root","value":2}`,
	}

	html := buildDoctorReport(d)

	checks := []string{
		"<!DOCTYPE html>",
		"goroscope doctor",
		"test.gtrace",
		"Thu, 01 Jan 2026 00:00:00 UTC",
		"Summary",
		"Insights",
		"Deadlock hints",
		"Resource contention",
		"Flamegraph",
		"Goroutines",
		"potential leak",
		"badge-warning",
		"No deadlock hints detected.",
		"No resource contention detected.",
		`{"name":"root","value":2}`,
	}
	for _, want := range checks {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestBuildDoctorReport_EscapesHTML(t *testing.T) {
	t.Parallel()
	d := doctorReportData{
		CaptureFile:    "<script>alert(1)</script>.gtrace",
		GeneratedAt:    "now",
		FlamegraphJSON: `{"name":"root","value":0}`,
	}
	out := buildDoctorReport(d)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("XSS: unescaped script tag in output")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected HTML-escaped script tag")
	}
}

func TestBuildDoctorReport_GoroutineTruncation(t *testing.T) {
	t.Parallel()
	goroutines := make([]model.Goroutine, 505)
	for i := range goroutines {
		goroutines[i] = model.Goroutine{ID: int64(i + 1), State: model.StateRunning}
	}
	d := doctorReportData{
		CaptureFile:    "big.gtrace",
		GeneratedAt:    "now",
		Goroutines:     goroutines,
		FlamegraphJSON: `{"name":"root","value":0}`,
	}
	out := buildDoctorReport(d)
	if !strings.Contains(out, "more goroutines omitted") {
		t.Error("expected truncation message for >500 goroutines")
	}
}

func TestDoctorCommand_MissingFile(t *testing.T) {
	t.Parallel()
	err := doctorCommand(t.Context(), []string{}, &strings.Builder{}, &strings.Builder{})
	if err == nil {
		t.Fatal("expected error for missing capture file")
	}
}

func TestInsightBadgeClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sev  analysis.InsightSeverity
		want string
	}{
		{analysis.SeverityCritical, "badge-error"},
		{analysis.SeverityWarning, "badge-warning"},
		{analysis.SeverityInfo, "badge-info"},
		{"unknown", "badge-ok"},
	}
	for _, tc := range cases {
		if got := insightBadgeClass(tc.sev); got != tc.want {
			t.Errorf("insightBadgeClass(%q) = %q, want %q", tc.sev, got, tc.want)
		}
	}
}
