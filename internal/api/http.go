package api

import (
	"context"
	"encoding/json"
	"errors"
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
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(uiFileSystem()))))
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

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	serveEmbeddedFile(w, "index.html")
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
