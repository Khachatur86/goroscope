// Package staticanalysis implements static concurrency analysis for Go source code.
// It detects common concurrency anti-patterns (race conditions, deadlock-prone
// patterns, goroutine leaks) by walking the AST of Go packages.
//
// Findings can optionally be enriched with runtime evidence from a live Engine
// session by matching file:line locations against goroutine stack frames.
package staticanalysis

import (
	"fmt"
	"time"
)

// Severity classifies how urgent a finding is.
type Severity int

// Severity levels, ordered from most to least urgent.
const (
	SeverityCritical Severity = iota // Data race or definite deadlock path
	SeverityHigh                     // Likely concurrency bug under load
	SeverityMedium                   // Suspicious pattern, context-dependent
	SeverityInfo                     // Style/best-practice note
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "CRITICAL"
	case SeverityHigh:
		return "HIGH"
	case SeverityMedium:
		return "MEDIUM"
	default:
		return "INFO"
	}
}

// RuleID identifies a specific concurrency detector.
type RuleID string

// Rule IDs for each concurrency detector.
const (
	RuleLockWithoutDefer   RuleID = "SA-1" // sync.Mutex.Lock() not followed by defer Unlock()
	RuleLoopClosure        RuleID = "SA-2" // goroutine closure captures loop variable
	RuleWaitGroupAfterGo   RuleID = "SA-3" // wg.Add() called after goroutine start
	RuleMutexByValue       RuleID = "SA-4" // sync.Mutex or sync.RWMutex copied by value
	RuleUnbufferedChanSend RuleID = "SA-5" // unbuffered channel send outside select
	RuleLockAcrossCall     RuleID = "SA-6" // mutex held across external/blocking call
	RuleDoubleLock         RuleID = "SA-7" // same mutex locked twice in one function
	RuleSleepNoContext     RuleID = "SA-8" // time.Sleep in goroutine without context cancel
	RuleChanNoClose        RuleID = "SA-9" // channel iterated with range but never closed
)

// Location pinpoints a finding in source code.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

func (l Location) String() string {
	return fmt.Sprintf("%s:%d:%d", l.File, l.Line, l.Column)
}

// RuntimeEvidence links a static finding to observed runtime behaviour.
// Populated by the Enricher when a live Engine session is available.
type RuntimeEvidence struct {
	// GoroutineIDs are the IDs of goroutines whose stack frames match this location.
	GoroutineIDs []int64 `json:"goroutine_ids,omitempty"`
	// MaxBlockNS is the longest observed block duration (ns) among matching goroutines.
	MaxBlockNS int64 `json:"max_block_ns,omitempty"`
	// DeadlockCycleIDs are IDs of deadlock cycles that include this location.
	DeadlockCycleIDs []int `json:"deadlock_cycle_ids,omitempty"`
	// ObservedAt is when the runtime evidence was collected.
	ObservedAt time.Time `json:"observed_at"`
}

// Finding is a single static analysis result.
type Finding struct {
	Rule     RuleID   `json:"rule"`
	Severity Severity `json:"severity"`
	Location Location `json:"location"`
	// Message is a human-readable description of the issue.
	Message string `json:"message"`
	// Suggestion is an actionable fix hint.
	Suggestion string `json:"suggestion,omitempty"`
	// RuntimeEvidence is non-nil when this finding has been corroborated by
	// live runtime data from an Engine session.
	RuntimeEvidence *RuntimeEvidence `json:"runtime_evidence,omitempty"`
}

// Report is the output of an analysis run over one or more packages.
type Report struct {
	// Packages lists the package import paths that were analysed.
	Packages []string `json:"packages"`
	// Findings contains all detected issues, sorted by severity then file:line.
	Findings []Finding `json:"findings"`
	// Stats summarises the run.
	Stats ReportStats `json:"stats"`
}

// ReportStats captures analysis run metrics.
type ReportStats struct {
	FilesScanned    int `json:"files_scanned"`
	PackagesScanned int `json:"packages_scanned"`
	Critical        int `json:"critical"`
	High            int `json:"high"`
	Medium          int `json:"medium"`
	Info            int `json:"info"`
}
