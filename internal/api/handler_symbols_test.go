package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols"
)

func TestSymbolEndpoints(t *testing.T) {
	dbDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	line := 12
	sym := &symbols.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "method",
		Name:          "CreateRepo",
		QualifiedName: "sqlite.Store.CreateRepo",
		FilePath:      "internal/store/sqlite/repo.go",
		LineStart:     &line,
		LineEnd:       &line,
		ContentHash:   "body",
		SignatureHash: "sig",
		Language:      "go",
		Visibility:    "public",
	}
	if err := store.UpsertSymbol(sym); err != nil {
		t.Fatalf("seed symbol: %v", err)
	}

	srv := NewServer(store)

	req := httptest.NewRequest(http.MethodGet, "/api/symbols/"+itoa(sym.ID), nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get symbol status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/symbols/search?q=CreateRepo&repo=stack-explorer", nil)
	rec = httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search symbols status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" || !contains(body, "sqlite.Store.CreateRepo") {
		t.Fatalf("search response missing symbol: %s", body)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
