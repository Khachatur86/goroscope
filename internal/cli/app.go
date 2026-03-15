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
	fmt.Fprintln(w, "  goroscope run [--addr 127.0.0.1:7070] [--session-name name] [--poll-interval 1s] [--save path.gtrace] <package-or-binary>")
	fmt.Fprintln(w, "  goroscope collect [--addr 127.0.0.1:7070]")
	fmt.Fprintln(w, "  goroscope ui [--addr 127.0.0.1:7070]")
	fmt.Fprintln(w, "  goroscope replay [--addr 127.0.0.1:7070] <capture-file>")
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	sessionName := fs.String("session-name", "local-run", "Session name")
	pollInterval := fs.Duration("poll-interval", time.Second, "How often to re-read the live trace file")
	savePath := fs.String("save", "", "Save capture to file when session completes (e.g. ./captures/run.gtrace)")
	noBrowser := fs.Bool("no-browser", true, "Reserved for future browser integration")

	if err := fs.Parse(args); err != nil {
		return err
	}

	target := "./app"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	_ = noBrowser

	return serveLiveRunSession(ctx, serveLiveRunInput{
		Addr:         *addr,
		SessionName:  *sessionName,
		Target:       target,
		PollInterval: *pollInterval,
		SavePath:     *savePath,
		Stdout:       stdout,
		Stderr:       stderr,
	})
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

// serveLiveRunInput holds parameters for serveLiveRunSession.
type serveLiveRunInput struct {
	Addr         string
	SessionName  string
	Target       string
	PollInterval time.Duration
	SavePath     string
	Stdout       io.Writer
	Stderr       io.Writer
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

func serveLiveRunSession(ctx context.Context, in serveLiveRunInput) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(in.SessionName, in.Target)
	engine.Reset(current)

	liveRun, err := tracebridge.StartGoTargetWithTrace(ctx, in.Target, in.Stdout, in.Stderr)
	if err != nil {
		return err
	}
	defer liveRun.Close()

	go watchLiveTrace(ctx, watchLiveTraceInput{
		SessionID:    current.ID,
		Target:       in.Target,
		LiveRun:      liveRun,
		Engine:       engine,
		Sessions:     sessions,
		PollInterval: in.PollInterval,
		SavePath:     in.SavePath,
		Stderr:       in.Stderr,
	})

	server := api.NewServer(in.Addr, engine, sessions)
	fmt.Fprintf(in.Stdout, "goroscope live run serving %q at http://%s\n", in.Target, in.Addr)

	return server.Serve(ctx)
}

// watchLiveTraceInput holds all non-context parameters for watchLiveTrace.
type watchLiveTraceInput struct {
	SessionID    string
	Target       string
	LiveRun      *tracebridge.LiveTraceRun
	Engine       *analysis.Engine
	Sessions     *session.Manager
	PollInterval time.Duration
	SavePath     string
	Stderr       io.Writer
}

func watchLiveTrace(ctx context.Context, in watchLiveTraceInput) {
	pollInterval := in.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastSize int64 = -1

	refreshCapture := func(final bool) (model.Capture, error) {
		size, err := in.LiveRun.TraceSize()
		if err != nil {
			if os.IsNotExist(err) && !final {
				return model.Capture{}, nil
			}
			if os.IsNotExist(err) && final {
				return model.Capture{}, fmt.Errorf("target did not emit a runtime trace; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
			}
			return model.Capture{}, err
		}
		if size == 0 {
			if final {
				return model.Capture{}, fmt.Errorf("target did not emit a runtime trace; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main")
			}
			return model.Capture{}, nil
		}
		if !final && size == lastSize {
			return model.Capture{}, nil
		}

		capture, err := in.LiveRun.BuildCapture(ctx)
		if err != nil {
			if final {
				return model.Capture{}, err
			}
			return model.Capture{}, nil
		}

		sessionState := in.Sessions.Current()
		if sessionState == nil {
			return model.Capture{}, nil
		}

		in.Engine.LoadCapture(sessionState, tracebridge.BindCaptureSession(capture, in.SessionID))
		lastSize = size

		if final {
			capture.Target = in.Target
			return capture, nil
		}
		return model.Capture{}, nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := refreshCapture(false); err != nil {
				fmt.Fprintf(in.Stderr, "goroscope: refresh live trace: %v\n", err)
			}
		case <-in.LiveRun.Done():
			runErr := in.LiveRun.Wait()
			finalCapture, refreshErr := refreshCapture(true)

			switch {
			case runErr != nil:
				in.Sessions.FailCurrent(runErr.Error())
				fmt.Fprintf(in.Stderr, "goroscope: target exited with error: %v\n", runErr)
			case refreshErr != nil:
				in.Sessions.FailCurrent(refreshErr.Error())
				fmt.Fprintf(in.Stderr, "goroscope: finalize trace capture: %v\n", refreshErr)
			default:
				in.Sessions.CompleteCurrent()
				if in.SavePath != "" && len(finalCapture.Events) > 0 {
					if err := tracebridge.SaveCaptureFile(in.SavePath, finalCapture); err != nil {
						fmt.Fprintf(in.Stderr, "goroscope: save capture: %v\n", err)
					} else {
						fmt.Fprintf(in.Stderr, "goroscope: saved capture to %s\n", in.SavePath)
					}
				}
			}
			return
		}
	}
}
