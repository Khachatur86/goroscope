package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/pprofpoll"
)

// topInput holds validated parameters for runTop.
type topInput struct {
	Target   string
	Interval time.Duration
	N        int  // number of goroutines to display
	Once     bool // print once and exit (non-interactive / CI mode)
	Stdout   io.Writer
	Stderr   io.Writer
}

// topRow is one display row in the live table.
type topRow struct {
	ID     int64
	State  string
	Reason string
	Frame  string
}

func topCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(stderr)

	interval := fs.Duration("interval", 2*time.Second, "refresh interval")
	n := fs.Int("n", 20, "number of goroutines to show")
	once := fs.Bool("once", false, "print one frame and exit (non-interactive)")

	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope top [flags] <target-url>")
		_, _ = fmt.Fprintln(stderr, "")
		_, _ = fmt.Fprintln(stderr, "Live goroutine monitor. Polls the pprof endpoint and displays")
		_, _ = fmt.Fprintln(stderr, "the top-N goroutines sorted by wait duration.")
		_, _ = fmt.Fprintln(stderr, "")
		_, _ = fmt.Fprintln(stderr, "Example:")
		_, _ = fmt.Fprintln(stderr, "  goroscope top http://localhost:6060")
		_, _ = fmt.Fprintln(stderr, "  goroscope top --n=50 --interval=1s http://localhost:6060")
		_, _ = fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope top [flags] <target-url>")
		return fmt.Errorf("target URL required")
	}

	return runTop(ctx, topInput{
		Target:   fs.Arg(0),
		Interval: *interval,
		N:        *n,
		Once:     *once,
		Stdout:   stdout,
		Stderr:   stderr,
	})
}

func runTop(ctx context.Context, in topInput) error {
	client := &http.Client{Timeout: 10 * time.Second}

	render := func() error {
		snaps, err := pprofpoll.FetchGoroutines(ctx, client, in.Target)
		if err != nil {
			return err
		}
		renderTopFrame(in, snaps)
		return nil
	}

	if in.Once {
		return render()
	}

	// Interactive mode: clear screen before first render, then refresh in place.
	clearScreen(in.Stdout)
	if err := render(); err != nil {
		_, _ = fmt.Fprintf(in.Stderr, "top: %v\n", err)
	}

	ticker := time.NewTicker(in.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			moveCursorHome(in.Stdout)
			if err := render(); err != nil {
				_, _ = fmt.Fprintf(in.Stderr, "top: %v\n", err)
			}
		}
	}
}

// renderTopFrame writes one full top-frame to stdout.
//
//nolint:errcheck // writes to terminal stdout; errors are intentionally ignored.
func renderTopFrame(in topInput, snaps []pprofpoll.GoroutineSnapshot) {
	now := time.Now().Format("15:04:05")

	// Aggregate state counts.
	stateCounts := make(map[model.GoroutineState]int)
	for _, s := range snaps {
		stateCounts[s.State]++
	}

	// Build and sort rows: blocked/waiting first, then by WaitNS desc, then by ID.
	rows := buildTopRows(snaps, in.N)

	// Header.
	fmt.Fprintf(in.Stdout, "\033[1mgoroscope top\033[0m  target: %s  updated: %s\n", in.Target, now)
	fmt.Fprintf(in.Stdout, "goroutines: \033[1m%d\033[0m total  ", len(snaps))
	for _, state := range []model.GoroutineState{
		model.StateRunning, model.StateRunnable, model.StateWaiting, model.StateBlocked, model.StateSyscall,
	} {
		if c := stateCounts[state]; c > 0 {
			fmt.Fprintf(in.Stdout, "%s:%d  ", string(state), c)
		}
	}
	fmt.Fprintln(in.Stdout)
	fmt.Fprintln(in.Stdout, strings.Repeat("─", 80))

	// Column headers.
	fmt.Fprintf(in.Stdout, "%-8s  %-10s  %-20s  %s\n", "GID", "STATE", "REASON", "TOP FRAME")
	fmt.Fprintln(in.Stdout, strings.Repeat("─", 80))

	// Rows.
	for _, r := range rows {
		reason := r.Reason
		if len(reason) > 20 {
			reason = reason[:17] + "..."
		}
		frame := r.Frame
		if len(frame) > 50 {
			frame = "…" + frame[len(frame)-49:]
		}
		stateColor := stateANSI(r.State)
		fmt.Fprintf(in.Stdout, "%-8d  %s%-10s\033[0m  %-20s  %s\n",
			r.ID, stateColor, r.State, reason, frame)
	}
	fmt.Fprintln(in.Stdout, strings.Repeat("─", 80))

	if len(snaps) > in.N {
		fmt.Fprintf(in.Stdout, "  … %d more goroutines (use --n to show more)\n", len(snaps)-in.N)
	}
}

// buildTopRows selects and sorts up to n rows.
// Priority: blocked/waiting first, then sorted by WaitNS desc, then by ID asc.
func buildTopRows(snaps []pprofpoll.GoroutineSnapshot, n int) []topRow {
	rows := make([]topRow, 0, len(snaps))
	for _, s := range snaps {
		frame := ""
		if len(s.Frames) > 0 {
			frame = s.Frames[0].Func
		}
		rows = append(rows, topRow{
			ID:     s.ID,
			State:  string(s.State),
			Reason: string(s.Reason),
			Frame:  frame,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		iBlocked := isBlockedState(rows[i].State)
		jBlocked := isBlockedState(rows[j].State)
		if iBlocked != jBlocked {
			return iBlocked
		}
		return rows[i].ID < rows[j].ID
	})

	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

func isBlockedState(state string) bool {
	return state == string(model.StateBlocked) || state == string(model.StateWaiting)
}

// stateANSI returns an ANSI color escape sequence for a goroutine state.
func stateANSI(state string) string {
	switch model.GoroutineState(state) {
	case model.StateRunning:
		return "\033[32m" // green
	case model.StateRunnable:
		return "\033[36m" // cyan
	case model.StateBlocked, model.StateWaiting:
		return "\033[33m" // yellow
	case model.StateSyscall:
		return "\033[35m" // magenta
	default:
		return ""
	}
}

func clearScreen(w io.Writer) {
	_, _ = fmt.Fprint(w, "\033[2J\033[H")
}

func moveCursorHome(w io.Writer) {
	_, _ = fmt.Fprint(w, "\033[H")
}
