// Package cli implements the goroscope command-line interface.
package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/api"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/pprofpoll"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
	"github.com/Khachatur86/goroscope/internal/version"
)

// Run is the CLI entry point; it parses args and dispatches to the appropriate command.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "attach":
		return attachCommand(ctx, args[1:], stdout, stderr)
	case "run":
		return runCommand(ctx, args[1:], stdout, stderr)
	case "collect":
		return collectCommand(ctx, args[1:], stdout, stderr)
	case "ui":
		return uiCommand(ctx, args[1:], stdout, stderr)
	case "replay":
		return replayCommand(ctx, args[1:], stdout, stderr)
	case "check":
		return checkCommand(ctx, args[1:], stdout, stderr)
	case "export":
		return exportCommand(ctx, args[1:], stdout, stderr)
	case "test":
		return testCommand(ctx, args[1:], stdout, stderr)
	case "version":
		_, _ = fmt.Fprintln(stdout, version.Version)
		return nil
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q; run 'goroscope help' for usage", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Goroscope — local Go concurrency debugger")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  goroscope attach [flags] <url>")
	_, _ = fmt.Fprintln(w, "  goroscope run [flags] <package-or-binary>")
	_, _ = fmt.Fprintln(w, "  goroscope test [flags] [packages] [go-test-flags]")
	_, _ = fmt.Fprintln(w, "  goroscope collect [flags]")
	_, _ = fmt.Fprintln(w, "  goroscope ui [flags]")
	_, _ = fmt.Fprintln(w, "  goroscope replay [flags] <capture-file>")
	_, _ = fmt.Fprintln(w, "  goroscope check <capture-file>")
	_, _ = fmt.Fprintln(w, "  goroscope export [flags] <capture-file>")
	_, _ = fmt.Fprintln(w, "  goroscope version")
	_, _ = fmt.Fprintln(w, "  goroscope help")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  attach    Attach to any running Go process via /debug/pprof (zero code changes)")
	_, _ = fmt.Fprintln(w, "  run       Run a Go program with live trace capture (target must use agent)")
	_, _ = fmt.Fprintln(w, "  test      Run 'go test' with tracing, then open the UI with the result")
	_, _ = fmt.Fprintln(w, "  collect   Load demo data and serve UI")
	_, _ = fmt.Fprintln(w, "  ui        Load demo data and serve UI")
	_, _ = fmt.Fprintln(w, "  replay    Load a .gtrace capture file and serve UI")
	_, _ = fmt.Fprintln(w, "  check     Analyze capture for deadlock hints; exit 1 if found (--format text|json|github|dot)")
	_, _ = fmt.Fprintln(w, "  export    Export timeline segments to CSV or JSON (for pandas, analysis)")
	_, _ = fmt.Fprintln(w, "  version   Print version")
	_, _ = fmt.Fprintln(w, "  help      Show this help")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Common flags (run, test, collect, ui, replay):")
	_, _ = fmt.Fprintln(w, "  -addr string       HTTP bind address (default \"127.0.0.1:7070\")")
	_, _ = fmt.Fprintln(w, "  -open-browser      Open the default browser to the UI")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Run-specific flags:")
	_, _ = fmt.Fprintln(w, "  -session-name      Session name (default \"local-run\")")
	_, _ = fmt.Fprintln(w, "  -poll-interval     How often to re-read the trace file (default 1s)")
	_, _ = fmt.Fprintln(w, "  -save path         Save capture to .gtrace file when session completes")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Test-specific flags:")
	_, _ = fmt.Fprintln(w, "  -save path         Save capture to .gtrace file after tests complete")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  goroscope run ./examples/trace_demo --open-browser")
	_, _ = fmt.Fprintln(w, "  goroscope test ./pkg/worker -run TestWorkerPool -open-browser")
	_, _ = fmt.Fprintln(w, "  goroscope ui --open-browser")
}

// openBrowserURL opens the default browser to the given URL. It returns silently on
// failure (e.g. headless environment) so the CLI does not block or error.
func openBrowserURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// scheduleOpenBrowser spawns a goroutine that opens the browser after a short
// delay, but respects ctx cancellation (CC-2: goroutine lifetime tied to context).
func scheduleOpenBrowser(ctx context.Context, delay time.Duration, url string) {
	go func() {
		select {
		case <-time.After(delay):
			openBrowserURL(url)
		case <-ctx.Done():
		}
	}()
}

// attachCommand implements `goroscope attach <url>`.
// It polls the target's /debug/pprof/goroutine endpoint, feeds the data into
// the engine, and serves the UI — no changes to the target process required.
func attachCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope attach [flags] <url>\n\n")
		_, _ = fmt.Fprintf(stderr, "Attach to any running Go process that exposes /debug/pprof.\n")
		_, _ = fmt.Fprintf(stderr, "The URL should be the base address, e.g. http://localhost:6060\n\n")
		_, _ = fmt.Fprintf(stderr, "Example:\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope attach http://localhost:6060 --open-browser\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address for the goroscope UI")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser when ready")
	interval := fs.Duration("interval", 2*time.Second, "Poll interval")
	sessionName := fs.String("session-name", "attach", "Session name shown in the UI")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")
	maxSegments := fs.Int("max-segments", 500_000, "Maximum closed timeline segments to retain in memory (0 = unlimited)")
	maxStacks := fs.Int("max-stacks", 200, "Maximum stack snapshots to retain per goroutine (0 = unlimited)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("missing target URL; usage: goroscope attach <url>")
	}
	targetURL := fs.Arg(0)

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	engine := analysis.NewEngine(analysis.WithRetention(analysis.RetentionPolicy{
		MaxClosedSegments:     *maxSegments,
		MaxStacksPerGoroutine: *maxStacks,
	}))
	sessions := session.NewManager()

	poller := pprofpoll.NewPoller(pprofpoll.PollInput{
		TargetURL:   targetURL,
		Interval:    *interval,
		Engine:      engine,
		Sessions:    sessions,
		SessionName: *sessionName,
	})

	_, _ = fmt.Fprintf(stdout, "goroscope attach: connecting to %s ...\n", targetURL)

	// Verify the target is reachable with a single synchronous poll.
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer verifyCancel()
	if err := poller.PollOnce(verifyCtx); err != nil {
		return fmt.Errorf("attach: cannot reach %s: %w", targetURL, err)
	}
	_, _ = fmt.Fprintf(stdout, "goroscope attach: connected. Starting polling loop.\n")

	// Start continuous polling in the background.
	go poller.Run(ctx, stderr)

	server := api.NewServer(*addr, engine, sessions, uiPathResolved)
	uiURL := "http://" + *addr
	_, _ = fmt.Fprintf(stdout, "goroscope attach: UI at %s  (polling every %s)\n", uiURL, *interval)

	if *openBrowser {
		scheduleOpenBrowser(ctx, 300*time.Millisecond, uiURL)
	}

	return server.Serve(ctx)
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope run [flags] <package-or-binary>\n\n")
		_, _ = fmt.Fprintf(stderr, "Run a Go program with live trace capture. The target must import\n")
		_, _ = fmt.Fprintf(stderr, "github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main.\n\n")
		_, _ = fmt.Fprintf(stderr, "Flags must appear before the target. Example with React UI:\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope run -ui=react -open-browser ./examples/trace_demo\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser to the UI")
	sessionName := fs.String("session-name", "local-run", "Session name")
	pollInterval := fs.Duration("poll-interval", time.Second, "How often to re-read the live trace file")
	savePath := fs.String("save", "", "Save capture to file when session completes (e.g. ./captures/run.gtrace)")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	target := "./app"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	return serveLiveRunSession(ctx, serveLiveRunInput{
		Addr:         *addr,
		OpenBrowser:  *openBrowser,
		SessionName:  *sessionName,
		Target:       target,
		PollInterval: *pollInterval,
		SavePath:     *savePath,
		UIPath:       uiPathResolved,
		Stdout:       stdout,
		Stderr:       stderr,
	})
}

func collectCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope collect [flags]\n\n")
		_, _ = fmt.Fprintf(stderr, "Load demo data and serve the UI.\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser to the UI")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		return err
	}

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	return serveCaptureSession(ctx, *addr, "collector", "collector", capture, stdout, *openBrowser, uiPathResolved)
}

func uiCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope ui [flags]\n\n")
		_, _ = fmt.Fprintf(stderr, "Load demo data and serve the UI.\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser to the UI")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		return err
	}

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	return serveCaptureSession(ctx, *addr, "ui-demo", "demo://ui", capture, stdout, *openBrowser, uiPathResolved)
}

func replayCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope replay [flags] <capture-file>\n\n")
		_, _ = fmt.Fprintf(stderr, "Load a capture file and serve the UI. Supports .gtrace (JSON) and raw Go trace\n")
		_, _ = fmt.Fprintf(stderr, "(e.g. from go test -trace=file.out). Without agent: go test -trace=out ./pkg && goroscope replay out\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser to the UI")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	target := "./captures/sample.gtrace"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	capture, err := tracebridge.LoadCaptureFromPath(ctx, target)
	if err != nil {
		return err
	}

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	return serveCaptureSession(ctx, *addr, "replay", target, capture, stdout, *openBrowser, uiPathResolved)
}

func checkCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope check [flags] <capture-file>\n\n")
		_, _ = fmt.Fprintf(stderr, "Load a .gtrace capture, run deadlock analysis, and exit with code 1 if\n")
		_, _ = fmt.Fprintf(stderr, "potential deadlocks are found.\n\n")
		_, _ = fmt.Fprintf(stderr, "Example (CI pipeline):\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope run -save out.gtrace ./tests\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope check out.gtrace\n\n")
		fs.PrintDefaults()
	}

	format := fs.String("format", "text", "Output format: text | json | github | dot")
	dotOut := fs.String("dot-out", "", "Write the wait-for graph as a DOT file (Graphviz); - for stdout")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("missing capture file; usage: goroscope check <capture-file>")
	}
	target := fs.Arg(0)

	capture, err := tracebridge.LoadCaptureFromPath(ctx, target)
	if err != nil {
		return err
	}

	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession("check", target)
	engine.LoadCapture(current, tracebridge.BindCaptureSession(capture, current.ID))

	edges := engine.ResourceGraph()
	goroutines := engine.ListGoroutines()
	if len(edges) == 0 {
		edges = analysis.DeriveResourceEdgesFromTimeline(engine.Timeline(), goroutines)
	}

	hints := analysis.FindDeadlockHints(edges, goroutines)
	report := analysis.BuildDeadlockReport(analysis.BuildDeadlockReportInput{
		Hints:      hints,
		Goroutines: goroutines,
	})

	// buildWFG is a lazy helper that computes the wait-for graph at most once,
	// avoiding a duplicate O(E+G) pass when both --dot-out and --format=dot are set.
	var cachedWFG *analysis.WaitForGraph
	buildWFG := func() analysis.WaitForGraph {
		if cachedWFG == nil {
			wfg := analysis.BuildWaitForGraph(analysis.BuildWaitForGraphInput{
				Edges:      edges,
				Goroutines: goroutines,
			})
			cachedWFG = &wfg
		}
		return *cachedWFG
	}

	// Optionally write the wait-for graph as DOT.
	if *dotOut != "" {
		wfg := buildWFG()
		dotWriter := stdout
		if *dotOut != "-" {
			f, ferr := openForWrite(*dotOut)
			if ferr != nil {
				return fmt.Errorf("open dot-out %s: %w", *dotOut, ferr)
			}
			defer f.Close()
			dotWriter = f
		}
		wfg.WriteDOT(dotWriter)
		if *dotOut != "-" {
			_, _ = fmt.Fprintf(stderr, "wait-for graph written to %s\n", *dotOut)
		}
	}

	switch *format {
	case "json":
		if werr := report.WriteJSON(stdout); werr != nil {
			return fmt.Errorf("write json: %w", werr)
		}
	case "github":
		report.WriteGitHubAnnotations(stdout)
	case "dot":
		// Convenience: --format=dot prints DOT to stdout (same as --dot-out=-)
		buildWFG().WriteDOT(stdout)
	default: // "text"
		report.WriteText(stdout)
	}

	if report.Total == 0 {
		return nil
	}
	return fmt.Errorf("deadlock hints found: %w", errDeadlockHints)
}

var errDeadlockHints = fmt.Errorf("potential deadlocks detected")

// testCaptureInput holds all parameters for runTestCapture.
type testCaptureInput struct {
	Addr        string
	OpenBrowser bool
	SavePath    string
	UIPath      string
	GoTestArgs  []string
	Stdout      io.Writer
	Stderr      io.Writer
}

func testCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope test [flags] [packages] [go-test-flags]\n\n")
		_, _ = fmt.Fprintf(stderr, "Run 'go test' with runtime tracing enabled, then open the goroscope\n")
		_, _ = fmt.Fprintf(stderr, "UI with the resulting trace. All arguments after goroscope flags are\n")
		_, _ = fmt.Fprintf(stderr, "forwarded verbatim to 'go test -trace=<tmpfile>'.\n\n")
		_, _ = fmt.Fprintf(stderr, "If tests fail, goroscope still attempts to load and serve the trace\n")
		_, _ = fmt.Fprintf(stderr, "so you can inspect the goroutine state at the time of failure.\n\n")
		_, _ = fmt.Fprintf(stderr, "Examples:\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope test ./pkg/worker -run TestWorkerPool -open-browser\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope test ./... -count=1 -save=debug.gtrace\n\n")
		fs.PrintDefaults()
	}

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	openBrowser := fs.Bool("open-browser", false, "Open the default browser to the UI")
	savePath := fs.String("save", "", "Save capture to .gtrace file after tests complete")
	ui := fs.String("ui", "vanilla", "UI to serve: vanilla (default) or react")
	uiPath := fs.String("ui-path", "web/dist", "Path to React build (when -ui=react)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	uiPathResolved := resolveUIPath(*ui, *uiPath)
	if uiPathResolved == "" && *ui == "react" {
		return fmt.Errorf("react UI not found at %q: run 'make web' first", *uiPath)
	}

	return runTestCapture(ctx, testCaptureInput{
		Addr:        *addr,
		OpenBrowser: *openBrowser,
		SavePath:    *savePath,
		UIPath:      uiPathResolved,
		GoTestArgs:  fs.Args(),
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

// runTestCapture runs 'go test -trace=<tmpfile>' with the provided arguments,
// loads the resulting trace, and serves the goroscope UI.
// If go test exits with a non-zero status, the error is logged but the trace
// is still loaded (if present) so the goroutine state at failure is visible.
func runTestCapture(ctx context.Context, in testCaptureInput) error {
	// Create a temp file to receive the runtime trace.
	traceFile, err := os.CreateTemp("", "goroscope-test-*.trace")
	if err != nil {
		return fmt.Errorf("create temp trace file: %w", err)
	}
	tracePath := traceFile.Name()
	_ = traceFile.Close()
	defer func() { _ = os.Remove(tracePath) }()

	// Inject -trace=<path> and forward all remaining args to go test.
	goArgs := append([]string{"test", "-trace=" + tracePath}, in.GoTestArgs...)
	_, _ = fmt.Fprintf(in.Stdout, "goroscope test: go %s\n", strings.Join(goArgs, " "))

	//nolint:gosec // args originate from the CLI user
	cmd := exec.CommandContext(ctx, "go", goArgs...)
	cmd.Stdout = in.Stdout
	cmd.Stderr = in.Stderr
	testErr := cmd.Run()

	if testErr != nil {
		_, _ = fmt.Fprintf(in.Stderr, "goroscope test: go test exited: %v\n", testErr)
	}

	// Check whether the trace file was written at all.
	info, statErr := os.Stat(tracePath)
	if statErr != nil || info.Size() == 0 {
		if testErr != nil {
			return fmt.Errorf("go test: %w", testErr)
		}
		return fmt.Errorf("go test did not emit a trace file")
	}

	_, _ = fmt.Fprintf(in.Stdout, "goroscope test: loading trace %s\n", tracePath)
	capture, err := tracebridge.LoadCaptureFromPath(ctx, tracePath)
	if err != nil {
		return fmt.Errorf("load test trace: %w", err)
	}

	if in.SavePath != "" {
		if err := tracebridge.SaveCaptureFile(in.SavePath, capture); err != nil {
			_, _ = fmt.Fprintf(in.Stderr, "goroscope test: save capture: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(in.Stderr, "goroscope test: saved capture to %s\n", in.SavePath)
		}
	}

	// Derive a session name from the go test arguments for display.
	sessionName := "test"
	if len(in.GoTestArgs) > 0 {
		sessionName = "test " + strings.Join(in.GoTestArgs, " ")
	}

	return serveCaptureSession(ctx, in.Addr, sessionName, tracePath, capture, in.Stdout, in.OpenBrowser, in.UIPath)
}

func exportCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "csv", "Output format: csv or json")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope export [flags] <capture-file>\n\n")
		_, _ = fmt.Fprintf(stderr, "Export timeline segments for analysis (e.g. pandas).\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("missing capture file; usage: goroscope export [flags] <capture-file>")
	}

	capture, err := tracebridge.LoadCaptureFromPath(ctx, fs.Arg(0))
	if err != nil {
		return err
	}

	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession("export", fs.Arg(0))
	engine.LoadCapture(current, tracebridge.BindCaptureSession(capture, current.ID))
	segments := engine.Timeline()

	switch *format {
	case "csv":
		return writeExportCSV(stdout, segments)
	case "json":
		return writeExportJSON(stdout, segments)
	default:
		return fmt.Errorf("unsupported format %q; use csv or json", *format)
	}
}

func writeExportCSV(w io.Writer, segments []model.TimelineSegment) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"goroutine_id", "state", "start_ns", "end_ns", "reason", "resource_id"}); err != nil {
		return err
	}
	for _, s := range segments {
		row := []string{
			strconv.FormatInt(s.GoroutineID, 10),
			string(s.State),
			strconv.FormatInt(s.StartNS, 10),
			strconv.FormatInt(s.EndNS, 10),
			string(s.Reason),
			s.ResourceID,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeExportJSON(w io.Writer, segments []model.TimelineSegment) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"segments": segments})
}

// writeCloser wraps an *os.File to satisfy io.WriteCloser.
type writeCloser struct{ *os.File }

// openForWrite creates or truncates the file at path and returns it as a
// WriteCloser.  The caller is responsible for calling Close.
func openForWrite(path string) (*writeCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &writeCloser{f}, nil
}

func resolveUIPath(ui, uiPath string) string {
	if ui != "react" {
		return ""
	}
	abs, err := filepath.Abs(uiPath)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(abs); err != nil {
		return ""
	}
	return abs
}

// serveLiveRunInput holds parameters for serveLiveRunSession.
type serveLiveRunInput struct {
	Addr         string
	OpenBrowser  bool
	SessionName  string
	Target       string
	PollInterval time.Duration
	SavePath     string
	UIPath       string
	Stdout       io.Writer
	Stderr       io.Writer
}

func serveCaptureSession(ctx context.Context, addr, sessionName, target string, capture model.Capture, stdout io.Writer, openBrowser bool, uiPath string) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(sessionName, target)
	engine.LoadCapture(current, tracebridge.BindCaptureSession(capture, current.ID))

	server := api.NewServer(addr, engine, sessions, uiPath)
	url := "http://" + addr
	_, _ = fmt.Fprintf(stdout, "goroscope scaffold serving %q at %s\n", target, url)

	if openBrowser {
		scheduleOpenBrowser(ctx, 500*time.Millisecond, url)
	}

	return server.Serve(ctx)
}

func serveLiveRunSession(ctx context.Context, in serveLiveRunInput) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(in.SessionName, in.Target)
	engine.Reset(current)

	liveRun, err := tracebridge.StartGoTargetWithTrace(ctx, in.Target, in.Stdout, in.Stderr)
	if err != nil {
		return err
	}
	defer func() { _ = liveRun.Close() }()

	go streamLiveTrace(ctx, streamLiveTraceInput{
		Target:       in.Target,
		LiveRun:      liveRun,
		Engine:       engine,
		Sessions:     sessions,
		PollInterval: in.PollInterval,
		SavePath:     in.SavePath,
		Stderr:       in.Stderr,
	})

	server := api.NewServer(in.Addr, engine, sessions, in.UIPath)
	url := "http://" + in.Addr
	_, _ = fmt.Fprintf(in.Stdout, "goroscope live run serving %q at %s\n", in.Target, url)

	if in.OpenBrowser {
		scheduleOpenBrowser(ctx, 500*time.Millisecond, url)
	}

	return server.Serve(ctx)
}

// streamLiveTraceInput holds all non-context parameters for streamLiveTrace.
type streamLiveTraceInput struct {
	Target       string
	LiveRun      *tracebridge.LiveTraceRun
	Engine       *analysis.Engine
	Sessions     *session.Manager
	PollInterval time.Duration
	SavePath     string
	Stderr       io.Writer
}

// streamLiveTrace follows a growing runtime/trace binary file and streams
// parsed events directly into the engine via the EngineWriter interface.
// It replaces the previous O(n²) watchLiveTrace (full re-read every poll tick)
// with an O(n) streaming approach: the TailReader blocks at EOF and unblocks
// as new data arrives, so the engine is updated with near-zero latency.
//
// When the target exits, streamLiveTrace drains the remaining trace data,
// applies label overrides from the .labels sidecar, and marks the session
// complete (or failed).  If SavePath is set, the final capture is written
// using a single pass over the completed trace file.
func streamLiveTrace(ctx context.Context, in streamLiveTraceInput) {
	pollDelay := in.PollInterval / 2
	if pollDelay <= 0 {
		pollDelay = 500 * time.Millisecond
	}

	tracePath := in.LiveRun.TracePath()

	f, err := tracebridge.WaitForTraceFile(ctx, tracePath, in.LiveRun.Done(), pollDelay)
	if err != nil {
		in.Sessions.FailCurrent(err.Error())
		_, _ = fmt.Fprintf(in.Stderr, "goroscope: wait for trace file: %v\n", err)
		return
	}
	defer func() { _ = f.Close() }()

	tailReader := tracebridge.NewTailReader(f, in.LiveRun.Done(), pollDelay)

	streamErr := tracebridge.StreamBinaryTrace(ctx, tracebridge.StreamBinaryTraceInput{
		Reader: tailReader,
		Writer: in.Engine,
	})

	// Apply label overrides from the agent sidecar now that the stream is done.
	if overrides, labelsErr := tracebridge.ReadLabelsFile(tracePath + ".labels"); labelsErr == nil && len(overrides) > 0 {
		in.Engine.SetLabelOverrides(overrides)
		in.Engine.Flush()
	}

	runErr := in.LiveRun.Wait()

	switch {
	case runErr != nil:
		in.Sessions.FailCurrent(runErr.Error())
		_, _ = fmt.Fprintf(in.Stderr, "goroscope: target exited with error: %v\n", runErr)
	case streamErr != nil:
		in.Sessions.FailCurrent(streamErr.Error())
		_, _ = fmt.Fprintf(in.Stderr, "goroscope: trace stream error: %v\n", streamErr)
	default:
		in.Sessions.CompleteCurrent()
		if in.SavePath != "" {
			// Re-parse the completed file for the save snapshot — single pass,
			// same result as if BuildCaptureFromRawTrace were called directly.
			saveCapture, saveErr := tracebridge.BuildCaptureFromRawTrace(ctx, tracePath)
			if saveErr != nil {
				_, _ = fmt.Fprintf(in.Stderr, "goroscope: build capture for save: %v\n", saveErr)
			} else {
				saveCapture.Target = in.Target
				if err := tracebridge.SaveCaptureFile(in.SavePath, saveCapture); err != nil {
					_, _ = fmt.Fprintf(in.Stderr, "goroscope: save capture: %v\n", err)
				} else {
					_, _ = fmt.Fprintf(in.Stderr, "goroscope: saved capture to %s\n", in.SavePath)
				}
			}
		}
	}
}
