package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	libauth "github.com/hollis-labs/libs/security/auth"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// Every route the API serves has a scope in the table the guard enforces: reads need mentat.read,
// changes mentat.write, and the whole-database download mentat.admin. The mux and the table are
// built from one route list, so this fails if either side stops using it.
func TestEveryRouteHasItsScope(t *testing.T) {
	srv := NewServer(newTestStore(t), Deps{})
	routes := srv.routes()
	if len(routes) == 0 {
		t.Fatal("no routes")
	}
	table := libauth.NewScopeTable()
	srv.RegisterScopes(table)
	for _, rt := range routes {
		concrete := strings.ReplaceAll(rt.Pattern, "{id}", "x")
		got, known := table.ForRoute(rt.Method, concrete)
		want := ScopeWrite
		switch {
		case rt.Pattern == "/api/db/backup":
			want = ScopeAdmin
		case rt.Method == http.MethodGet:
			want = ScopeRead
		}
		if !known || got != want {
			t.Errorf("%s %s: scope=%q known=%v, want %q", rt.Method, concrete, got, known, want)
		}
		if !strings.HasPrefix(rt.Pattern, "/api/") {
			t.Errorf("%s %s is outside /api/, where the guard does not look", rt.Method, rt.Pattern)
		}
	}
	// A route nobody declared is unmapped, which the guard refuses in enforce mode.
	if _, known := table.ForRoute("DELETE", "/api/search"); known {
		t.Error("DELETE /api/search is mapped but not declared")
	}
}

// Errors use the flat {code, message} body.
func TestErrorBodyIsFlat(t *testing.T) {
	srv := NewServer(newTestStore(t), Deps{})
	rec := doRequest(srv.Handler(), http.MethodGet, "/api/repos/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"not_found"`) || !strings.Contains(body, `"message"`) || strings.Contains(body, `"error"`) {
		t.Fatalf("body = %s", body)
	}
}

func doRequest(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
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
