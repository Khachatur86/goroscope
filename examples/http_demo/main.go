// Package main demonstrates agent.WithRequestID with a net/http handler.
// Run with: goroscope run ./examples/http_demo --open-browser
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/Khachatur86/goroscope/agent"
)

func main() {
	stopTrace, err := agent.StartFromEnv()
	if err != nil {
		log.Fatalf("start goroscope agent: %v", err)
	}
	defer func() {
		if err := stopTrace(); err != nil {
			log.Fatalf("stop goroscope agent: %v", err)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := agent.WithRequestID(r.Context(), r.Header.Get("X-Request-Id"))
		if id := agent.GetRequestID(ctx); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	})

	log.Println("http_demo listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
