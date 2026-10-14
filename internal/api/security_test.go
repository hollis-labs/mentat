package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestServeOptionsValidate(t *testing.T) {
	cases := []struct {
		name    string
		opts    ServeOptions
		wantErr bool
	}{
		{"default host is loopback", ServeOptions{Port: 1}, false},
		{"ipv4 loopback", ServeOptions{Host: "127.0.0.1"}, false},
		{"ipv6 loopback", ServeOptions{Host: "::1"}, false},
		{"localhost", ServeOptions{Host: "localhost"}, false},
		{"all interfaces without token", ServeOptions{Host: "0.0.0.0"}, true},
		{"all interfaces with token", ServeOptions{Host: "0.0.0.0", Token: "t"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.opts.validate(); (err != nil) != tc.wantErr {
				t.Fatalf("validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
	if got := (ServeOptions{Port: 8080}).addr(); got != "127.0.0.1:8080" {
		t.Fatalf("addr() = %q, want 127.0.0.1:8080", got)
	}
}

func TestRequireToken(t *testing.T) {
	store := newTestStore(t)
	srv := NewServer(store)
	srv.opts = ServeOptions{Token: "secret"}
	srv.router = srv.buildRouter()

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer nope", http.StatusUnauthorized},
		{"correct", "Bearer secret", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			srv.router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestCORSDefaultsToFrontendOrigin(t *testing.T) {
	srv := NewServer(newTestStore(t))
	for origin, allowed := range map[string]bool{
		"http://localhost:3334": true,
		"https://evil.example":  false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)
		got := rec.Header().Get("Access-Control-Allow-Origin")
		if allowed && got != origin {
			t.Fatalf("origin %s: Allow-Origin = %q, want it echoed", origin, got)
		}
		if !allowed && got != "" {
			t.Fatalf("origin %s: Allow-Origin = %q, want none", origin, got)
		}
	}
}

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
