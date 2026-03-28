package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Khachatur86/goroscope/internal/staticanalysis"
)

// AnalyzeRequest is the request body for POST /api/v1/analyze.
type AnalyzeRequest struct {
	// Dirs is the list of directories to scan. Defaults to ["."].
	Dirs []string `json:"dirs"`
	// Recursive enables recursive directory walking.
	Recursive bool `json:"recursive"`
	// Rules restricts analysis to specific rule IDs. Empty means all rules.
	Rules []staticanalysis.RuleID `json:"rules,omitempty"`
	// EnrichRuntime, when true, cross-references findings with live goroutine
	// stacks from the current Engine session.
	EnrichRuntime bool `json:"enrich_runtime,omitempty"`
}

// handleAnalyze handles POST /api/v1/analyze.
// It runs static concurrency analysis on the requested directories and,
// optionally, enriches findings with runtime evidence from the live Engine.
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Dirs) == 0 {
		req.Dirs = []string{"."}
	}

	report, err := staticanalysis.Analyze(staticanalysis.AnalyzeInput{
		Dirs:      req.Dirs,
		Recursive: req.Recursive,
		Rules:     req.Rules,
	})
	if err != nil {
		http.Error(w, "analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.EnrichRuntime {
		s.enrichFindings(r, report)
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

// enrichFindings cross-references static findings with live goroutine stacks
// from the Engine session, populating RuntimeEvidence where a matching
// file:line is found in the most recent stack snapshot of any goroutine.
func (s *Server) enrichFindings(r *http.Request, report *staticanalysis.Report) {
	eng := s.engineFor(r)
	if eng == nil {
		return
	}
	goroutines := eng.ListGoroutines()
	if len(goroutines) == 0 {
		return
	}

	// Build a fast index: "file:line" → list of goroutine IDs whose latest
	// stack snapshot includes that location.
	type blockInfo struct {
		gids     []int64
		maxBlock int64
	}
	index := make(map[string]*blockInfo)
	for _, g := range goroutines {
		stacks := eng.GetStacksFor(g.ID)
		if len(stacks) == 0 {
			continue
		}
		latest := stacks[len(stacks)-1]
		for _, frame := range latest.Frames {
			key := frame.File + ":" + itoa(frame.Line)
			bi := index[key]
			if bi == nil {
				bi = &blockInfo{}
				index[key] = bi
			}
			bi.gids = append(bi.gids, g.ID)
			if g.WaitNS > bi.maxBlock {
				bi.maxBlock = g.WaitNS
			}
		}
	}

	now := time.Now()
	for i := range report.Findings {
		f := &report.Findings[i]
		key := f.Location.File + ":" + itoa(f.Location.Line)
		if bi, ok := index[key]; ok {
			f.RuntimeEvidence = &staticanalysis.RuntimeEvidence{
				GoroutineIDs: bi.gids,
				MaxBlockNS:   bi.maxBlock,
				ObservedAt:   now,
			}
		}
	}
}

// itoa is a minimal int→string helper shared within the api package.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 8)
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	d := make([]byte, 0, 8)
	for n > 0 {
		d = append(d, byte('0'+n%10))
		n /= 10
	}
	for i := len(d) - 1; i >= 0; i-- {
		b = append(b, d[i])
	}
	return string(b)
}
