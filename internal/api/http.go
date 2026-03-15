// Package api provides the local REST API, SSE stream, and embedded UI assets.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
	"github.com/Khachatur86/goroscope/internal/version"
)

// Server is the goroscope local HTTP server.
type Server struct {
	addr     string
	engine   *analysis.Engine
	sessions *session.Manager
	uiPath   string // if non-empty, serve React UI from this dir instead of embedded vanilla UI
}

// NewServer returns a Server bound to addr with the given engine and session manager.
// If uiPath is non-empty, the server serves the React UI from that directory (e.g. web/dist).
func NewServer(addr string, engine *analysis.Engine, sessions *session.Manager, uiPath string) *Server {
	return &Server{
		addr:     addr,
		engine:   engine,
		sessions: sessions,
		uiPath:   uiPath,
	}
}

// Serve starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:    s.addr,
		Handler: s.routes(),
		// ReadHeaderTimeout guards against slowloris attacks.
		// WriteTimeout and ReadTimeout are intentionally omitted: the /stream
		// endpoint uses Server-Sent Events which requires long-lived connections,
		// and those timeouts would forcibly close active SSE clients.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		s.sessions.CompleteCurrent()
		_ = httpServer.Shutdown(context.Background())
	}()

	err := httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	if s.uiPath != "" {
		mux.Handle("/", s.handleReactUI())
	} else {
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(uiFileSystem()))))
		mux.HandleFunc("/", s.handleIndex)
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/session/current", s.handleSessionCurrent)
	mux.HandleFunc("/api/v1/goroutines", s.handleGoroutines)
	mux.HandleFunc("/api/v1/goroutines/{id}/children", s.handleGoroutineChildren)
	mux.HandleFunc("/api/v1/goroutines/{id}", s.handleGoroutineByID)
	mux.HandleFunc("/api/v1/insights", s.handleInsights)
	mux.HandleFunc("/api/v1/timeline", s.handleTimeline)
	mux.HandleFunc("/api/v1/processor-timeline", s.handleProcessorTimeline)
	mux.HandleFunc("/api/v1/resources/graph", s.handleGraph)
	mux.HandleFunc("/api/v1/deadlock-hints", s.handleDeadlockHints)
	mux.HandleFunc("/api/v1/stream", s.handleStream)
	mux.HandleFunc("/api/v1/replay/load", s.handleReplayLoad)

	if isLocalhostAddr(s.addr) {
		mux.Handle("/debug/pprof/", http.StripPrefix("/debug/pprof", http.HandlerFunc(pprof.Index)))
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	return mux
}

// isLocalhostAddr reports whether addr binds to localhost (127.0.0.1, ::1, or localhost).
// pprof endpoints are only exposed when the server is local-only (OBS-3).
func isLocalhostAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.ToLower(host) == "localhost"
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	serveEmbeddedFile(w, "index.html")
}

// handleReactUI returns a handler that serves the React SPA from s.uiPath.
// Serves index.html for unknown paths (SPA client-side routing).
func (s *Server) handleReactUI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		cleanPath := filepath.Clean(strings.TrimPrefix(path, "/"))
		fullPath := filepath.Join(s.uiPath, cleanPath)
		absRoot, _ := filepath.Abs(s.uiPath)
		absPath, _ := filepath.Abs(fullPath)
		if !strings.HasPrefix(absPath, absRoot) {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			fullPath = filepath.Join(s.uiPath, "index.html")
		}
		http.ServeFile(w, r, fullPath)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sessions.History())
}

func (s *Server) handleSessionCurrent(w http.ResponseWriter, _ *http.Request) {
	current := s.sessions.Current()
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	writeJSON(w, http.StatusOK, current)
}

// goroutineListParams holds query parameters for the goroutines endpoint.
type goroutineListParams struct {
	State     model.GoroutineState
	Reason    model.BlockingReason
	Search    string
	MinWaitNS int64 // filter goroutines in wait state with WaitNS >= MinWaitNS
	Limit     int
	Offset    int
}

func parseGoroutineListParams(r *http.Request) goroutineListParams {
	q := r.URL.Query()
	params := goroutineListParams{
		Limit:  -1, // -1 means no limit
		Offset: 0,
	}

	if v := q.Get("state"); v != "" {
		params.State = model.GoroutineState(v)
	}
	if v := q.Get("reason"); v != "" {
		params.Reason = model.BlockingReason(v)
	}
	if v := q.Get("search"); v != "" {
		params.Search = strings.TrimSpace(strings.ToLower(v))
	}
	if v := q.Get("min_wait_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			params.MinWaitNS = n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			params.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Offset = n
		}
	}

	return params
}

func isWaitState(s model.GoroutineState) bool {
	switch s {
	case model.StateBlocked, model.StateWaiting, model.StateSyscall:
		return true
	default:
		return false
	}
}

func filterGoroutines(goroutines []model.Goroutine, params goroutineListParams) []model.Goroutine {
	var out []model.Goroutine
	for _, g := range goroutines {
		if params.State != "" && g.State != params.State {
			continue
		}
		if params.Reason != "" && g.Reason != params.Reason {
			continue
		}
		if params.Search != "" {
			if !goroutineMatchesSearch(g, params.Search) {
				continue
			}
		}
		if params.MinWaitNS > 0 {
			if !isWaitState(g.State) || g.WaitNS < params.MinWaitNS {
				continue
			}
		}
		out = append(out, g)
	}
	return out
}

func goroutineMatchesSearch(g model.Goroutine, search string) bool {
	for _, v := range g.Labels {
		if strings.Contains(strings.ToLower(v), search) {
			return true
		}
	}
	if g.LastStack != nil {
		for _, f := range g.LastStack.Frames {
			if strings.Contains(strings.ToLower(f.Func), search) ||
				strings.Contains(strings.ToLower(f.File), search) {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleGoroutines(w http.ResponseWriter, r *http.Request) {
	etag := fmt.Sprintf(`"%x"`, s.engine.DataVersion())
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)

	params := parseGoroutineListParams(r)
	all := s.engine.ListGoroutines()
	filtered := filterGoroutines(all, params)
	total := len(filtered)

	if params.Limit >= 0 || params.Offset > 0 {
		start := params.Offset
		if start > total {
			start = total
		}
		end := total
		if params.Limit >= 0 && start+params.Limit < end {
			end = start + params.Limit
		}
		filtered = filtered[start:end]

		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		writeJSON(w, http.StatusOK, goroutineListResponse{
			Goroutines: filtered,
			Total:      total,
			Limit:      params.Limit,
			Offset:     params.Offset,
		})
		return
	}

	writeJSON(w, http.StatusOK, filtered)
}

// goroutineListResponse is the paginated response for /api/v1/goroutines.
type goroutineListResponse struct {
	Goroutines []model.Goroutine `json:"goroutines"`
	Total      int               `json:"total"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
}

func (s *Server) handleGoroutineByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}

	goroutine, ok := s.engine.GetGoroutine(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "goroutine not found"})
		return
	}

	writeJSON(w, http.StatusOK, goroutine)
}

func (s *Server) handleGoroutineChildren(w http.ResponseWriter, r *http.Request) {
	parentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}

	all := s.engine.ListGoroutines()
	var children []model.Goroutine
	for _, g := range all {
		if g.ParentID == parentID {
			children = append(children, g)
		}
	}

	writeJSON(w, http.StatusOK, children)
}

// insightsResponse is the response for /api/v1/insights.
type insightsResponse struct {
	LongBlockedCount int64             `json:"long_blocked_count"`
	LongBlocked      []model.Goroutine `json:"long_blocked"`
	MinWaitNS        int64             `json:"min_wait_ns"`
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	const defaultMinWaitNS = int64(time.Second)

	minWaitNS := defaultMinWaitNS
	if v := r.URL.Query().Get("min_wait_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			minWaitNS = n
		}
	}

	all := s.engine.ListGoroutines()
	var longBlocked []model.Goroutine
	for _, g := range all {
		if isWaitState(g.State) && g.WaitNS >= minWaitNS {
			longBlocked = append(longBlocked, g)
		}
	}

	totalCount := len(longBlocked)

	sort.Slice(longBlocked, func(i, j int) bool {
		return longBlocked[i].WaitNS > longBlocked[j].WaitNS
	})

	// Limit to top 20 for response size.
	topN := 20
	if len(longBlocked) > topN {
		longBlocked = longBlocked[:topN]
	}

	writeJSON(w, http.StatusOK, insightsResponse{
		LongBlockedCount: int64(totalCount),
		LongBlocked:      longBlocked,
		MinWaitNS:        minWaitNS,
	})
}

// timelineListParams holds query parameters for the timeline endpoint.
type timelineListParams struct {
	State  model.GoroutineState
	Reason model.BlockingReason
	Search string
}

func parseTimelineListParams(r *http.Request) timelineListParams {
	q := r.URL.Query()
	var params timelineListParams
	if v := q.Get("state"); v != "" {
		params.State = model.GoroutineState(v)
	}
	if v := q.Get("reason"); v != "" {
		params.Reason = model.BlockingReason(v)
	}
	if v := q.Get("search"); v != "" {
		params.Search = strings.TrimSpace(strings.ToLower(v))
	}
	return params
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	etag := fmt.Sprintf(`"%x"`, s.engine.DataVersion())
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)

	params := parseTimelineListParams(r)
	all := s.engine.Timeline()
	goroutines := s.engine.ListGoroutines()

	if params.State == "" && params.Reason == "" && params.Search == "" {
		writeJSON(w, http.StatusOK, all)
		return
	}

	// Build set of goroutine IDs that match the filter.
	matchIDs := make(map[int64]bool)
	for _, g := range goroutines {
		if params.State != "" && g.State != params.State {
			continue
		}
		if params.Reason != "" && g.Reason != params.Reason {
			continue
		}
		if params.Search != "" && !goroutineMatchesSearch(g, params.Search) {
			continue
		}
		matchIDs[g.ID] = true
	}

	var filtered []model.TimelineSegment
	for _, seg := range all {
		if matchIDs[seg.GoroutineID] {
			filtered = append(filtered, seg)
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleProcessorTimeline(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ProcessorTimeline())
}

func (s *Server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ResourceGraph())
}

func (s *Server) handleDeadlockHints(w http.ResponseWriter, _ *http.Request) {
	edges := s.engine.ResourceGraph()
	if len(edges) == 0 {
		timeline := s.engine.Timeline()
		goroutines := s.engine.ListGoroutines()
		edges = analysis.DeriveResourceEdgesFromTimeline(timeline, goroutines)
	}

	hints := analysis.FindDeadlockHints(edges, s.engine.ListGoroutines())
	writeJSON(w, http.StatusOK, map[string]any{
		"hints": hints,
	})
}

// handleReplayLoad accepts a .gtrace file upload and loads it into the engine.
// POST /api/v1/replay/load with multipart form field "file".
func (s *Server) handleReplayLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	const maxUploadSize = 100 << 20 // 100 MiB
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, fmt.Sprintf("parse multipart: %v", err), http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("missing or invalid file field: %v", err), http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, fmt.Sprintf("read file: %v", err), http.StatusInternalServerError)
		return
	}

	capture, err := tracebridge.LoadCaptureFromBytes(data)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid capture: %v", err), http.StatusBadRequest)
		return
	}

	current := s.sessions.StartSession("replay", "upload.gtrace")
	capture = tracebridge.BindCaptureSession(capture, current.ID)
	s.engine.LoadCapture(current, capture)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "session_id": current.ID})
}

// handleStream implements a Server-Sent Events (SSE) endpoint.
// The client receives an "update" event whenever the engine processes a new
// capture snapshot. Each event carries no payload — the client is expected to
// re-fetch the REST endpoints it cares about. This keeps the stream
// protocol-agnostic and the payload format versioning out of SSE.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "streaming not supported by this HTTP handler",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx / Google Cloud Run, etc.)
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.engine.Subscribe()
	defer s.engine.Unsubscribe(ch)

	// Immediately send a "connected" event so the client knows the stream is live.
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: update\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
