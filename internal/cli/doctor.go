package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

// doctorInput holds validated parameters for runDoctor (CS-5).
type doctorInput struct {
	CaptureFile string
	Stdout      io.Writer
	Stderr      io.Writer
}

func doctorCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope doctor <capture-file>")
		_, _ = fmt.Fprintln(stderr, "")
		_, _ = fmt.Fprintln(stderr, "Generate a self-contained HTML diagnostic report from a .gtrace file.")
		_, _ = fmt.Fprintln(stderr, "The report includes: insights, deadlock hints, contention, flamegraph.")
		_, _ = fmt.Fprintln(stderr, "")
		_, _ = fmt.Fprintln(stderr, "Example:")
		_, _ = fmt.Fprintln(stderr, "  goroscope doctor capture.gtrace > report.html")
		_, _ = fmt.Fprintln(stderr, "  goroscope doctor capture.gtrace | open -f -a Safari")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope doctor <capture-file>")
		return fmt.Errorf("capture file required")
	}
	return runDoctor(ctx, doctorInput{
		CaptureFile: fs.Arg(0),
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

func runDoctor(ctx context.Context, in doctorInput) error {
	capture, err := tracebridge.LoadCaptureFromPath(ctx, in.CaptureFile)
	if err != nil {
		return fmt.Errorf("load capture: %w", err)
	}

	eng := analysis.NewEngine()
	mgr := session.NewManager()
	sess := mgr.StartSession("doctor", in.CaptureFile)
	eng.LoadCapture(sess, tracebridge.BindCaptureSession(capture, sess.ID))

	goroutines := eng.ListGoroutines()
	edges := eng.ResourceGraph()
	if len(edges) == 0 {
		edges = analysis.DeriveCurrentContentionEdges(goroutines)
	}
	segments := eng.Timeline()
	insights := analysis.GenerateInsights(analysis.GenerateInsightsInput{
		Goroutines: goroutines,
		Segments:   segments,
		Edges:      edges,
	})
	hints := analysis.FindDeadlockHints(edges, goroutines)
	contention := eng.ResourceContention()
	flamegraphResult := eng.Flamegraph("", 0)

	flamegraphJSON, err := json.Marshal(flamegraphResult.Root)
	if err != nil {
		return fmt.Errorf("marshal flamegraph: %w", err)
	}

	report := buildDoctorReport(doctorReportData{
		CaptureFile:    in.CaptureFile,
		GeneratedAt:    time.Now().UTC().Format(time.RFC1123),
		Goroutines:     goroutines,
		Insights:       insights,
		Hints:          hints,
		Contention:     contention,
		FlamegraphJSON: string(flamegraphJSON),
	})
	_, err = fmt.Fprint(in.Stdout, report)
	return err
}

// doctorReportData holds all data needed to render the HTML report.
type doctorReportData struct {
	CaptureFile    string
	GeneratedAt    string
	Goroutines     []model.Goroutine
	Insights       []analysis.Insight
	Hints          []analysis.DeadlockHint
	Contention     []analysis.ResourceContention
	FlamegraphJSON string
}

func buildDoctorReport(d doctorReportData) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>goroscope doctor — `)
	sb.WriteString(html.EscapeString(d.CaptureFile))
	sb.WriteString(`</title>
<style>
  :root { --bg:#0d1117; --fg:#e6edf3; --border:#30363d; --accent:#58a6ff;
          --red:#f85149; --yellow:#d29922; --green:#3fb950; --muted:#8b949e; }
  * { box-sizing:border-box; margin:0; padding:0; }
  body { background:var(--bg); color:var(--fg); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",monospace; font-size:14px; }
  header { padding:16px 24px; border-bottom:1px solid var(--border); display:flex; align-items:center; gap:16px; }
  header h1 { font-size:20px; }
  header .meta { color:var(--muted); font-size:12px; }
  section { padding:24px; border-bottom:1px solid var(--border); }
  section h2 { font-size:16px; margin-bottom:12px; color:var(--accent); }
  .badge { display:inline-block; padding:2px 8px; border-radius:4px; font-size:11px; font-weight:600; text-transform:uppercase; }
  .badge-error   { background:#3d0c0c; color:var(--red); }
  .badge-warning { background:#2d1e00; color:var(--yellow); }
  .badge-info    { background:#0c1e36; color:var(--accent); }
  .badge-ok      { background:#0c2d12; color:var(--green); }
  table { width:100%; border-collapse:collapse; font-size:13px; }
  th { text-align:left; padding:8px 12px; border-bottom:1px solid var(--border); color:var(--muted); font-weight:600; }
  td { padding:6px 12px; border-bottom:1px solid var(--border); vertical-align:top; }
  tr:hover td { background:rgba(255,255,255,.03); }
  .state-RUNNING   { color:var(--green); }
  .state-RUNNABLE  { color:#79c0ff; }
  .state-BLOCKED   { color:var(--yellow); }
  .state-WAITING   { color:var(--yellow); }
  .state-SYSCALL   { color:#d2a8ff; }
  .flame-container { height:400px; background:#161b22; border:1px solid var(--border); border-radius:6px; padding:12px; overflow:auto; }
  .empty { color:var(--muted); font-style:italic; padding:8px 0; }
  .summary-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(160px,1fr)); gap:12px; margin-bottom:16px; }
  .summary-card { background:#161b22; border:1px solid var(--border); border-radius:6px; padding:16px; text-align:center; }
  .summary-card .num  { font-size:28px; font-weight:700; }
  .summary-card .lbl  { font-size:12px; color:var(--muted); margin-top:4px; }
  pre { background:#161b22; border:1px solid var(--border); border-radius:6px; padding:12px; overflow:auto; white-space:pre-wrap; font-size:12px; }
</style>
</head>
<body>
<header>
  <div>
    <h1>goroscope doctor</h1>
    <div class="meta">`)
	sb.WriteString(html.EscapeString(d.CaptureFile))
	sb.WriteString(` &nbsp;·&nbsp; Generated `)
	sb.WriteString(html.EscapeString(d.GeneratedAt))
	sb.WriteString(`</div>
  </div>
</header>
`)

	// ── Summary cards ──────────────────────────────────────────────────────────
	stateCounts := make(map[model.GoroutineState]int)
	for _, g := range d.Goroutines {
		stateCounts[g.State]++
	}

	sb.WriteString(`<section>
<h2>Summary</h2>
<div class="summary-grid">
`)
	writeCard(&sb, strconv.Itoa(len(d.Goroutines)), "Goroutines")
	writeCard(&sb, strconv.Itoa(stateCounts[model.StateRunning]), "Running")
	writeCard(&sb, strconv.Itoa(stateCounts[model.StateBlocked]+stateCounts[model.StateWaiting]), "Blocked / Waiting")
	writeCard(&sb, strconv.Itoa(len(d.Hints)), "Deadlock hints")
	writeCard(&sb, strconv.Itoa(len(d.Insights)), "Insights")
	sb.WriteString("</div>\n</section>\n")

	// ── Insights ───────────────────────────────────────────────────────────────
	sb.WriteString(`<section>
<h2>Insights</h2>
`)
	if len(d.Insights) == 0 {
		sb.WriteString(`<p class="empty">No insights found — looks healthy.</p>`)
	} else {
		sb.WriteString(`<table>
<thead><tr><th>Severity</th><th>Type</th><th>Message</th><th>Goroutines</th></tr></thead>
<tbody>
`)
		for _, ins := range d.Insights {
			badgeClass := insightBadgeClass(ins.Severity)
			sb.WriteString("<tr><td><span class=\"badge ")
			sb.WriteString(badgeClass)
			sb.WriteString("\">")
			sb.WriteString(html.EscapeString(string(ins.Severity)))
			sb.WriteString("</span></td><td>")
			sb.WriteString(html.EscapeString(string(ins.Kind)))
			sb.WriteString("</td><td>")
			sb.WriteString(html.EscapeString(ins.Title))
			sb.WriteString("</td><td>")
			sb.WriteString(strconv.Itoa(len(ins.GoroutineIDs)))
			sb.WriteString("</td></tr>\n")
		}
		sb.WriteString("</tbody></table>\n")
	}
	sb.WriteString("</section>\n")

	// ── Deadlock hints ─────────────────────────────────────────────────────────
	sb.WriteString(`<section>
<h2>Deadlock hints</h2>
`)
	if len(d.Hints) == 0 {
		sb.WriteString(`<p class="empty">No deadlock hints detected.</p>`)
	} else {
		sb.WriteString(`<table>
<thead><tr><th>Cycle</th><th>Goroutines</th><th>Resource</th></tr></thead>
<tbody>
`)
		for i, h := range d.Hints {
			ids := make([]string, len(h.GoroutineIDs))
			for j, id := range h.GoroutineIDs {
				ids[j] = strconv.Itoa(int(id))
			}
			sb.WriteString("<tr><td>")
			sb.WriteString(strconv.Itoa(i + 1))
			sb.WriteString("</td><td>")
			sb.WriteString(html.EscapeString(strings.Join(ids, " → ")))
			sb.WriteString("</td><td>")
			if len(h.ResourceIDs) > 0 {
				sb.WriteString(html.EscapeString(h.ResourceIDs[0]))
			}
			sb.WriteString("</td></tr>\n")
		}
		sb.WriteString("</tbody></table>\n")
	}
	sb.WriteString("</section>\n")

	// ── Contention ─────────────────────────────────────────────────────────────
	sb.WriteString(`<section>
<h2>Resource contention (top 10)</h2>
`)
	if len(d.Contention) == 0 {
		sb.WriteString(`<p class="empty">No resource contention detected.</p>`)
	} else {
		sb.WriteString(`<table>
<thead><tr><th>Resource</th><th>Peak waiters</th><th>Segments</th><th>Avg wait (ms)</th></tr></thead>
<tbody>
`)
		limit := len(d.Contention)
		if limit > 10 {
			limit = 10
		}
		for _, c := range d.Contention[:limit] {
			avgMS := float64(c.TotalWaitNS) / float64(c.SegmentCount) / 1e6
			sb.WriteString("<tr><td>")
			sb.WriteString(html.EscapeString(c.ResourceID))
			sb.WriteString("</td><td>")
			sb.WriteString(strconv.Itoa(c.PeakWaiters))
			sb.WriteString("</td><td>")
			sb.WriteString(strconv.Itoa(c.SegmentCount))
			sb.WriteString("</td><td>")
			sb.WriteString(fmt.Sprintf("%.1f", avgMS))
			sb.WriteString("</td></tr>\n")
		}
		sb.WriteString("</tbody></table>\n")
	}
	sb.WriteString("</section>\n")

	// ── Flamegraph ─────────────────────────────────────────────────────────────
	sb.WriteString(`<section>
<h2>Flamegraph (call-stack aggregation)</h2>
<div class="flame-container" id="flame"></div>
<script>
(function(){
  const data = `)
	sb.WriteString(d.FlamegraphJSON)
	sb.WriteString(`;
  const container = document.getElementById('flame');
  const W = container.clientWidth || 800;
  function render(node, x, y, w, depth, colors) {
    if (w < 2) return;
    const div = document.createElement('div');
    const hue = (depth * 37) % 360;
    div.style.cssText = 'position:absolute;left:'+x+'px;top:'+y+'px;width:'+(w-1)+'px;height:18px;' +
      'background:hsl('+hue+',60%,35%);border:1px solid rgba(0,0,0,.3);overflow:hidden;' +
      'cursor:default;font-size:11px;line-height:18px;padding:0 3px;white-space:nowrap;';
    const pct = node.value ? ((node.value / data.value) * 100).toFixed(1) : '?';
    div.title = node.name + ' (' + pct + '%)';
    div.textContent = node.name;
    container.appendChild(div);
    if (!node.children) return;
    let cx = x;
    const totalVal = node.value || 1;
    for (const child of node.children) {
      const cw = Math.floor((child.value / totalVal) * w);
      render(child, cx, y + 20, cw, depth + 1, colors);
      cx += cw;
    }
  }
  container.style.position = 'relative';
  if (data && data.value > 0) {
    render(data, 0, 0, W, 0, null);
    const totalH = (function depth(n, d){ return n.children ? Math.max(...n.children.map(c=>depth(c,d+1))) : d; })(data, 1);
    container.style.height = (totalH * 20 + 20) + 'px';
  } else {
    container.textContent = 'No stack data available.';
    container.style.color = '#8b949e';
  }
})();
</script>
</section>
`)

	// ── Goroutine table ────────────────────────────────────────────────────────
	sb.WriteString(`<section>
<h2>Goroutines</h2>
<table>
<thead><tr><th>ID</th><th>State</th><th>Reason</th><th>Top frame</th></tr></thead>
<tbody>
`)
	limit := len(d.Goroutines)
	if limit > 500 {
		limit = 500
	}
	for _, g := range d.Goroutines[:limit] {
		frame := ""
		if g.LastStack != nil && len(g.LastStack.Frames) > 0 {
			frame = g.LastStack.Frames[0].Func
		}
		stateClass := "state-" + string(g.State)
		sb.WriteString("<tr><td>")
		sb.WriteString(strconv.Itoa(int(g.ID)))
		sb.WriteString(`</td><td class="`)
		sb.WriteString(stateClass)
		sb.WriteString(`">`)
		sb.WriteString(html.EscapeString(string(g.State)))
		sb.WriteString("</td><td>")
		sb.WriteString(html.EscapeString(string(g.Reason)))
		sb.WriteString("</td><td>")
		sb.WriteString(html.EscapeString(frame))
		sb.WriteString("</td></tr>\n")
	}
	sb.WriteString("</tbody></table>\n")
	if len(d.Goroutines) > 500 {
		sb.WriteString(fmt.Sprintf(`<p class="empty">… %d more goroutines omitted</p>`, len(d.Goroutines)-500))
	}
	sb.WriteString("</section>\n</body>\n</html>\n")

	return sb.String()
}

func writeCard(sb *strings.Builder, num, label string) {
	sb.WriteString(`<div class="summary-card"><div class="num">`)
	sb.WriteString(html.EscapeString(num))
	sb.WriteString(`</div><div class="lbl">`)
	sb.WriteString(html.EscapeString(label))
	sb.WriteString("</div></div>\n")
}

func insightBadgeClass(sev analysis.InsightSeverity) string {
	switch sev {
	case analysis.SeverityCritical:
		return "badge-error"
	case analysis.SeverityWarning:
		return "badge-warning"
	case analysis.SeverityInfo:
		return "badge-info"
	default:
		return "badge-ok"
	}
}
