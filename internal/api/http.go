package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
	mux.HandleFunc("/api/v1/goroutines/{id}", s.handleGoroutineByID)
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

func (s *Server) handleTimeline(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Timeline())
}

func (s *Server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ResourceGraph())
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
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: update\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
