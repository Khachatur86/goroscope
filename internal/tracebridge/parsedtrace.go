package tracebridge

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

var (
	syncLineRE        = regexp.MustCompile(`Sync Time=(\d+).*Wall=([0-9T:\-+:.Z]+)`)
	stateTransitionRE = regexp.MustCompile(`Time=(\d+)\s+Resource=Goroutine\((\d+)\)\s+Reason="([^"]*)"(?:\s+GoID=\d+)?\s+([A-Za-z]+)->([A-Za-z]+)$`)
	workspaceRoot     = mustGetwd()
)

type parsedTransition struct {
	TimeNS int64
	GoID   int64
	Reason string
	From   string
	To     string
	Stack  []model.StackFrame
}

type parsedTraceBuilder struct {
	baseTime      int64
	baseWall      time.Time
	eventSeq      uint64
	stackSeq      uint64
	capture       model.Capture
	keptGoroutine map[int64]bool
}

func BuildCaptureFromRawTrace(ctx context.Context, tracePath string) (model.Capture, error) {
	cmd := exec.CommandContext(ctx, "go", "tool", "trace", "-d=parsed", tracePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return model.Capture{}, fmt.Errorf("parse raw trace with go tool trace: %w\n%s", err, string(output))
	}

	return ParseParsedTrace(bytes.NewReader(output))
}

func RunGoTargetWithTrace(ctx context.Context, target string, stdout, stderr io.Writer) (model.Capture, error) {
	tempDir, err := os.MkdirTemp("", "goroscope-trace-*")
	if err != nil {
		return model.Capture{}, fmt.Errorf("create temp trace dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	tracePath := filepath.Join(tempDir, "trace.out")
	cmd := exec.CommandContext(ctx, "go", "run", target)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "GOROSCOPE_TRACE_FILE="+tracePath)

	if err := cmd.Run(); err != nil {
		return model.Capture{}, fmt.Errorf("run target %q: %w", target, err)
	}

	info, err := os.Stat(tracePath)
	if err != nil || info.Size() == 0 {
		return model.Capture{}, fmt.Errorf("target %q did not emit a runtime trace; import github.com/Khachatur86/goroscope/agent and call agent.StartFromEnv() in main", target)
	}

	return BuildCaptureFromRawTrace(ctx, tracePath)
}

func ParseParsedTrace(r io.Reader) (model.Capture, error) {
	builder := parsedTraceBuilder{
		capture: model.Capture{
			Name: "runtime-trace",
		},
		keptGoroutine: make(map[int64]bool),
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var current *parsedTransition
	collectingStack := false
	pendingFunc := ""

	flushCurrent := func() error {
		if current == nil {
			return nil
		}
		if err := builder.appendTransition(*current); err != nil {
			return err
		}
		current = nil
		collectingStack = false
		pendingFunc = ""
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		if builder.baseWall.IsZero() {
			if match := syncLineRE.FindStringSubmatch(line); match != nil {
				baseTime, err := strconv.ParseInt(match[1], 10, 64)
				if err != nil {
					return model.Capture{}, fmt.Errorf("parse sync time: %w", err)
				}
				baseWall, err := time.Parse(time.RFC3339, match[2])
				if err != nil {
					return model.Capture{}, fmt.Errorf("parse sync wall time: %w", err)
				}
				builder.baseTime = baseTime
				builder.baseWall = baseWall
			}
		}

		if match := stateTransitionRE.FindStringSubmatch(line); match != nil {
			if err := flushCurrent(); err != nil {
				return model.Capture{}, err
			}

			timeNS, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return model.Capture{}, fmt.Errorf("parse transition time: %w", err)
			}
			goID, err := strconv.ParseInt(match[2], 10, 64)
			if err != nil {
				return model.Capture{}, fmt.Errorf("parse goroutine id: %w", err)
			}

			current = &parsedTransition{
				TimeNS: timeNS,
				GoID:   goID,
				Reason: match[3],
				From:   match[4],
				To:     match[5],
			}
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case line == "Stack=":
			collectingStack = true
			pendingFunc = ""
		case line == "TransitionStack=":
			collectingStack = false
			pendingFunc = ""
		case strings.TrimSpace(line) == "":
			if collectingStack {
				collectingStack = false
				pendingFunc = ""
			}
		case collectingStack:
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.Contains(trimmed, " @ "):
				pendingFunc = strings.SplitN(trimmed, " @ ", 2)[0]
			case pendingFunc != "" && strings.HasPrefix(trimmed, "/"):
				file, lineNumber := parseStackLocation(trimmed)
				current.Stack = append(current.Stack, model.StackFrame{
					Func: pendingFunc,
					File: file,
					Line: lineNumber,
				})
				pendingFunc = ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return model.Capture{}, fmt.Errorf("scan parsed trace: %w", err)
	}
	if err := flushCurrent(); err != nil {
		return model.Capture{}, err
	}
	builder.capture = filterCaptureToRelevantGoroutines(builder.capture)
	if len(builder.capture.Events) == 0 {
		return model.Capture{}, fmt.Errorf("parsed trace produced no goroutine events")
	}

	return builder.capture, nil
}

func (b *parsedTraceBuilder) appendTransition(transition parsedTransition) error {
	if b.baseWall.IsZero() {
		return fmt.Errorf("parsed trace is missing sync line")
	}

	keep := b.keptGoroutine[transition.GoID] || hasUserFrame(transition.Stack)
	if !keep {
		return nil
	}
	b.keptGoroutine[transition.GoID] = true

	timestamp := b.baseWall.Add(time.Duration(transition.TimeNS - b.baseTime))
	label := primaryFunction(transition.Stack)
	labels := model.Labels{}
	if label != "" {
		labels["function"] = label
	}

	if transition.To == "NotExist" {
		b.eventSeq++
		b.capture.Events = append(b.capture.Events, model.Event{
			Seq:         b.eventSeq,
			Timestamp:   timestamp,
			Kind:        model.EventKindGoroutineEnd,
			GoroutineID: transition.GoID,
			Labels:      labels,
		})
		return nil
	}

	state, reason := mapTraceState(transition.To, transition.Reason)
	kind := model.EventKindGoroutineState
	if transition.From == "NotExist" || transition.From == "Undetermined" {
		kind = model.EventKindGoroutineCreate
	}

	b.eventSeq++
	b.capture.Events = append(b.capture.Events, model.Event{
		Seq:         b.eventSeq,
		Timestamp:   timestamp,
		Kind:        kind,
		GoroutineID: transition.GoID,
		State:       state,
		Reason:      reason,
		Labels:      labels,
	})

	if len(transition.Stack) > 0 {
		b.stackSeq++
		b.capture.Stacks = append(b.capture.Stacks, model.StackSnapshot{
			Seq:         b.stackSeq,
			Timestamp:   timestamp,
			StackID:     fmt.Sprintf("trace_stack_%d", b.stackSeq),
			GoroutineID: transition.GoID,
			Frames:      append([]model.StackFrame(nil), transition.Stack...),
		})
	}

	return nil
}

func mapTraceState(toState, reason string) (model.GoroutineState, model.BlockingReason) {
	switch toState {
	case "Running":
		return model.StateRunning, ""
	case "Runnable":
		return model.StateRunnable, ""
	case "Syscall":
		return model.StateSyscall, model.ReasonSyscall
	case "Waiting":
		return mapWaitingReason(reason)
	default:
		return model.StateWaiting, model.ReasonUnknown
	}
}

func mapWaitingReason(reason string) (model.GoroutineState, model.BlockingReason) {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "chan receive"):
		return model.StateBlocked, model.ReasonChanRecv
	case strings.Contains(lower, "chan send"):
		return model.StateBlocked, model.ReasonChanSend
	case strings.Contains(lower, "select"):
		return model.StateBlocked, model.ReasonSelect
	case strings.Contains(lower, "sync"):
		return model.StateBlocked, model.ReasonMutexLock
	case strings.Contains(lower, "sleep"):
		return model.StateWaiting, model.ReasonSleep
	case strings.Contains(lower, "syscall"):
		return model.StateSyscall, model.ReasonSyscall
	default:
		return model.StateWaiting, model.ReasonUnknown
	}
}

func parseStackLocation(raw string) (string, int) {
	idx := strings.LastIndex(raw, ":")
	if idx == -1 {
		return raw, 0
	}

	lineNumber, err := strconv.Atoi(raw[idx+1:])
	if err != nil {
		return raw, 0
	}

	return raw[:idx], lineNumber
}

func hasUserFrame(frames []model.StackFrame) bool {
	for _, frame := range frames {
		if isRelevantFrame(frame) {
			return true
		}
	}
	return false
}

func primaryFunction(frames []model.StackFrame) string {
	for _, frame := range frames {
		if isRelevantFrame(frame) {
			return frame.Func
		}
	}
	if len(frames) > 0 {
		return frames[0].Func
	}
	return ""
}

func isUserFunction(function string) bool {
	return function != "" &&
		!strings.HasPrefix(function, "runtime.") &&
		!strings.HasPrefix(function, "internal/") &&
		!strings.HasPrefix(function, "syscall.") &&
		!strings.HasPrefix(function, "runtime/")
}

func isRelevantFrame(frame model.StackFrame) bool {
	if frame.File != "" {
		cleanFile := filepath.Clean(frame.File)
		if workspaceRoot != "" && strings.HasPrefix(cleanFile, workspaceRoot+string(os.PathSeparator)) {
			return true
		}

		gorootSrc := filepath.Clean(filepath.Join(runtime.GOROOT(), "src")) + string(os.PathSeparator)
		if strings.HasPrefix(cleanFile, gorootSrc) {
			return false
		}
		if cleanFile != "" {
			return true
		}
	}

	return isUserFunction(frame.Func)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(wd)
}

func filterCaptureToRelevantGoroutines(capture model.Capture) model.Capture {
	latest := make(map[int64]model.StackSnapshot)
	for _, snapshot := range capture.Stacks {
		current, ok := latest[snapshot.GoroutineID]
		if !ok || snapshot.Timestamp.After(current.Timestamp) {
			latest[snapshot.GoroutineID] = snapshot
		}
	}

	keep := make(map[int64]bool)
	for goroutineID, snapshot := range latest {
		if goroutineID == 1 || hasUserFrame(snapshot.Frames) {
			keep[goroutineID] = true
		}
	}

	if len(keep) == 0 {
		return capture
	}

	filtered := model.Capture{
		Name:   capture.Name,
		Target: capture.Target,
	}

	for _, event := range capture.Events {
		if keep[event.GoroutineID] {
			filtered.Events = append(filtered.Events, event)
		}
	}
	for _, snapshot := range capture.Stacks {
		if keep[snapshot.GoroutineID] {
			filtered.Stacks = append(filtered.Stacks, snapshot)
		}
	}
	for _, edge := range capture.Resources {
		if keep[edge.FromGoroutineID] || keep[edge.ToGoroutineID] {
			filtered.Resources = append(filtered.Resources, edge)
		}
	}

	return filtered
}
