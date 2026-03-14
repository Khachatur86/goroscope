package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/session"
)

type Server struct {
	addr     string
	engine   *analysis.Engine
	sessions *session.Manager
}

func NewServer(addr string, engine *analysis.Engine, sessions *session.Manager) *Server {
	return &Server{
		addr:     addr,
		engine:   engine,
		sessions: sessions,
	}
}

func (s *Server) Serve(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:    s.addr,
		Handler: s.routes(),
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
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/session/current", s.handleSessionCurrent)
	mux.HandleFunc("/api/v1/goroutines", s.handleGoroutines)
	mux.HandleFunc("/api/v1/goroutines/", s.handleGoroutineByID)
	mux.HandleFunc("/api/v1/timeline", s.handleTimeline)
	mux.HandleFunc("/api/v1/resources/graph", s.handleGraph)
	mux.HandleFunc("/api/v1/stream", s.handleStream)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Goroscope Scaffold</title>
  <style>
    body { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; margin: 40px; background: #0f172a; color: #e2e8f0; }
    .card { max-width: 860px; padding: 24px; background: #111827; border: 1px solid #334155; border-radius: 16px; }
    code, pre { color: #93c5fd; }
    a { color: #f59e0b; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Goroscope Scaffold</h1>
    <p>This is the starter HTTP surface for the Goroscope MVP.</p>
    <p>Available endpoints:</p>
    <pre>/healthz
/api/v1/session/current
/api/v1/goroutines
/api/v1/goroutines/{id}
/api/v1/timeline
/api/v1/resources/graph</pre>
    <p>The full frontend will live under <code>web/</code>. For now, use the JSON endpoints to verify the scaffold.</p>
  </div>
</body>
</html>`)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSessionCurrent(w http.ResponseWriter, _ *http.Request) {
	current := s.sessions.Current()
	if current == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	writeJSON(w, http.StatusOK, current)
}

func (s *Server) handleGoroutines(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ListGoroutines())
}

func (s *Server) handleGoroutineByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/goroutines/"), 10, 64)
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

func (s *Server) handleTimeline(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Timeline())
}

func (s *Server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ResourceGraph())
}

func (s *Server) handleStream(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "stream endpoint is not implemented in the scaffold yet",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
