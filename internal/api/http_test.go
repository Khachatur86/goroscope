package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLocalhostAddr(t *testing.T) {
	tests := []struct {
		addr   string
		expect bool
	}{
		{"127.0.0.1:7070", true},
		{"[::1]:7070", true},
		{"localhost:7070", true},
		{"0.0.0.0:7070", false},
		{"192.168.1.1:7070", false},
		{"", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		got := isLocalhostAddr(tt.addr)
		if got != tt.expect {
			t.Errorf("isLocalhostAddr(%q) = %v, want %v", tt.addr, got, tt.expect)
		}
	}
}

func TestPprofOnlyWhenLocalhost(t *testing.T) {
	// When bound to localhost, /debug/pprof/ should return 200.
	s := NewServer("127.0.0.1:0", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /debug/pprof/ on localhost: got %d, want 200", rec.Code)
	}
}

func TestPprofDisabledWhenNotLocalhost(t *testing.T) {
	// When bound to 0.0.0.0, /debug/pprof/ should 404 (not registered).
	s := NewServer("0.0.0.0:7070", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /debug/pprof/ on 0.0.0.0: got %d, want 404", rec.Code)
	}
}
