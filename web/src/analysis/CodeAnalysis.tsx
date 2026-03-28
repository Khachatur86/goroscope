import { useState, useCallback } from "react";

// ── Types ─────────────────────────────────────────────────────────────────────

interface AnalyzeFinding {
  rule: string;
  severity: number; // 0=critical, 1=high, 2=medium, 3=info
  location: { file: string; line: number; column: number };
  message: string;
  suggestion?: string;
  runtime_evidence?: {
    goroutine_ids: number[];
    max_block_ns: number;
    observed_at: string;
  };
}

interface AnalyzeReport {
  packages: string[];
  findings: AnalyzeFinding[];
  stats: {
    files_scanned: number;
    packages_scanned: number;
    critical: number;
    high: number;
    medium: number;
    info: number;
  };
}

const SEVERITY_LABEL = ["CRITICAL", "HIGH", "MEDIUM", "INFO"] as const;
const SEVERITY_CLASS = ["sev-critical", "sev-high", "sev-medium", "sev-info"] as const;

// ── Component ─────────────────────────────────────────────────────────────────

export function CodeAnalysis() {
  const [dirs, setDirs] = useState(".");
  const [recursive, setRecursive] = useState(false);
  const [enrichRuntime, setEnrichRuntime] = useState(false);
  const [minSeverity, setMinSeverity] = useState<0 | 1 | 2 | 3>(3);
  const [report, setReport] = useState<AnalyzeReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/v1/analyze", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          dirs: dirs.split(",").map((d) => d.trim()).filter(Boolean),
          recursive,
          enrich_runtime: enrichRuntime,
        }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(`${res.status} ${text}`);
      }
      const data: AnalyzeReport = await res.json();
      // Filter by min severity client-side
      data.findings = data.findings.filter((f) => f.severity <= minSeverity);
      setReport(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [dirs, recursive, enrichRuntime, minSeverity]);

  return (
    <div className="code-analysis">
      <div className="code-analysis-toolbar">
        <label className="ca-label">
          Directories
          <input
            className="ca-input"
            value={dirs}
            onChange={(e) => setDirs(e.target.value)}
            placeholder="., ./cmd, ./internal/..."
            spellCheck={false}
          />
        </label>

        <label className="ca-label ca-check">
          <input
            type="checkbox"
            checked={recursive}
            onChange={(e) => setRecursive(e.target.checked)}
          />
          Recursive
        </label>

        <label className="ca-label ca-check">
          <input
            type="checkbox"
            checked={enrichRuntime}
            onChange={(e) => setEnrichRuntime(e.target.checked)}
          />
          Cross-ref runtime
        </label>

        <label className="ca-label">
          Min severity
          <select
            className="ca-select"
            value={minSeverity}
            onChange={(e) => setMinSeverity(Number(e.target.value) as 0 | 1 | 2 | 3)}
          >
            <option value={0}>CRITICAL</option>
            <option value={1}>HIGH</option>
            <option value={2}>MEDIUM</option>
            <option value={3}>INFO</option>
          </select>
        </label>

        <button
          type="button"
          className="btn btn--primary ca-run-btn"
          onClick={run}
          disabled={loading}
        >
          {loading ? "Analyzing…" : "Analyze"}
        </button>
      </div>

      {error && <div className="ca-error">{error}</div>}

      {report && (
        <>
          <div className="ca-stats">
            <span>Files: <strong>{report.stats.files_scanned}</strong></span>
            <span>Packages: <strong>{report.stats.packages_scanned}</strong></span>
            {report.stats.critical > 0 && (
              <span className="sev-critical">Critical: <strong>{report.stats.critical}</strong></span>
            )}
            {report.stats.high > 0 && (
              <span className="sev-high">High: <strong>{report.stats.high}</strong></span>
            )}
            {report.stats.medium > 0 && (
              <span className="sev-medium">Medium: <strong>{report.stats.medium}</strong></span>
            )}
            {report.stats.info > 0 && (
              <span className="sev-info">Info: <strong>{report.stats.info}</strong></span>
            )}
          </div>

          {report.findings.length === 0 ? (
            <p className="ca-empty">No findings — looks clean!</p>
          ) : (
            <ul className="ca-findings">
              {report.findings.map((f, i) => (
                <FindingRow key={i} finding={f} />
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}

interface FindingRowProps {
  finding: AnalyzeFinding;
}

function FindingRow({ finding: f }: FindingRowProps) {
  const [open, setOpen] = useState(false);
  const sevClass = SEVERITY_CLASS[f.severity] ?? "sev-info";
  const sevLabel = SEVERITY_LABEL[f.severity] ?? "INFO";

  return (
    <li className={`ca-finding ${sevClass}`}>
      <button
        type="button"
        className="ca-finding-header"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span className={`ca-badge ${sevClass}`}>{sevLabel}</span>
        <span className="ca-rule">{f.rule}</span>
        <span className="ca-loc">
          {f.location.file}:{f.location.line}
        </span>
        <span className="ca-msg">{f.message}</span>
        <span className="ca-chevron">{open ? "▾" : "▸"}</span>
      </button>

      {open && (
        <div className="ca-finding-body">
          {f.suggestion && (
            <p className="ca-suggestion">
              <strong>Suggestion:</strong> {f.suggestion}
            </p>
          )}
          {f.runtime_evidence && (
            <p className="ca-evidence">
              <strong>Runtime evidence:</strong>{" "}
              goroutines [{f.runtime_evidence.goroutine_ids.join(", ")}]
              {f.runtime_evidence.max_block_ns > 0 && (
                <> · max block {(f.runtime_evidence.max_block_ns / 1e6).toFixed(2)}ms</>
              )}
            </p>
          )}
        </div>
      )}
    </li>
  );
}
