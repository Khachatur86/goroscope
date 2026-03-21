package pprofpoll

import (
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

// sampleDump is a realistic excerpt of /debug/pprof/goroutine?debug=2 output.
const sampleDump = `goroutine 1 [running]:
main.main()
	/home/user/app/main.go:25 +0x7c

goroutine 18 [chan receive, 2 minutes]:
net/http.(*connReader).Read(0xc0003e8000, {0xc00076e000, 0x1000, 0x1000})
	/usr/local/go/src/net/http/server.go:789 +0x149
created by net/http.(*conn).serve in goroutine 7
	/usr/local/go/src/net/http/server.go:2040 +0x3cd

goroutine 42 [sync.Mutex.Lock]:
sync.runtime_SemacquireMutex(0xc000a86f34?, 0x0?, 0x1?)
	/usr/local/go/src/runtime/sema.go:77 +0x26
main.(*Worker).Process(0xc000b20000)
	/home/user/app/worker.go:88 +0x5c
created by main.startWorkers in goroutine 1
	/home/user/app/main.go:60 +0x98

goroutine 99 [syscall]:
syscall.Syscall(0x1, 0x5, 0xc0004e2000, 0x200)
	/usr/local/go/src/syscall/asm_linux_amd64.s:17 +0x5
`

func TestParseGoroutineDump_Count(t *testing.T) {
	t.Parallel()

	gs, err := parseGoroutineDump(sampleDump)
	if err != nil {
		t.Fatalf("parseGoroutineDump: %v", err)
	}
	if len(gs) != 4 {
		t.Fatalf("want 4 goroutines, got %d", len(gs))
	}
}

func TestParseGoroutineDump_States(t *testing.T) {
	t.Parallel()

	gs, _ := parseGoroutineDump(sampleDump)

	cases := []struct {
		id    int64
		state model.GoroutineState
	}{
		{1, model.StateRunning},
		{18, model.StateWaiting},
		{42, model.StateBlocked},
		{99, model.StateSyscall},
	}

	byID := make(map[int64]goroutineInfo, len(gs))
	for _, g := range gs {
		byID[g.id] = g
	}

	for _, c := range cases {
		g, ok := byID[c.id]
		if !ok {
			t.Errorf("goroutine %d not found", c.id)
			continue
		}
		if g.state != c.state {
			t.Errorf("goroutine %d: want state %s, got %s", c.id, c.state, g.state)
		}
	}
}

func TestParseGoroutineDump_Reasons(t *testing.T) {
	t.Parallel()

	gs, _ := parseGoroutineDump(sampleDump)
	byID := make(map[int64]goroutineInfo, len(gs))
	for _, g := range gs {
		byID[g.id] = g
	}

	if r := byID[18].reason; r != model.ReasonChanRecv {
		t.Errorf("goroutine 18: want reason %s, got %s", model.ReasonChanRecv, r)
	}
	if r := byID[42].reason; r != model.ReasonMutexLock {
		t.Errorf("goroutine 42: want reason %s, got %s", model.ReasonMutexLock, r)
	}
}

func TestParseGoroutineDump_ParentID(t *testing.T) {
	t.Parallel()

	gs, _ := parseGoroutineDump(sampleDump)
	byID := make(map[int64]goroutineInfo, len(gs))
	for _, g := range gs {
		byID[g.id] = g
	}

	if p := byID[18].parentID; p != 7 {
		t.Errorf("goroutine 18: want parentID 7, got %d", p)
	}
	if p := byID[42].parentID; p != 1 {
		t.Errorf("goroutine 42: want parentID 1, got %d", p)
	}
}

func TestParseGoroutineDump_ResourceID(t *testing.T) {
	t.Parallel()

	gs, _ := parseGoroutineDump(sampleDump)
	byID := make(map[int64]goroutineInfo, len(gs))
	for _, g := range gs {
		byID[g.id] = g
	}

	// G42 blocks on sync.runtime_SemacquireMutex(0xc000a86f34?, ...)
	if rid := byID[42].resourceID; rid != "mutex:0xc000a86f34" {
		t.Errorf("goroutine 42: want resourceID mutex:0xc000a86f34, got %q", rid)
	}
	// G1 is running — no resource
	if rid := byID[1].resourceID; rid != "" {
		t.Errorf("goroutine 1: want empty resourceID, got %q", rid)
	}
}

func TestExtractResourceID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		frames []model.StackFrame
		want   string
	}{
		{
			desc:   "mutex via SemacquireMutex",
			frames: []model.StackFrame{{Func: "sync.runtime_SemacquireMutex(0xc000a86f34?, 0x0?, 0x1?)"}},
			want:   "mutex:0xc000a86f34",
		},
		{
			desc:   "chan recv",
			frames: []model.StackFrame{{Func: "runtime.chanrecv(0xc000036060, 0x0, 0x1)"}},
			want:   "chan:0xc000036060",
		},
		{
			desc:   "chan send",
			frames: []model.StackFrame{{Func: "runtime.chansend(0xc000100080, 0xc000012340, 0x1)"}},
			want:   "chan:0xc000100080",
		},
		{
			desc:   "nil address skipped, fallback to second frame",
			frames: []model.StackFrame{{Func: "runtime.chanrecv(0x0, 0x0, 0x0)"}, {Func: "runtime.chansend(0xc000100080, 0x0, 0x1)"}},
			want:   "chan:0xc000100080",
		},
		{
			desc:   "no blocking frame",
			frames: []model.StackFrame{{Func: "main.doWork(0xc000a00000)"}},
			want:   "",
		},
		{
			desc:   "empty frames",
			frames: nil,
			want:   "",
		},
	}

	for _, c := range cases {
		got := extractResourceID(c.frames)
		if got != c.want {
			t.Errorf("%s: want %q, got %q", c.desc, c.want, got)
		}
	}
}

func TestParseGoroutineDump_Frames(t *testing.T) {
	t.Parallel()

	gs, _ := parseGoroutineDump(sampleDump)
	byID := make(map[int64]goroutineInfo, len(gs))
	for _, g := range gs {
		byID[g.id] = g
	}

	frames := byID[42].frames
	if len(frames) == 0 {
		t.Fatal("goroutine 42: expected at least one frame")
	}
	if frames[0].File == "" {
		t.Error("goroutine 42: first frame has empty file")
	}
}

func TestParseGoroutineDump_Empty(t *testing.T) {
	t.Parallel()

	gs, err := parseGoroutineDump("")
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
	if len(gs) != 0 {
		t.Errorf("want 0 goroutines for empty input, got %d", len(gs))
	}
}

func TestParseGoroutineDump_MalformedHeaderSkipped(t *testing.T) {
	t.Parallel()

	input := `goroutine bad_id [running]:
main.foo()
	/tmp/foo.go:1 +0x1

goroutine 5 [runnable]:
main.bar()
	/tmp/bar.go:2 +0x1

`
	gs, err := parseGoroutineDump(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only goroutine 5 should be parsed; malformed header is skipped.
	if len(gs) != 1 || gs[0].id != 5 {
		t.Errorf("want 1 goroutine with id=5, got %v", gs)
	}
}

// ── classifyState ────────────────────────────────────────────────────────────

func TestClassifyState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input  string
		state  model.GoroutineState
		reason model.BlockingReason
	}{
		{"running", model.StateRunning, ""},
		{"runnable", model.StateRunnable, ""},
		{"chan receive", model.StateWaiting, model.ReasonChanRecv},
		{"chan receive, 5 minutes", model.StateWaiting, model.ReasonChanRecv},
		{"chan send", model.StateWaiting, model.ReasonChanSend},
		{"select", model.StateWaiting, model.ReasonSelect},
		{"sync.Mutex.Lock", model.StateBlocked, model.ReasonMutexLock},
		{"semacquire", model.StateBlocked, model.ReasonMutexLock},
		{"sync.RWMutex.Lock", model.StateBlocked, model.ReasonRWMutexLock},
		{"sync.RWMutex.RLock", model.StateWaiting, model.ReasonRWMutexR},
		{"syscall", model.StateSyscall, model.ReasonSyscall},
		{"sleep", model.StateWaiting, model.ReasonSleep},
		{"IO wait", model.StateWaiting, model.ReasonUnknown},
		{"dead", model.StateDone, ""},
		{"", model.StateDone, ""},
		{"some unknown state", model.StateWaiting, model.ReasonUnknown},
	}

	for _, c := range cases {
		s, r := classifyState(c.input)
		if s != c.state {
			t.Errorf("classifyState(%q): state want %s, got %s", c.input, c.state, s)
		}
		if r != c.reason {
			t.Errorf("classifyState(%q): reason want %q, got %q", c.input, c.reason, r)
		}
	}
}

// ── parseCreatedBy ───────────────────────────────────────────────────────────

func TestParseCreatedBy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		want int64
	}{
		{"created by net/http.(*Server).ListenAndServe in goroutine 1", 1},
		{"created by main.startWorkers in goroutine 42", 42},
		{"created by main.startWorkers", 0},
		{"no marker here", 0},
	}

	for _, c := range cases {
		got := parseCreatedBy(c.line)
		if got != c.want {
			t.Errorf("parseCreatedBy(%q): want %d, got %d", c.line, c.want, got)
		}
	}
}

// ── parseFileLine ────────────────────────────────────────────────────────────

func TestParseFileLine(t *testing.T) {
	t.Parallel()

	frame := parseFileLine("main.main", "/home/user/app/main.go:25 +0x7c")
	if frame.Func != "main.main" {
		t.Errorf("want func main.main, got %s", frame.Func)
	}
	if frame.File != "/home/user/app/main.go" {
		t.Errorf("want file /home/user/app/main.go, got %s", frame.File)
	}
	if frame.Line != 25 {
		t.Errorf("want line 25, got %d", frame.Line)
	}
}
