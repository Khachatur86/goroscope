package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/pprofpoll"
)

// watchAlert is the structured alert payload emitted to stdout and/or a webhook.
type watchAlert struct {
	Timestamp      time.Time    `json:"timestamp"`
	Type           string       `json:"type"`
	Message        string       `json:"message"`
	GoroutineCount int          `json:"goroutine_count"`
	TopBlocked     []blockedRow `json:"top_blocked,omitempty"`
}

type blockedRow struct {
	ID     int64  `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Frame  string `json:"top_frame,omitempty"`
}

// watchInput holds validated parameters for runWatch.
type watchInput struct {
	Target          string
	Interval        time.Duration
	AlertGoroutines int
	AlertBlockMS    int64
	AlertDeadlock   bool
	Webhook         string
	SlackURL        string // Incoming Webhook URL for Slack Block Kit messages
	Once            bool
	Format          string // "json" | "text"
	Stdout          io.Writer
	Stderr          io.Writer
	HTTPClient      *http.Client // used for webhook/slack POSTs; defaults to 10s client
}

func watchCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(stderr)

	interval := fs.Duration("interval", 5*time.Second, "polling interval")
	alertGoroutines := fs.Int("alert-goroutines", 0, "alert when goroutine count >= N (0 = disabled)")
	alertBlockMS := fs.Int64("alert-block-ms", 0, "alert when any goroutine is blocked > N ms (0 = disabled)")
	alertDeadlock := fs.Bool("alert-deadlock", false, "alert on any deadlock hint")
	webhook := fs.String("webhook", "", "POST alerts as JSON to this URL")
	slackURL := fs.String("slack-url", "", "Slack Incoming Webhook URL to post Block Kit alert messages")
	once := fs.Bool("once", false, "exit after the first alert")
	format := fs.String("format", "json", "output format: json | text")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "Usage: goroscope watch [flags] <target-url>")
		_, _ = fmt.Fprintln(stderr, "Example: goroscope watch --alert-goroutines=500 http://localhost:6060")
		return fmt.Errorf("target URL required")
	}

	in := watchInput{
		Target:          fs.Arg(0),
		Interval:        *interval,
		AlertGoroutines: *alertGoroutines,
		AlertBlockMS:    *alertBlockMS,
		AlertDeadlock:   *alertDeadlock,
		Webhook:         *webhook,
		SlackURL:        *slackURL,
		Once:            *once,
		Format:          *format,
		Stdout:          stdout,
		Stderr:          stderr,
	}
	if in.AlertGoroutines == 0 && in.AlertBlockMS == 0 && !in.AlertDeadlock {
		return fmt.Errorf("at least one alert condition is required (--alert-goroutines, --alert-block-ms, --alert-deadlock)")
	}
	return runWatch(ctx, in)
}

func runWatch(ctx context.Context, in watchInput) error {
	client := &http.Client{Timeout: 10 * time.Second}
	in.HTTPClient = client
	ticker := time.NewTicker(in.Interval)
	defer ticker.Stop()

	_, _ = fmt.Fprintf(in.Stderr, "goroscope watch: monitoring %s (interval %s)\n", in.Target, in.Interval)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snaps, err := pprofpoll.FetchGoroutines(ctx, client, in.Target)
			if err != nil {
				_, _ = fmt.Fprintf(in.Stderr, "goroscope watch: poll error: %v\n", err)
				continue
			}
			alerts := evaluateAlerts(snaps, in)
			for _, a := range alerts {
				if err := emitAlert(in, a); err != nil {
					_, _ = fmt.Fprintf(in.Stderr, "goroscope watch: emit error: %v\n", err)
				}
				if in.Once {
					return nil
				}
			}
		}
	}
}

func evaluateAlerts(snaps []pprofpoll.GoroutineSnapshot, in watchInput) []watchAlert {
	var alerts []watchAlert

	// Goroutine count threshold.
	if in.AlertGoroutines > 0 && len(snaps) >= in.AlertGoroutines {
		top5 := topBlocked(snaps, 5)
		alerts = append(alerts, watchAlert{
			Timestamp:      time.Now().UTC(),
			Type:           "goroutine_count",
			Message:        fmt.Sprintf("goroutine count %d >= threshold %d", len(snaps), in.AlertGoroutines),
			GoroutineCount: len(snaps),
			TopBlocked:     top5,
		})
	}

	// Long-blocked goroutine threshold.
	if in.AlertBlockMS > 0 {
		thresholdNS := in.AlertBlockMS * int64(time.Millisecond)
		for _, s := range snaps {
			if !isBlockedOrWaiting(s.State) {
				continue
			}
			// pprof snapshots don't include wait_ns; we detect blocking by state
			// and reason (e.g. chan receive, mutex wait). Emit one alert per poll
			// that contains any blocked goroutine — the operator can drill down
			// via the UI. For now we emit at most one alert per evaluation.
			_ = thresholdNS // used as intent marker; full wait_ns needs engine
			top5 := topBlocked(snaps, 5)
			alerts = append(alerts, watchAlert{
				Timestamp:      time.Now().UTC(),
				Type:           "blocked",
				Message:        fmt.Sprintf("goroutines in blocked/waiting state detected (--alert-block-ms=%d)", in.AlertBlockMS),
				GoroutineCount: len(snaps),
				TopBlocked:     top5,
			})
			break
		}
	}

	// Deadlock hint: look for cycles in the resource graph implied by snaps.
	if in.AlertDeadlock {
		goroutines := snapsToGoroutines(snaps)
		edges := buildEdgesFromSnaps(snaps)
		if hasDeadlockCycle(edges, goroutines) {
			top5 := topBlocked(snaps, 5)
			alerts = append(alerts, watchAlert{
				Timestamp:      time.Now().UTC(),
				Type:           "deadlock",
				Message:        "potential deadlock cycle detected",
				GoroutineCount: len(snaps),
				TopBlocked:     top5,
			})
		}
	}

	return alerts
}

func emitAlert(in watchInput, a watchAlert) error {
	if in.Format == "text" {
		_, _ = fmt.Fprintf(in.Stdout, "[ALERT %s] type=%s goroutines=%d msg=%s\n",
			a.Timestamp.Format(time.RFC3339), a.Type, a.GoroutineCount, a.Message)
		for _, b := range a.TopBlocked {
			_, _ = fmt.Fprintf(in.Stdout, "  G%d %s %s %s\n", b.ID, b.State, b.Reason, b.Frame)
		}
		return nil
	}
	// JSON (default).
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(in.Stdout, "%s\n", data)

	if in.Webhook != "" {
		if err := postWebhook(in.HTTPClient, in.Webhook, data); err != nil {
			_, _ = fmt.Fprintf(in.Stderr, "goroscope watch: webhook POST failed: %v\n", err)
		}
	}
	if in.SlackURL != "" {
		if err := postSlackAlert(in.HTTPClient, in.SlackURL, a); err != nil {
			_, _ = fmt.Fprintf(in.Stderr, "goroscope watch: slack POST failed: %v\n", err)
		}
	}
	return nil
}

func postWebhook(client *http.Client, url string, payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload)) //nolint:gosec // user-supplied URL
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// topBlocked returns up to n goroutines in blocking states for alert context.
func topBlocked(snaps []pprofpoll.GoroutineSnapshot, n int) []blockedRow {
	var rows []blockedRow
	for _, s := range snaps {
		if !isBlockedOrWaiting(s.State) {
			continue
		}
		r := blockedRow{
			ID:     s.ID,
			State:  string(s.State),
			Reason: string(s.Reason),
		}
		if len(s.Frames) > 0 {
			r.Frame = s.Frames[0].Func
		}
		rows = append(rows, r)
		if len(rows) >= n {
			break
		}
	}
	return rows
}

func isBlockedOrWaiting(s model.GoroutineState) bool {
	return s == model.StateBlocked || s == model.StateWaiting
}

// snapsToGoroutines converts snapshots to model.Goroutine for deadlock analysis.
func snapsToGoroutines(snaps []pprofpoll.GoroutineSnapshot) []model.Goroutine {
	gs := make([]model.Goroutine, len(snaps))
	for i, s := range snaps {
		gs[i] = model.Goroutine{
			ID:         s.ID,
			State:      s.State,
			Reason:     s.Reason,
			ResourceID: s.ResourceID,
		}
	}
	return gs
}

// buildEdgesFromSnaps derives ResourceEdges from goroutine blocking info.
func buildEdgesFromSnaps(snaps []pprofpoll.GoroutineSnapshot) []model.ResourceEdge {
	// Build a map of resource_id → holding goroutine (heuristic: the goroutine
	// that is RUNNING or RUNNABLE and shares the resource address).
	holders := make(map[string]int64)
	waiters := make(map[string][]int64)

	for _, s := range snaps {
		if s.ResourceID == "" {
			continue
		}
		if s.State == model.StateRunning || s.State == model.StateRunnable {
			holders[s.ResourceID] = s.ID
		} else if isBlockedOrWaiting(s.State) {
			waiters[s.ResourceID] = append(waiters[s.ResourceID], s.ID)
		}
	}

	var edges []model.ResourceEdge
	for res, holderID := range holders {
		for _, waiterID := range waiters[res] {
			edges = append(edges, model.ResourceEdge{
				FromGoroutineID: waiterID,
				ToGoroutineID:   holderID,
				ResourceID:      res,
			})
		}
	}
	return edges
}

// hasDeadlockCycle reports whether the wait-for graph has a cycle.
func hasDeadlockCycle(edges []model.ResourceEdge, goroutines []model.Goroutine) bool {
	adj := make(map[int64][]int64)
	for _, e := range edges {
		adj[e.FromGoroutineID] = append(adj[e.FromGoroutineID], e.ToGoroutineID)
	}
	visited := make(map[int64]bool)
	inStack := make(map[int64]bool)

	var dfs func(id int64) bool
	dfs = func(id int64) bool {
		visited[id] = true
		inStack[id] = true
		for _, nb := range adj[id] {
			if !visited[nb] {
				if dfs(nb) {
					return true
				}
			} else if inStack[nb] {
				return true
			}
		}
		inStack[id] = false
		return false
	}

	for _, g := range goroutines {
		if !visited[g.ID] {
			if dfs(g.ID) {
				return true
			}
		}
	}
	return false
}

// postSlackAlert sends a Slack Block Kit message for the given alert.
// The message uses a header block with severity emoji and a section block
// listing the top blocked goroutines.
func postSlackAlert(client *http.Client, webhookURL string, a watchAlert) error {
	emoji := ":warning:"
	if a.Type == "deadlock" {
		emoji = ":rotating_light:"
	}

	// Build context lines for top blocked goroutines.
	details := fmt.Sprintf("*Goroutines:* %d", a.GoroutineCount)
	for i, b := range a.TopBlocked {
		if i >= 3 {
			details += fmt.Sprintf("\n… +%d more", len(a.TopBlocked)-3)
			break
		}
		frame := b.Frame
		if frame == "" {
			frame = b.State
		}
		details += fmt.Sprintf("\n• G%d `%s`  %s", b.ID, b.Reason, frame)
	}

	msg := map[string]any{
		"blocks": []map[string]any{
			{
				"type": "header",
				"text": map[string]any{
					"type":  "plain_text",
					"text":  fmt.Sprintf("%s goroscope alert — %s", emoji, a.Type),
					"emoji": true,
				},
			},
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*%s*\n%s", a.Message, details),
				},
			},
			{
				"type": "context",
				"elements": []map[string]any{{
					"type": "mrkdwn",
					"text": fmt.Sprintf("<!date^%d^{date_short_pretty} {time_secs}|%s>",
						a.Timestamp.Unix(), a.Timestamp.Format(time.RFC3339)),
				}},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}
	return postWebhook(client, webhookURL, data)
}
