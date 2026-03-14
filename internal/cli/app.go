package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/api"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "run":
		return runCommand(ctx, args[1:], stdout, stderr)
	case "collect":
		return collectCommand(ctx, args[1:], stdout, stderr)
	case "ui":
		return uiCommand(ctx, args[1:], stdout, stderr)
	case "replay":
		return replayCommand(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  goroscope run [--addr 127.0.0.1:7070] [--session-name name] <package-or-binary>")
	fmt.Fprintln(w, "  goroscope collect [--addr 127.0.0.1:7070]")
	fmt.Fprintln(w, "  goroscope ui [--addr 127.0.0.1:7070]")
	fmt.Fprintln(w, "  goroscope replay [--addr 127.0.0.1:7070] <capture-file>")
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	sessionName := fs.String("session-name", "local-run", "Session name")
	noBrowser := fs.Bool("no-browser", true, "Reserved for future browser integration")

	if err := fs.Parse(args); err != nil {
		return err
	}

	target := "./app"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	_ = noBrowser

	return serveLiveRunSession(ctx, *addr, *sessionName, target, stdout, stderr)
}

func collectCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		return err
	}

	return serveCaptureSession(ctx, *addr, "collector", "collector", capture, stdout)
}

func uiCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	capture, err := tracebridge.LoadDemoCapture()
	if err != nil {
		return err
	}

	return serveCaptureSession(ctx, *addr, "ui-demo", "demo://ui", capture, stdout)
}

func replayCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target := "./captures/sample.gtrace"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	capture, err := tracebridge.LoadCaptureFile(target)
	if err != nil {
		return err
	}

	return serveCaptureSession(ctx, *addr, "replay", target, capture, stdout)
}

func serveCaptureSession(ctx context.Context, addr, sessionName, target string, capture model.Capture, stdout io.Writer) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(sessionName, target)
	engine.LoadCapture(current, tracebridge.BindCaptureSession(capture, current.ID))

	server := api.NewServer(addr, engine, sessions)
	fmt.Fprintf(stdout, "goroscope scaffold serving %q at http://%s\n", target, addr)

	return server.Serve(ctx)
}

func serveLiveRunSession(ctx context.Context, addr, sessionName, target string, stdout, stderr io.Writer) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(sessionName, target)
	engine.Reset(current)

	liveRun, err := tracebridge.StartGoTargetWithTrace(ctx, target, stdout, stderr)
	if err != nil {
		return err
	}
	defer liveRun.Close()

	go watchLiveTrace(ctx, current.ID, liveRun, engine, sessions, stderr)

	server := api.NewServer(addr, engine, sessions)
	fmt.Fprintf(stdout, "goroscope live run serving %q at http://%s\n", target, addr)

	return server.Serve(ctx)
}

func watchLiveTrace(
	ctx context.Context,
	sessionID string,
	liveRun *tracebridge.LiveTraceRun,
	engine *analysis.Engine,
	sessions *session.Manager,
	stderr io.Writer,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var lastSize int64 = -1

	refreshCapture := func(final bool) error {
		size, err := liveRun.TraceSize()
		if err != nil {
			if os.IsNotExist(err) && !final {
				return nil
			}
			if os.IsNotExist(err) && final {
				return fmt.Errorf("target did not emit a runtime trace; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
			}
			return err
		}
		if size == 0 {
			if final {
				return fmt.Errorf("target did not emit a runtime trace; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
			}
			return nil
		}
		if !final && size == lastSize {
			return nil
		}

		capture, err := liveRun.BuildCapture(ctx)
		if err != nil {
			if final {
				return err
			}
			return nil
		}

		sessionState := sessions.Current()
		if sessionState == nil {
			return nil
		}

		engine.LoadCapture(sessionState, tracebridge.BindCaptureSession(capture, sessionID))
		lastSize = size
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := refreshCapture(false); err != nil {
				fmt.Fprintf(stderr, "goroscope: refresh live trace: %v\n", err)
			}
		case <-liveRun.Done():
			runErr := liveRun.Wait()
			refreshErr := refreshCapture(true)

			switch {
			case runErr != nil:
				sessions.FailCurrent(runErr.Error())
				fmt.Fprintf(stderr, "goroscope: target exited with error: %v\n", runErr)
			case refreshErr != nil:
				sessions.FailCurrent(refreshErr.Error())
				fmt.Fprintf(stderr, "goroscope: finalize trace capture: %v\n", refreshErr)
			default:
				sessions.CompleteCurrent()
			}
			return
		}
	}
}
