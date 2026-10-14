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
	"github.com/hollis-labs/mentat/internal/symbols"
)

func TestRelationshipEndpoint(t *testing.T) {
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
	left := &symbols.Symbol{
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
	right := &symbols.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "method",
		Name:          "UpdateRepo",
		QualifiedName: "sqlite.Store.UpdateRepo",
		FilePath:      "internal/store/sqlite/repo.go",
		LineStart:     &line,
		LineEnd:       &line,
		ContentHash:   "body2",
		SignatureHash: "sig2",
		Language:      "go",
		Visibility:    "public",
	}
	if err := store.UpsertSymbol(left); err != nil {
		t.Fatalf("seed left symbol: %v", err)
	}
	if err := store.UpsertSymbol(right); err != nil {
		t.Fatalf("seed right symbol: %v", err)
	}
	if err := store.UpsertRelationship(&domain.Relationship{
		RepoID:       "stack-explorer",
		SrcSymbolID:  left.ID,
		DstSymbolID:  right.ID,
		Kind:         "references",
		Source:       "scip",
		Weight:       1,
		DiscoveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed relationship: %v", err)
	}

	srv := NewServer(store)

	req := httptest.NewRequest(http.MethodGet, "/api/relationships?src="+itoa(left.ID)+"&repo=stack-explorer&kind=references&depth=1", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("relationships status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body == "" || !strings.Contains(body, "sqlite.Store.UpdateRepo") {
		t.Fatalf("relationships response missing neighbor: %s", body)
	}
}
