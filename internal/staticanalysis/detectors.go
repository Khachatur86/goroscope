package staticanalysis

import (
	"go/ast"
	"go/token"
	"strings"
)

// ── SA-1: Lock without defer ──────────────────────────────────────────────────

type lockWithoutDeferDetector struct{}

func (d *lockWithoutDeferDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		lockCalls := collectLockCalls(fn.Body)
		deferUnlocks := collectDeferUnlocks(fn.Body)
		for _, lc := range lockCalls {
			if !deferUnlocks[lc.recv] {
				pos := fset.Position(lc.pos)
				findings = append(findings, Finding{
					Rule:       RuleLockWithoutDefer,
					Severity:   SeverityHigh,
					Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
					Message:    lc.recv + ".Lock() called without a paired defer " + lc.recv + ".Unlock()",
					Suggestion: "Add 'defer " + lc.recv + ".Unlock()' immediately after the Lock() call.",
				})
			}
		}
		return true
	})
	return findings
}

type lockCall struct {
	recv string
	pos  token.Pos
}

// collectLockCalls returns all expr.Lock() calls in the function body.
func collectLockCalls(body *ast.BlockStmt) []lockCall {
	var out []lockCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock" {
			if id, ok := sel.X.(*ast.Ident); ok {
				out = append(out, lockCall{recv: id.Name, pos: call.Pos()})
			}
		}
		return true
	})
	return out
}

// collectDeferUnlocks returns a set of receiver names that have a defer Unlock.
func collectDeferUnlocks(body *ast.BlockStmt) map[string]bool {
	out := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		call, ok := d.Call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if call.Sel.Name == "Unlock" || call.Sel.Name == "RUnlock" {
			if id, ok := call.X.(*ast.Ident); ok {
				out[id.Name] = true
			}
		}
		return true
	})
	return out
}

// ── SA-2: Loop closure capture ────────────────────────────────────────────────

type loopClosureDetector struct{}

func (d *loopClosureDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		loop, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		// Collect loop variable names.
		loopVars := loopVarNames(loop)
		if len(loopVars) == 0 {
			return true
		}
		// Look for go func() literals inside the loop body that capture loop vars.
		ast.Inspect(loop.Body, func(inner ast.Node) bool {
			goStmt, ok := inner.(*ast.GoStmt)
			if !ok {
				return true
			}
			lit, ok := goStmt.Call.Fun.(*ast.FuncLit)
			if !ok {
				return true
			}
			// Check if any loop var is read inside the closure body
			// but not passed as a parameter.
			params := funcLitParamNames(lit)
			ast.Inspect(lit.Body, func(ref ast.Node) bool {
				id, ok := ref.(*ast.Ident)
				if !ok {
					return true
				}
				if loopVars[id.Name] && !params[id.Name] {
					pos := fset.Position(goStmt.Pos())
					findings = append(findings, Finding{
						Rule:       RuleLoopClosure,
						Severity:   SeverityHigh,
						Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
						Message:    "goroutine closure captures loop variable '" + id.Name + "' by reference",
						Suggestion: "Pass '" + id.Name + "' as a parameter to the goroutine: go func(" + id.Name + " T) { ... }(" + id.Name + ")",
					})
					return false // one finding per go statement is enough
				}
				return true
			})
			return true
		})
		return true
	})
	return findings
}

func loopVarNames(loop *ast.RangeStmt) map[string]bool {
	out := make(map[string]bool)
	if id, ok := loop.Key.(*ast.Ident); ok && id.Name != "_" {
		out[id.Name] = true
	}
	if loop.Value != nil {
		if id, ok := loop.Value.(*ast.Ident); ok && id.Name != "_" {
			out[id.Name] = true
		}
	}
	return out
}

func funcLitParamNames(lit *ast.FuncLit) map[string]bool {
	out := make(map[string]bool)
	if lit.Type.Params == nil {
		return out
	}
	for _, field := range lit.Type.Params.List {
		for _, name := range field.Names {
			out[name.Name] = true
		}
	}
	return out
}

// ── SA-3: WaitGroup.Add after goroutine start ─────────────────────────────────

type waitGroupAfterGoDetector struct{}

func (d *waitGroupAfterGoDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		// Walk statements in order; flag wg.Add() that appears after a go statement
		// that is not preceded by a wg.Add in the same block.
		seenGo := false
		for _, stmt := range fn.Body.List {
			if isGoStmt(stmt) {
				seenGo = true
			}
			if seenGo && isWGAdd(stmt) {
				pos := fset.Position(stmt.Pos())
				findings = append(findings, Finding{
					Rule:       RuleWaitGroupAfterGo,
					Severity:   SeverityHigh,
					Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
					Message:    "WaitGroup.Add() called after goroutine is already started",
					Suggestion: "Call wg.Add(n) before launching the goroutine to avoid a race on the counter.",
				})
			}
		}
		return true
	})
	return findings
}

func isGoStmt(s ast.Stmt) bool {
	_, ok := s.(*ast.GoStmt)
	return ok
}

func isWGAdd(s ast.Stmt) bool {
	expr, ok := s.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Add"
}

// ── SA-4: Mutex copied by value ───────────────────────────────────────────────

type mutexByValueDetector struct{}

var mutexTypes = map[string]bool{
	"Mutex":   true,
	"RWMutex": true,
}

func (d *mutexByValueDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			return true
		}
		for _, field := range fn.Type.Params.List {
			typeName := selectorOrIdentName(field.Type)
			if mutexTypes[typeName] {
				pos := fset.Position(field.Pos())
				findings = append(findings, Finding{
					Rule:       RuleMutexByValue,
					Severity:   SeverityCritical,
					Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
					Message:    "sync." + typeName + " passed by value — copying a mutex invalidates its lock state",
					Suggestion: "Pass *sync." + typeName + " (pointer) instead.",
				})
			}
		}
		return true
	})
	return findings
}

func selectorOrIdentName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// ── SA-5: Unbuffered channel send outside select ──────────────────────────────

type unbufferedChanSendDetector struct{}

func (d *unbufferedChanSendDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	// Collect channels declared as make(chan T) without a capacity argument.
	unbuffered := collectUnbufferedChans(file)
	if len(unbuffered) == 0 {
		return nil
	}
	ast.Inspect(file, func(n ast.Node) bool {
		send, ok := n.(*ast.SendStmt)
		if !ok {
			return true
		}
		ch, ok := send.Chan.(*ast.Ident)
		if !ok {
			return true
		}
		if unbuffered[ch.Name] {
			pos := fset.Position(send.Pos())
			findings = append(findings, Finding{
				Rule:       RuleUnbufferedChanSend,
				Severity:   SeverityMedium,
				Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
				Message:    "send on unbuffered channel '" + ch.Name + "' outside a select — may block indefinitely",
				Suggestion: "Either buffer the channel: make(chan T, 1), or wrap in a select with a default case.",
			})
		}
		return true
	})
	return findings
}

// collectUnbufferedChans finds all `make(chan T)` assignments (no capacity).
func collectUnbufferedChans(file *ast.File) map[string]bool {
	out := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "make" {
				continue
			}
			if len(call.Args) != 1 { // make(chan T) has 1 arg; make(chan T, N) has 2
				continue
			}
			if _, isChan := call.Args[0].(*ast.ChanType); !isChan {
				continue
			}
			if i < len(assign.Lhs) {
				if id, ok := assign.Lhs[i].(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// ── SA-7: Double lock ─────────────────────────────────────────────────────────

type doubleLockDetector struct{}

func (d *doubleLockDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		// Map from receiver name → first lock position.
		locked := make(map[string]token.Pos)
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			// On defer-unlock: release the lock record.
			if ds, ok := inner.(*ast.DeferStmt); ok {
				if sel, ok := ds.Call.Fun.(*ast.SelectorExpr); ok {
					if sel.Sel.Name == "Unlock" || sel.Sel.Name == "RUnlock" {
						if id, ok := sel.X.(*ast.Ident); ok {
							delete(locked, id.Name)
						}
					}
				}
				return true
			}
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Lock", "RLock":
				if first, exists := locked[id.Name]; exists {
					pos := fset.Position(call.Pos())
					firstPos := fset.Position(first)
					findings = append(findings, Finding{
						Rule:       RuleDoubleLock,
						Severity:   SeverityCritical,
						Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
						Message:    id.Name + ".Lock() called twice in the same function (first lock at line " + itoa(firstPos.Line) + ")",
						Suggestion: "Unlock '" + id.Name + "' before locking it again, or refactor to avoid nested locking.",
					})
				} else {
					locked[id.Name] = call.Pos()
				}
			case "Unlock", "RUnlock":
				delete(locked, id.Name)
			}
			return true
		})
		return true
	})
	return findings
}

// ── SA-8: time.Sleep without context ─────────────────────────────────────────

type sleepNoContextDetector struct{}

func (d *sleepNoContextDetector) Detect(fset *token.FileSet, file *ast.File) []Finding {
	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		goStmt, ok := n.(*ast.GoStmt)
		if !ok {
			return true
		}
		lit, ok := goStmt.Call.Fun.(*ast.FuncLit)
		if !ok {
			return true
		}
		hasSleep := false
		hasCtxDone := false
		ast.Inspect(lit.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isTimeSleep(call) {
				hasSleep = true
			}
			if isCtxDoneSelect(call) || isCtxDoneRef(inner) {
				hasCtxDone = true
			}
			return true
		})
		// Also check for select { case <-ctx.Done(): } pattern in the literal.
		if !hasCtxDone {
			ast.Inspect(lit.Body, func(inner ast.Node) bool {
				if isChanCtxDone(inner) {
					hasCtxDone = true
				}
				return true
			})
		}
		if hasSleep && !hasCtxDone {
			pos := fset.Position(goStmt.Pos())
			findings = append(findings, Finding{
				Rule:       RuleSleepNoContext,
				Severity:   SeverityMedium,
				Location:   Location{File: pos.Filename, Line: pos.Line, Column: pos.Column},
				Message:    "goroutine uses time.Sleep without respecting context cancellation — may leak on shutdown",
				Suggestion: "Replace time.Sleep with a select on time.After and ctx.Done().",
			})
		}
		return true
	})
	return findings
}

func isTimeSleep(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time" && sel.Sel.Name == "Sleep"
}

func isCtxDoneSelect(call *ast.CallExpr) bool {
	// Detect ctx.Err() usage as a proxy for checking context.
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Err" || sel.Sel.Name == "Done"
}

func isCtxDoneRef(n ast.Node) bool {
	// Detect references to ctx.Done() channel in select cases.
	sel, ok := n.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Done"
}

func isChanCtxDone(n ast.Node) bool {
	sel, ok := n.(*ast.CommClause)
	if !ok {
		return false
	}
	if sel.Comm == nil {
		return false
	}
	recv, ok := sel.Comm.(*ast.ExprStmt)
	if !ok {
		return false
	}
	unary, ok := recv.X.(*ast.UnaryExpr)
	if !ok || unary.Op != token.ARROW {
		return false
	}
	call, ok := unary.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := call.Fun.(*ast.SelectorExpr)
	return ok && s.Sel.Name == "Done"
}

// itoa is a minimal int-to-string helper to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i := len(digits) - 1; i >= 0; i-- {
		buf = append(buf, digits[i])
	}
	return strings.TrimSpace(string(buf))
}
