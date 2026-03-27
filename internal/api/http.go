// Package api provides the local REST API, SSE stream, and embedded UI assets.
package api

import (
	"context"
	"embed"
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
	"github.com/Khachatur86/goroscope/internal/target"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
	"github.com/Khachatur86/goroscope/internal/version"
)

//go:embed openapi.yaml
var openapiYAML embed.FS

// Config holds optional TLS and authentication settings for the Server.
// Zero value means plaintext HTTP with no authentication.
type Config struct {
	// TLSCertFile is the path to a PEM-encoded TLS certificate.
	// When non-empty, the server uses HTTPS via ListenAndServeTLS.
	TLSCertFile string
	// TLSKeyFile is the path to the PEM-encoded private key paired with TLSCertFile.
	TLSKeyFile string
	// Token is a bearer token required for every request.
	// When empty, no authentication is enforced.
	Token string
	// CORSOrigins is an allowlist of origins for cross-origin requests (I-6).
	// When empty, cross-origin requests are not permitted.
	// Use "*" to allow any origin (insecure; not recommended for remote access).
	CORSOrigins []string
}

// Server is the goroscope local HTTP server.
type Server struct {
	addr     string
	engine   *analysis.Engine
	sessions *session.Manager
	uiPath   string // if non-empty, serve React UI from this dir instead of embedded vanilla UI
	config   Config
	// registry holds the multi-target state when more than one process is
	// being monitored (H-7). When nil, s.engine / s.sessions are used directly.
	registry *target.Registry
	// serveCtx is set in Serve and passed to target additions requested via API.
	serveCtx context.Context
}

// NewServer returns a Server bound to addr with the given engine and session manager.
// If uiPath is non-empty, the server serves the React UI from that directory (e.g. web/dist).
// An optional Config may be supplied as the last argument to enable TLS or bearer-token auth.
func NewServer(addr string, engine *analysis.Engine, sessions *session.Manager, uiPath string, cfgs ...Config) *Server {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return &Server{
		addr:     addr,
		engine:   engine,
		sessions: sessions,
		uiPath:   uiPath,
		config:   cfg,
	}
}

// WithRegistry attaches a multi-target registry to the server. When set,
// requests carrying a ?target_id= query parameter are routed to the
// corresponding target's engine; requests without target_id fall back to
// the registry's default target (or s.engine when registry is also empty).
func (s *Server) WithRegistry(r *target.Registry) {
	s.registry = r
}

// engineFor returns the analysis engine for the request. If the request
// carries a ?target_id= parameter the matching registry target is used;
// otherwise the server's primary engine is returned.
func (s *Server) engineFor(r *http.Request) *analysis.Engine {
	if s.registry != nil {
		if id := r.URL.Query().Get("target_id"); id != "" {
			if t, ok := s.registry.Get(id); ok {
				return t.Engine
			}
		}
		if t, ok := s.registry.Default(); ok {
			return t.Engine
		}
	}
	return s.engine
}

// sessionsFor returns the session manager for the request, mirroring engineFor.
func (s *Server) sessionsFor(r *http.Request) *session.Manager {
	if s.registry != nil {
		if id := r.URL.Query().Get("target_id"); id != "" {
			if t, ok := s.registry.Get(id); ok {
				return t.Sessions
			}
		}
		if t, ok := s.registry.Default(); ok {
			return t.Sessions
		}
	}
	return s.sessions
}

// Serve starts the HTTP server and blocks until ctx is cancelled.
// When Config.TLSCertFile is set, the server uses HTTPS.
// When Config.Token is set, every request must carry a matching Bearer token.
func (s *Server) Serve(ctx context.Context) error {
	if err := s.validateConfig(); err != nil {
		return err
	}
	s.serveCtx = ctx // stored for use by handleTargetsCreate

	handler := s.routes()
	if s.config.Token != "" {
		handler = bearerAuth(s.config.Token, handler)
	}
	handler = securityHeaders(s.config, handler)

	httpServer := &http.Server{
		Addr:    s.addr,
		Handler: handler,
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

	var err error
	if s.config.TLSCertFile != "" {
		err = httpServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
	} else {
		err = httpServer.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// validateConfig returns an error when the server is bound to a non-loopback
// address but no TLS certificate is provided (SEC-1: explicit I/O timeouts;
// TLS required for remote access).
func (s *Server) validateConfig() error {
	if isLocalhostAddr(s.addr) {
		return nil
	}
	if s.config.TLSCertFile == "" {
		return fmt.Errorf(
			"server bound to non-loopback address %q requires TLS: "+
				"provide --tls-cert and --tls-key, or bind to 127.0.0.1",
			s.addr,
		)
	}
	if s.config.TLSKeyFile == "" {
		return fmt.Errorf("--tls-key is required when --tls-cert is set")
	}
	return nil
}

// bearerAuth wraps next with HTTP Bearer token authentication.
// Requests without a matching Authorization: Bearer <token> header receive 401.
func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) || auth[len(prefix):] != token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="goroscope"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds HTTP security headers to every response (I-6).
// CORS preflight is handled when cfg.CORSOrigins is non-empty.
func securityHeaders(cfg Config, next http.Handler) http.Handler {
	allowedOrigins := make(map[string]bool, len(cfg.CORSOrigins))
	for _, o := range cfg.CORSOrigins {
		allowedOrigins[o] = true
	}
	useHSTS := cfg.Token != "" || cfg.TLSCertFile != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		if useHSTS {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if len(cfg.CORSOrigins) > 0 {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowedOrigins["*"] || allowedOrigins[origin]) {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "86400")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	switch {
	case s.uiPath != "":
		// Explicit external path (dev mode: make run-react).
		mux.Handle("/", s.handleReactUI())
	default:
		// Always try the embedded React bundle first; fall back to vanilla UI
		// when the bundle has not been built yet (placeholder index.html only).
		if reactFS, ok := reactUIFileSystem(); ok {
			mux.Handle("/", serveEmbeddedReactSPA(reactFS))
		} else {
			mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(uiFileSystem()))))
			mux.HandleFunc("/", s.handleIndex)
		}
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/openapi.yaml", s.handleOpenAPISpec)
	mux.HandleFunc("/api/docs", s.handleSwaggerUI)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/session/current", s.handleSessionCurrent)
	mux.HandleFunc("/api/v1/goroutines", s.handleGoroutines)
	mux.HandleFunc("/api/v1/goroutines/groups", s.handleGoroutineGroups)
	mux.HandleFunc("/api/v1/goroutines/{id}/children", s.handleGoroutineChildren)
	mux.HandleFunc("/api/v1/goroutines/{id}", s.handleGoroutineByID)
	mux.HandleFunc("/api/v1/goroutines/{id}/stack-at", s.handleGoroutineStackAt)
	mux.HandleFunc("/api/v1/goroutines/{id}/stacks", s.handleGoroutineStacks)
	mux.HandleFunc("/api/v1/insights", s.handleInsights)
	mux.HandleFunc("/api/v1/smart-insights", s.handleSmartInsights)
	mux.HandleFunc("/api/v1/timeline", s.handleTimeline)
	mux.HandleFunc("/api/v1/processor-timeline", s.handleProcessorTimeline)
	mux.HandleFunc("/api/v1/resources/graph", s.handleGraph)
	mux.HandleFunc("/api/v1/deadlock-hints", s.handleDeadlockHints)
	mux.HandleFunc("/api/v1/stream", s.handleStream)
	mux.HandleFunc("/api/v1/replay/load", s.handleReplayLoad)
	mux.HandleFunc("/api/v1/replay/export", s.handleReplayExport)
	mux.HandleFunc("/api/v1/compare", s.handleCompare)
	mux.HandleFunc("/api/v1/compare/stacks", s.handleCompareStacks)
	mux.HandleFunc("/api/v1/memory", s.handleMemoryStats)
	mux.HandleFunc("/api/v1/pprof/stacks", s.handlePprofStacks)
	mux.HandleFunc("/api/v1/requests", s.handleRequests)
	mux.HandleFunc("/api/v1/requests/{id}/goroutines", s.handleRequestGoroutines)
	mux.HandleFunc("/metrics", s.handleMetrics)
	// H-7: multi-target monitoring endpoints.
	mux.HandleFunc("/api/v1/targets", s.handleTargets)
	mux.HandleFunc("/api/v1/targets/{id}", s.handleTargetByID)

	if isLocalhostAddr(s.addr) {
		mux.Handle("/debug/pprof/", http.StripPrefix("/debug/pprof", http.HandlerFunc(pprof.Index)))
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	} else {
		// Explicitly block the pprof prefix so the React SPA catch-all ("/"
		// returning 200) does not accidentally expose what looks like a pprof
		// endpoint to remote callers (OBS-3: local-only access).
		mux.HandleFunc("/debug/pprof/", http.NotFound)
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

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sessionsFor(r).History())
}

func (s *Server) handleSessionCurrent(w http.ResponseWriter, r *http.Request) {
	current := s.sessionsFor(r).Current()
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	writeJSON(w, http.StatusOK, current)
}

// goroutineListParams holds query parameters for the goroutines endpoint.
type goroutineListParams struct {
	State      model.GoroutineState
	Reason     model.BlockingReason
	Search     string
	StackFrame string // substring matched against any frame in LastStack (H-1)
	MinWaitNS  int64  // filter goroutines in wait state with WaitNS >= MinWaitNS
	Label      string // key=value for pprof label filter
	Limit      int
	Offset     int
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
	if v := q.Get("stack_frame"); v != "" {
		params.StackFrame = strings.TrimSpace(strings.ToLower(v))
	}
	if v := q.Get("min_wait_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			params.MinWaitNS = n
		}
	}
	if v := q.Get("label"); v != "" {
		params.Label = strings.TrimSpace(v)
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
		if params.StackFrame != "" {
			if !goroutineMatchesStackFrame(g, params.StackFrame) {
				continue
			}
		}
		if params.MinWaitNS > 0 {
			if !isWaitState(g.State) || g.WaitNS < params.MinWaitNS {
				continue
			}
		}
		if params.Label != "" {
			if eq := strings.Index(params.Label, "="); eq > 0 {
				key := params.Label[:eq]
				value := params.Label[eq+1:]
				if g.Labels == nil || g.Labels[key] != value {
					continue
				}
			}
		}
		out = append(out, g)
	}
	return out
}

// goroutineMatchesStackFrame reports whether any frame in g's last stack
// contains needle as a case-insensitive substring. Used by the ?stack_frame=
// query parameter (H-1) for pure frame-based search, independent of labels.
func goroutineMatchesStackFrame(g model.Goroutine, needle string) bool {
	if g.LastStack == nil {
		return false
	}
	for _, f := range g.LastStack.Frames {
		if strings.Contains(strings.ToLower(f.Func), needle) ||
			strings.Contains(strings.ToLower(f.File), needle) {
			return true
		}
	}
	return false
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
	etag := fmt.Sprintf(`"%x"`, s.engineFor(r).DataVersion())
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)

	params := parseGoroutineListParams(r)
	all := s.engineFor(r).ListGoroutines()
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

	// Apply display-level sampling when no explicit pagination is requested.
	// This keeps responses fast and the UI smooth for very large traces.
	sample := analysis.SampleGoroutines(filtered, s.engineFor(r).GetSamplingPolicy())
	if sample.Sampled {
		w.Header().Set("X-Sampled", "true")
		w.Header().Set("X-Total-Count", strconv.Itoa(sample.TotalCount))
		writeJSON(w, http.StatusOK, goroutineListResponse{
			Goroutines:   sample.Goroutines,
			Total:        sample.TotalCount,
			Sampled:      true,
			TotalCount:   sample.TotalCount,
			DisplayCount: sample.DisplayCount,
			Warning:      sample.Warning,
		})
		return
	}

	writeJSON(w, http.StatusOK, filtered)
}

// goroutineListResponse is the response for /api/v1/goroutines.
// It is returned for both paginated and sampled responses so the shape is
// always consistent. The frontend already handles both array and object forms.
type goroutineListResponse struct {
	Goroutines []model.Goroutine `json:"goroutines"`
	Total      int               `json:"total"`
	Limit      int               `json:"limit,omitempty"`
	Offset     int               `json:"offset,omitempty"`
	// Sampling fields — only set when the list was truncated by the display cap.
	Sampled      bool   `json:"sampled,omitempty"`
	TotalCount   int    `json:"total_count,omitempty"`
	DisplayCount int    `json:"display_count,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

func (s *Server) handleGoroutineByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}

	goroutine, ok := s.engineFor(r).GetGoroutine(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "goroutine not found"})
		return
	}

	writeJSON(w, http.StatusOK, goroutine)
}

func (s *Server) handleGoroutineStackAt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}
	nsStr := r.URL.Query().Get("ns")
	if nsStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ns query parameter"})
		return
	}
	ns, err := strconv.ParseInt(nsStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ns"})
		return
	}
	snapshot := s.engineFor(r).GetStackAt(id, ns)
	if snapshot == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no stack at that time"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// handleGoroutineStacks returns all historical stack snapshots for a goroutine.
// Clients use this to build per-goroutine flame graphs.
func (s *Server) handleGoroutineStacks(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}
	stacks := s.engineFor(r).GetStacksFor(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"goroutine_id": id,
		"stacks":       stacks,
	})
}

// handlePprofStacks returns all historical stack snapshots within a time range.
// Query params: start_ns and end_ns (int64, nanoseconds since epoch).
// Used to build cross-goroutine CPU flame graphs for a selected segment.
func (s *Server) handlePprofStacks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startNS, errA := strconv.ParseInt(q.Get("start_ns"), 10, 64)
	endNS, errB := strconv.ParseInt(q.Get("end_ns"), 10, 64)
	if errA != nil || errB != nil || startNS <= 0 || endNS <= 0 || startNS >= endNS {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_ns and end_ns must be valid positive integers with start_ns < end_ns"})
		return
	}
	stacks := s.engineFor(r).GetStacksInRange(startNS, endNS)
	writeJSON(w, http.StatusOK, map[string]any{
		"stacks":   stacks,
		"start_ns": startNS,
		"end_ns":   endNS,
	})
}

func (s *Server) handleGoroutineChildren(w http.ResponseWriter, r *http.Request) {
	parentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid goroutine id"})
		return
	}

	all := s.engineFor(r).ListGoroutines()
	var children []model.Goroutine
	for _, g := range all {
		if g.ParentID == parentID {
			children = append(children, g)
		}
	}

	writeJSON(w, http.StatusOK, children)
}

// handleGoroutineGroups aggregates goroutines by a shared dimension.
//
// GET /api/v1/goroutines/groups?by=function|package|parent_id|label[&label_key=<key>]
//
// Returns goroutine groups sorted by count descending. Each group carries
// per-state counts, wait-time metrics, and accumulated CPU time.
func (s *Server) handleGoroutineGroups(w http.ResponseWriter, r *http.Request) {
	byStr := r.URL.Query().Get("by")
	if byStr == "" {
		byStr = "function"
	}
	labelKey := strings.TrimSpace(r.URL.Query().Get("label_key"))

	goroutines := s.engineFor(r).ListGoroutines()
	segments := s.engineFor(r).Timeline()

	groups, err := analysis.GroupGoroutines(analysis.GroupGoroutinesInput{
		Goroutines: goroutines,
		Segments:   segments,
		By:         analysis.GroupByField(byStr),
		LabelKey:   labelKey,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"groups": groups,
		"by":     byStr,
		"total":  len(groups),
	})
}

// insightsResponse is the response for /api/v1/insights.
type insightsResponse struct {
	LongBlockedCount    int64             `json:"long_blocked_count"`
	LongBlocked         []model.Goroutine `json:"long_blocked"`
	MinWaitNS           int64             `json:"min_wait_ns"`
	LeakCandidatesCount int64             `json:"leak_candidates_count"`
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	const defaultMinWaitNS = int64(time.Second)
	const defaultLeakThresholdNS = 30 * int64(time.Second)

	minWaitNS := defaultMinWaitNS
	if v := r.URL.Query().Get("min_wait_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			minWaitNS = n
		}
	}

	leakThresholdNS := defaultLeakThresholdNS
	if v := r.URL.Query().Get("leak_threshold_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			leakThresholdNS = n
		}
	}

	all := s.engineFor(r).ListGoroutines()
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

	leakCandidates := analysis.LeakCandidates(all, leakThresholdNS)

	writeJSON(w, http.StatusOK, insightsResponse{
		LongBlockedCount:    int64(totalCount),
		LongBlocked:         longBlocked,
		MinWaitNS:           minWaitNS,
		LeakCandidatesCount: int64(len(leakCandidates)),
	})
}

// handleSmartInsights synthesises all analysis primitives into a ranked list
// of actionable findings.
//
// GET /api/v1/smart-insights
//
// Optional query params:
//
//	leak_threshold_ns      int64  (default 30s)
//	block_threshold_ns     int64  (default 1s)
//	contention_min_peak    int    (default 4)
//	goroutine_count_min    int    (default 1000)
func (s *Server) handleSmartInsights(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var leakNS, blockNS int64
	var contentionMin, goroutineMin int

	if v := q.Get("leak_threshold_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			leakNS = n
		}
	}
	if v := q.Get("block_threshold_ns"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			blockNS = n
		}
	}
	if v := q.Get("contention_min_peak"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			contentionMin = n
		}
	}
	if v := q.Get("goroutine_count_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			goroutineMin = n
		}
	}

	goroutines := s.engineFor(r).ListGoroutines()
	segments := s.engineFor(r).Timeline()
	edges := s.engineFor(r).ResourceGraph()
	if len(edges) == 0 {
		edges = analysis.DeriveCurrentContentionEdges(goroutines)
	}

	insights := analysis.GenerateInsights(analysis.GenerateInsightsInput{
		Goroutines:           goroutines,
		Segments:             segments,
		Edges:                edges,
		LeakThresholdNS:      leakNS,
		LongBlockThresholdNS: blockNS,
		ContentionMinPeak:    contentionMin,
		GoroutineCountMin:    goroutineMin,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"insights": insights,
		"total":    len(insights),
	})
}

// timelineListParams holds query parameters for the timeline endpoint.
type timelineListParams struct {
	State        model.GoroutineState
	Reason       model.BlockingReason
	Search       string
	Label        string
	GoroutineIDs map[int64]bool // when non-nil, only return segments for these IDs
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
	if v := q.Get("label"); v != "" {
		params.Label = strings.TrimSpace(v)
	}
	// goroutine_ids=1,2,3 — only return segments for these goroutine IDs.
	if v := q.Get("goroutine_ids"); v != "" {
		params.GoroutineIDs = make(map[int64]bool)
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			var id int64
			if _, err := fmt.Sscanf(tok, "%d", &id); err == nil {
				params.GoroutineIDs[id] = true
			}
		}
	}
	return params
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	etag := fmt.Sprintf(`"%x"`, s.engineFor(r).DataVersion())
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)

	params := parseTimelineListParams(r)
	all := s.engineFor(r).Timeline()

	// Fast path: no filters at all.
	hasFilters := params.State != "" || params.Reason != "" || params.Search != "" || params.Label != "" || params.GoroutineIDs != nil
	if !hasFilters {
		writeJSON(w, http.StatusOK, all)
		return
	}

	// Build set of goroutine IDs that pass attribute filters (state/reason/search/label).
	// When GoroutineIDs is set, use it as the primary allow-list and skip the goroutine scan.
	matchIDs := params.GoroutineIDs
	if params.State != "" || params.Reason != "" || params.Search != "" || params.Label != "" {
		goroutines := s.engineFor(r).ListGoroutines()
		matchIDs = make(map[int64]bool, len(goroutines))
		for _, g := range goroutines {
			// If the caller also supplied goroutine_ids, intersect.
			if params.GoroutineIDs != nil && !params.GoroutineIDs[g.ID] {
				continue
			}
			if params.State != "" && g.State != params.State {
				continue
			}
			if params.Reason != "" && g.Reason != params.Reason {
				continue
			}
			if params.Search != "" && !goroutineMatchesSearch(g, params.Search) {
				continue
			}
			if params.Label != "" {
				if eq := strings.Index(params.Label, "="); eq > 0 {
					key := params.Label[:eq]
					value := params.Label[eq+1:]
					if g.Labels == nil || g.Labels[key] != value {
						continue
					}
				}
			}
			matchIDs[g.ID] = true
		}
	}

	var filtered []model.TimelineSegment
	for _, seg := range all {
		if matchIDs[seg.GoroutineID] {
			filtered = append(filtered, seg)
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleProcessorTimeline(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.engineFor(r).ProcessorTimeline())
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "contention" {
		contention := s.engineFor(r).ResourceContention()
		// Sort by peak waiters descending
		sort.Slice(contention, func(i, j int) bool {
			return contention[i].PeakWaiters > contention[j].PeakWaiters
		})
		writeJSON(w, http.StatusOK, map[string]any{"contention": contention})
		return
	}
	writeJSON(w, http.StatusOK, s.engineFor(r).ResourceGraph())
}

func (s *Server) handleDeadlockHints(w http.ResponseWriter, r *http.Request) {
	goroutines := s.engineFor(r).ListGoroutines()
	edges := s.engineFor(r).ResourceGraph()
	if len(edges) == 0 {
		edges = analysis.DeriveCurrentContentionEdges(goroutines)
	}

	hints := analysis.FindDeadlockHints(edges, goroutines)
	writeJSON(w, http.StatusOK, map[string]any{
		"hints": hints,
	})
}

// handleMemoryStats returns the current in-memory data volumes and the
// configured retention policy.
// GET /api/v1/memory
func (s *Server) handleMemoryStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.engineFor(r).MemoryStats())
}

// handleRequests returns request groups (H-4 / G-5).
// GET /api/v1/requests
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	groups := s.engineFor(r).GroupByRequest()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"groups": groups,
		"total":  len(groups),
	})
}

// handleRequestGoroutines returns the goroutines belonging to a request group.
// GET /api/v1/requests/{id}/goroutines
func (s *Server) handleRequestGoroutines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reqID := r.PathValue("id")
	if reqID == "" {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	groups := s.engineFor(r).GroupByRequest()
	for _, g := range groups {
		if g.RequestID == reqID {
			all := s.engineFor(r).ListGoroutines()
			idSet := make(map[int64]bool, len(g.GoroutineIDs))
			for _, id := range g.GoroutineIDs {
				idSet[id] = true
			}
			result := make([]model.Goroutine, 0, len(g.GoroutineIDs))
			for _, goroutine := range all {
				if idSet[goroutine.ID] {
					result = append(result, goroutine)
				}
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"goroutines": result,
				"total":      len(result),
			})
			return
		}
	}
	http.Error(w, "request group not found", http.StatusNotFound)
}

// handleMetrics serves Prometheus text exposition format (H-5).
// GET /metrics — compatible with prometheus scrape_config, no dependencies.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	goroutines := s.engineFor(r).ListGoroutines()
	stateCounts := make(map[model.GoroutineState]int)
	for _, g := range goroutines {
		stateCounts[g.State]++
	}

	edges := s.engineFor(r).ResourceGraph()
	if len(edges) == 0 {
		edges = analysis.DeriveCurrentContentionEdges(goroutines)
	}
	hints := analysis.FindDeadlockHints(edges, goroutines)

	const leakThresholdNS = 30_000_000_000 // 30 s
	leaks := s.engineFor(r).LeakCandidates(leakThresholdNS)

	mem := s.engineFor(r).MemoryStats()

	var sessionDurationSecs float64
	if sess := s.engineFor(r).CurrentSession(); sess != nil && !sess.StartedAt.IsZero() {
		sessionDurationSecs = time.Since(sess.StartedAt).Seconds()
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	allStates := []model.GoroutineState{
		model.StateRunning, model.StateRunnable, model.StateWaiting,
		model.StateBlocked, model.StateSyscall, model.StateDone,
	}
	fmt.Fprintf(w, "# HELP goroscope_goroutines_total Number of goroutines by state.\n")
	fmt.Fprintf(w, "# TYPE goroscope_goroutines_total gauge\n")
	for _, state := range allStates {
		fmt.Fprintf(w, "goroscope_goroutines_total{state=%q} %d\n", state, stateCounts[state])
	}

	fmt.Fprintf(w, "# HELP goroscope_deadlock_hints_total Number of potential deadlock cycles detected.\n")
	fmt.Fprintf(w, "# TYPE goroscope_deadlock_hints_total gauge\n")
	fmt.Fprintf(w, "goroscope_deadlock_hints_total %d\n", len(hints))

	fmt.Fprintf(w, "# HELP goroscope_leak_candidates_total Goroutines blocked longer than 30s.\n")
	fmt.Fprintf(w, "# TYPE goroscope_leak_candidates_total gauge\n")
	fmt.Fprintf(w, "goroscope_leak_candidates_total %d\n", len(leaks))

	fmt.Fprintf(w, "# HELP goroscope_closed_segments_total Closed timeline segments retained in memory.\n")
	fmt.Fprintf(w, "# TYPE goroscope_closed_segments_total gauge\n")
	fmt.Fprintf(w, "goroscope_closed_segments_total %d\n", mem.ClosedSegments)

	fmt.Fprintf(w, "# HELP goroscope_stack_snapshots_total Stack snapshots retained in memory.\n")
	fmt.Fprintf(w, "# TYPE goroscope_stack_snapshots_total gauge\n")
	fmt.Fprintf(w, "goroscope_stack_snapshots_total %d\n", mem.StackSnapshots)

	fmt.Fprintf(w, "# HELP goroscope_session_duration_seconds Seconds since the current session started.\n")
	fmt.Fprintf(w, "# TYPE goroscope_session_duration_seconds gauge\n")
	fmt.Fprintf(w, "goroscope_session_duration_seconds %.3f\n", sessionDurationSecs)
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

	current := s.sessionsFor(r).StartSession("replay", "upload.gtrace")
	capture = tracebridge.BindCaptureSession(capture, current.ID)
	s.engineFor(r).LoadCapture(current, capture)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "session_id": current.ID})
}

// handleReplayExport serialises the current engine state as a .gtrace file download.
// GET /api/v1/replay/export
// Returns 404 when no session is active, 204 when the session has no goroutines yet.
func (s *Server) handleReplayExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	current := s.sessionsFor(r).Current()
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	capture := s.engineFor(r).ExportCapture()
	if len(capture.Events) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	data, err := json.Marshal(capture)
	if err != nil {
		http.Error(w, fmt.Sprintf("encode capture: %v", err), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("session-%s.gtrace", current.ID)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleCompare accepts two .gtrace files and returns baseline/compare data plus diff.
// POST /api/v1/compare with multipart form fields "file_a" (baseline) and "file_b" (compare).
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
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

	captureA, err := readCaptureFormFile(r, "file_a")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	captureB, err := readCaptureFormFile(r, "file_b")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess := &model.Session{ID: "compare", Name: "compare", Status: model.SessionStatusRunning, StartedAt: time.Now()}
	engA := analysis.NewEngine()
	engB := analysis.NewEngine()
	engA.LoadCapture(sess, tracebridge.BindCaptureSession(captureA, "baseline"))
	engB.LoadCapture(sess, tracebridge.BindCaptureSession(captureB, "compare"))

	goroutinesA := engA.ListGoroutines()
	goroutinesB := engB.ListGoroutines()
	timelineA := engA.Timeline()
	timelineB := engB.Timeline()

	diff := analysis.ComputeCaptureDiff(goroutinesA, timelineA, goroutinesB, timelineB)

	writeJSON(w, http.StatusOK, map[string]any{
		"baseline": map[string]any{
			"goroutines": goroutinesA,
			"timeline":   timelineA,
		},
		"compare": map[string]any{
			"goroutines": goroutinesB,
			"timeline":   timelineB,
		},
		"diff": diff,
	})
}

// handleCompareStacks computes a stack-pattern diff between two captures (I-9).
// POST /api/v1/compare/stacks with multipart fields "file_a" (baseline) and
// "file_b" (compare). Returns appeared/disappeared/common_count.
func (s *Server) handleCompareStacks(w http.ResponseWriter, r *http.Request) {
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

	captureA, err := readCaptureFormFile(r, "file_a")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	captureB, err := readCaptureFormFile(r, "file_b")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result := analysis.StackPatternDiff(captureA, captureB)
	writeJSON(w, http.StatusOK, result)
}

// readCaptureFormFile reads a multipart form file field and parses it as a
// capture. Returns a descriptive error for missing or invalid files.
func readCaptureFormFile(r *http.Request, field string) (model.Capture, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return model.Capture{}, fmt.Errorf("missing or invalid %s field: %w", field, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return model.Capture{}, fmt.Errorf("read %s: %w", field, err)
	}
	cap, err := tracebridge.LoadCaptureFromBytes(data)
	if err != nil {
		return model.Capture{}, fmt.Errorf("invalid capture %s: %w", field, err)
	}
	return cap, nil
}

// handleStream implements a Server-Sent Events (SSE) endpoint with delta
// streaming (H-6). Each "update" event carries a GoroutineDelta JSON payload
// describing only what changed since the client's last revision, so the client
// does not need to re-fetch the full goroutine list on every tick.
//
// Protocol:
//   - Client sends Last-Event-ID: <revision> to resume from a known revision.
//   - Revision 0 (or missing header) triggers a full snapshot in Added.
//   - Each SSE event includes an "id: <revision>" line so browsers auto-resume.
//   - When added/updated/removed are all empty the event is suppressed entirely,
//     keeping bandwidth near zero when nothing changes.
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

	// Parse client's last-known revision from the SSE resume header.
	var clientRevision uint64
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			clientRevision = n
		}
	}

	eng := s.engineFor(r)
	ch := eng.Subscribe()
	defer eng.Unsubscribe(ch)

	// Send the initial delta immediately (full snapshot when clientRevision==0).
	s.sendSSEDelta(w, flusher, eng, clientRevision)
	clientRevision = eng.DataVersion()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			s.sendSSEDelta(w, flusher, eng, clientRevision)
			clientRevision = eng.DataVersion()
		}
	}
}

// sendSSEDelta fetches a StreamDelta since revision, serialises it and
// writes one SSE event. Empty deltas are suppressed to save bandwidth.
func (s *Server) sendSSEDelta(w http.ResponseWriter, flusher http.Flusher, eng *analysis.Engine, revision uint64) {
	delta := eng.DeltaSince(revision)
	if len(delta.Added) == 0 && len(delta.Updated) == 0 && len(delta.Removed) == 0 && revision != 0 {
		return
	}
	data, err := json.Marshal(delta)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "id: %d\nevent: update\ndata: %s\n\n", delta.Revision, data)
	flusher.Flush()
}

// handleTargets handles GET /api/v1/targets and POST /api/v1/targets (H-7).
//
// GET  — returns the list of all registered targets.
// POST — adds a new target; body: {"addr":"http://localhost:6060","label":"svc"}.
func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.registry == nil {
			writeJSON(w, http.StatusOK, []target.Info{})
			return
		}
		writeJSON(w, http.StatusOK, s.registry.List())

	case http.MethodPost:
		if s.registry == nil {
			writeJSON(w, http.StatusServiceUnavailable,
				map[string]string{"error": "multi-target mode not enabled; start goroscope with --target flags"})
			return
		}
		var body struct {
			Addr  string `json:"addr"`
			Label string `json:"label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if body.Addr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "addr is required"})
			return
		}
		parentCtx := s.serveCtx
		if parentCtx == nil {
			parentCtx = r.Context()
		}
		t := s.registry.Add(parentCtx, target.AddInput{Addr: body.Addr, Label: body.Label})
		writeJSON(w, http.StatusCreated, target.Info{
			ID:      t.ID,
			Addr:    t.Addr,
			Label:   t.Label,
			AddedAt: t.AddedAt,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTargetByID handles DELETE /api/v1/targets/{id} (H-7).
func (s *Server) handleTargetByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing target id"})
		return
	}
	if s.registry == nil || !s.registry.Remove(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleOpenAPISpec serves the embedded OpenAPI 3.1 specification as YAML (I-1).
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	data, err := openapiYAML.ReadFile("openapi.yaml")
	if err != nil {
		http.Error(w, "openapi spec not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(data)
}

// handleSwaggerUI serves a minimal Swagger UI page that loads the spec from
// /api/openapi.yaml. Uses the official Swagger UI CDN so no JS bundle is
// embedded in the binary (I-1).
func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	specURL := "http://" + r.Host + "/api/openapi.yaml"
	html := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Goroscope API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "` + specURL + `",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
    });
  </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, html)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
