package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// The framework store gives reads a small pool and writes one connection, so a handler that issues a
// statement while a cursor is open can wait for a connection its own cursor holds. sqlite.Open
// gives the test store ONE read connection; this serves every GET route against seeded data and
// fails any request that does not finish.
func TestEveryReadRouteFinishesWithOneReadConnection(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pool.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRepo(&domain.Repo{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(store, Deps{})
	h := srv.Handler()
	seen := 0
	for _, rt := range srv.routes() {
		if rt.Method != http.MethodGet || rt.Pattern == "/api/events" || rt.Pattern == "/api/db/backup" {
			continue // the event stream does not end by itself; the backup streams a file
		}
		path := strings.ReplaceAll(rt.Pattern, "{id}", "demo")
		if strings.Contains(path, "/dimensions/") || strings.Contains(path, "/lenses/") {
			path = strings.ReplaceAll(rt.Pattern, "{id}", "1")
		}
		done := make(chan int, 1)
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?q=demo&src=1", nil))
			done <- rec.Code
		}()
		select {
		case code := <-done:
			if code >= 500 {
				t.Errorf("GET %s: status %d", path, code)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("GET %s did not finish: a statement is waiting for a connection an open cursor holds", path)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("no routes were exercised")
	}
}
