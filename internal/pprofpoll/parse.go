// Package pprofpoll polls a running Go process via its /debug/pprof/goroutine
// endpoint and converts the text-format goroutine dump into goroscope model
// events. No changes to the target process are required — any service that
// imports net/http/pprof (or exposes the default mux) works out of the box.
package pprofpoll

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// firstHexArgRE matches the first 0x-prefixed hex argument inside a function
// call signature, e.g. "sync.runtime_SemacquireMutex(0xc000a86f34?, 0x0?)".
var firstHexArgRE = regexp.MustCompile(`\(0x([0-9a-fA-F]+)`)

// resourceFunctions maps runtime blocking function names to the resource type
// prefix used in ResourceID strings (e.g. "mutex", "chan", "rwmutex").
// Only functions where the first argument is the resource pointer are listed.
var resourceFunctions = map[string]string{
	"sync.runtime_SemacquireMutex":    "mutex",
	"sync.runtime_SemacquireRWMutex":  "rwmutex",
	"sync.runtime_SemacquireRWMutexR": "rwmutex",
	"runtime.chansend":                "chan",
	"runtime.chansend1":               "chan",
	"runtime.chanrecv":                "chan",
	"runtime.chanrecv1":               "chan",
	"runtime.chanrecv2":               "chan",
	"runtime.semacquire":              "mutex",
}

// goroutineInfo is the intermediate result of parsing one goroutine block.
type goroutineInfo struct {
	id         int64
	parentID   int64
	state      model.GoroutineState
	reason     model.BlockingReason
	resourceID string // extracted from blocking frame arguments, e.g. "mutex:0xc000a86f34"
	frames     []model.StackFrame
}

// parseGoroutineDump parses the text output of
// GET /debug/pprof/goroutine?debug=2 and returns one goroutineInfo per block.
//
// Format overview (debug=2):
//
//	goroutine 1 [running]:
//	main.main()
//		/path/to/main.go:25 +0x7c
//	...
//	created by net/http.(*Server).ListenAndServe in goroutine 1
//		/usr/local/go/src/net/http/server.go:3161 +0x56
//
//	goroutine 18 [chan receive, 2 minutes]:
//	...
func parseGoroutineDump(body string) ([]goroutineInfo, error) {
	var results []goroutineInfo
	var current *goroutineInfo

	// pendingFunc holds the function-name line waiting for its file:line pair.
	pendingFunc := ""

	flush := func() {
		if current != nil {
			current.resourceID = extractResourceID(current.frames)
			results = append(results, *current)
			current = nil
		}
		pendingFunc = ""
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()

		// Blank line terminates the current block.
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		// "goroutine N [state_info]:" — start of a new block.
		if strings.HasPrefix(line, "goroutine ") && strings.Contains(line, "[") {
			flush()
			g, err := parseGoroutineHeader(line)
			if err != nil {
				// Malformed header — skip this block.
				continue
			}
			current = &g
			continue
		}

		if current == nil {
			continue
		}

		// "created by funcname in goroutine N" — optional last line.
		if strings.HasPrefix(line, "created by ") {
			pendingFunc = ""
			parentID := parseCreatedBy(line)
			if parentID > 0 {
				current.parentID = parentID
			}
			continue
		}

		// Tab-prefixed line: either a file:line for the pending func or a "created by" file.
		if strings.HasPrefix(line, "\t") {
			if pendingFunc != "" {
				frame := parseFileLine(pendingFunc, strings.TrimPrefix(line, "\t"))
				current.frames = append(current.frames, frame)
				pendingFunc = ""
			}
			continue
		}

		// Non-tab, non-header, non-blank: it's a function-name line.
		pendingFunc = strings.TrimSpace(line)
	}
	flush()

	return results, scanner.Err()
}

// parseGoroutineHeader parses "goroutine N [state_info]:" into a goroutineInfo.
func parseGoroutineHeader(line string) (goroutineInfo, error) {
	// Strip trailing colon.
	line = strings.TrimSuffix(strings.TrimSpace(line), ":")

	// Split into "goroutine N" and "[state_info]"
	bracketOpen := strings.Index(line, "[")
	bracketClose := strings.LastIndex(line, "]")
	if bracketOpen < 0 || bracketClose < 0 || bracketClose < bracketOpen {
		return goroutineInfo{}, fmt.Errorf("malformed goroutine header: %q", line)
	}

	idPart := strings.TrimSpace(line[:bracketOpen])
	statePart := line[bracketOpen+1 : bracketClose]

	// Parse numeric ID.
	fields := strings.Fields(idPart)
	if len(fields) < 2 {
		return goroutineInfo{}, fmt.Errorf("no goroutine ID in %q", line)
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return goroutineInfo{}, fmt.Errorf("bad goroutine ID %q: %w", fields[1], err)
	}

	state, reason := classifyState(statePart)
	return goroutineInfo{id: id, state: state, reason: reason}, nil
}

// classifyState maps the pprof state string (which may include a wait duration
// suffix like ", 5 minutes") to model.GoroutineState and model.BlockingReason.
func classifyState(s string) (model.GoroutineState, model.BlockingReason) {
	// Strip optional duration suffix: "chan receive, 2 minutes" → "chan receive"
	if idx := strings.Index(s, ","); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case "running":
		return model.StateRunning, ""
	case "runnable":
		return model.StateRunnable, ""
	case "chan receive":
		return model.StateWaiting, model.ReasonChanRecv
	case "chan send":
		return model.StateWaiting, model.ReasonChanSend
	case "select":
		return model.StateWaiting, model.ReasonSelect
	case "sync.mutex.lock", "semacquire":
		return model.StateBlocked, model.ReasonMutexLock
	case "sync.rwmutex.lock":
		return model.StateBlocked, model.ReasonRWMutexLock
	case "sync.rwmutex.rlock":
		return model.StateWaiting, model.ReasonRWMutexR
	case "sync.cond.wait":
		return model.StateWaiting, model.ReasonSyncCond
	case "syscall":
		return model.StateSyscall, model.ReasonSyscall
	case "sleep":
		return model.StateWaiting, model.ReasonSleep
	case "io wait":
		return model.StateWaiting, model.ReasonUnknown
	case "gc sweep wait", "gc assist marking", "gc background marking", "gc worker (idle)":
		return model.StateWaiting, model.ReasonGCAssist
	case "dead", "":
		return model.StateDone, ""
	default:
		return model.StateWaiting, model.ReasonUnknown
	}
}

// parseCreatedBy extracts the parent goroutine ID from a "created by" line.
// Example: "created by net/http.(*Server).ListenAndServe in goroutine 1"
func parseCreatedBy(line string) int64 {
	const marker = "in goroutine "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(line[idx+len(marker):])
	// rest might be "1" or "1\t/path..." — take first token.
	token := strings.Fields(rest)
	if len(token) == 0 {
		return 0
	}
	id, err := strconv.ParseInt(token[0], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// extractResourceID scans blocking stack frames for a known runtime function
// and returns a typed resource address like "mutex:0xc000a86f34" or
// "chan:0xc000100060". Returns "" when no identifiable resource address is found.
// The Go 1.21+ "?" suffix on uncertain values is handled by the regex char class
// (it only matches hex digits, so the suffix is naturally excluded).
func extractResourceID(frames []model.StackFrame) string {
	for _, frame := range frames {
		// frame.Func may be "sync.runtime_SemacquireMutex(0xc000a86f34?, 0x0?, 0x1?)"
		funcName := frame.Func
		if idx := strings.IndexByte(funcName, '('); idx >= 0 {
			funcName = funcName[:idx]
		}
		prefix, ok := resourceFunctions[funcName]
		if !ok {
			continue
		}
		m := firstHexArgRE.FindStringSubmatch(frame.Func)
		if len(m) < 2 {
			continue
		}
		addr := "0x" + m[1]
		if addr == "0x0" {
			continue // nil pointer, not a real resource
		}
		return prefix + ":" + addr
	}
	return ""
}

// GoroutineSnapshot is a lightweight snapshot of a single goroutine suitable
// for headless alerting (watch command) without requiring a full engine.
type GoroutineSnapshot struct {
	ID         int64
	State      model.GoroutineState
	Reason     model.BlockingReason
	ResourceID string
	Frames     []model.StackFrame
}

// FetchGoroutines fetches /debug/pprof/goroutine?debug=2 from targetURL and
// returns one GoroutineSnapshot per goroutine in the dump. It is used by the
// watch command for headless alerting without starting an engine.
func FetchGoroutines(ctx context.Context, client *http.Client, targetURL string) ([]GoroutineSnapshot, error) {
	endpoint := strings.TrimRight(targetURL, "/") + "/debug/pprof/goroutine?debug=2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", endpoint, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	infos, err := parseGoroutineDump(string(data))
	if err != nil {
		return nil, err
	}
	snaps := make([]GoroutineSnapshot, len(infos))
	for i, g := range infos {
		snaps[i] = GoroutineSnapshot{
			ID:         g.id,
			State:      g.state,
			Reason:     g.reason,
			ResourceID: g.resourceID,
			Frames:     g.frames,
		}
	}
	return snaps, nil
}

// parseFileLine creates a StackFrame from a function name and a "file:line +0x…" string.
func parseFileLine(fn, fileLine string) model.StackFrame {
	// Strip PC offset suffix "+0x..."
	if idx := strings.LastIndex(fileLine, " +"); idx >= 0 {
		fileLine = fileLine[:idx]
	}
	fileLine = strings.TrimSpace(fileLine)

	// Find the last colon that precedes the line number.
	colon := strings.LastIndex(fileLine, ":")
	if colon < 0 {
		return model.StackFrame{Func: fn, File: fileLine}
	}
	file := fileLine[:colon]
	lineStr := fileLine[colon+1:]
	lineNum, _ := strconv.Atoi(lineStr)
	return model.StackFrame{Func: fn, File: file, Line: lineNum}
}
