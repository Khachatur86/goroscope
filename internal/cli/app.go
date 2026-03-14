package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/api"
	"github.com/Khachatur86/goroscope/internal/session"
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

	return serveDemoSession(ctx, *addr, *sessionName, target, stdout)
}

func collectCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return serveDemoSession(ctx, *addr, "collector", "collector", stdout)
}

func uiCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)

	addr := fs.String("addr", "127.0.0.1:7070", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return serveDemoSession(ctx, *addr, "ui-demo", "demo://ui", stdout)
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

	return serveDemoSession(ctx, *addr, "replay", target, stdout)
}

func serveDemoSession(ctx context.Context, addr, sessionName, target string, stdout io.Writer) error {
	engine := analysis.NewEngine()
	sessions := session.NewManager()
	current := sessions.StartSession(sessionName, target)
	engine.SeedDemoSession(current)

	server := api.NewServer(addr, engine, sessions)
	fmt.Fprintf(stdout, "goroscope scaffold serving %q at http://%s\n", target, addr)

	return server.Serve(ctx)
}
