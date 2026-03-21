// Package pprofpoll polls a running Go process via its /debug/pprof/goroutine
// endpoint and converts the text-format goroutine dump into goroscope model
// events. No changes to the target process are required — any service that
// imports net/http/pprof (or exposes the default mux) works out of the box.
package pprofpoll

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/Khachatur86/goroscope/internal/model"
)

// goroutineInfo is the intermediate result of parsing one goroutine block.
type goroutineInfo struct {
	id       int64
	parentID int64
	state    model.GoroutineState
	reason   model.BlockingReason
	frames   []model.StackFrame
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
