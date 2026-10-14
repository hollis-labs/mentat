package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

func TestSearchReturnsLexicalSymbolResult(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := store.UpsertSymbol(&model.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "function",
		Name:          "CreateRepo",
		QualifiedName: "sqlite.Store.CreateRepo",
		FilePath:      "internal/store/sqlite/repo.go",
		ContentHash:   "abc123",
		SignatureHash: "sig123",
		Language:      "go",
	}); err != nil {
		t.Fatalf("upsert symbol: %v", err)
	}

	service := NewService(store)
	results, err := service.Search(context.Background(), SearchOptions{
		Query:  "CreateRepo",
		RepoID: "stack-explorer",
		Kind:   "symbol",
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].StableRef != "symbol:stack-explorer:sqlite.Store.CreateRepo" {
		t.Fatalf("unexpected top result: %+v", results[0])
	}
}

func TestRunEvalWritesBaseline(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := store.UpsertSymbol(&model.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "function",
		Name:          "CreateRepo",
		QualifiedName: "sqlite.Store.CreateRepo",
		FilePath:      "internal/store/sqlite/repo.go",
		ContentHash:   "abc123",
		SignatureHash: "sig123",
		Language:      "go",
	}); err != nil {
		t.Fatalf("upsert symbol: %v", err)
	}

	queryFile := filepath.Join(dir, "queries.yaml")
	baselineFile := filepath.Join(dir, "baseline.json")
	body := `queries:
  - name: create-repo
    query: CreateRepo
    repo_id: stack-explorer
    kind: symbol
    expected_refs:
      - symbol:stack-explorer:sqlite.Store.CreateRepo
`
	if err := os.WriteFile(queryFile, []byte(body), 0o644); err != nil {
		t.Fatalf("write queries: %v", err)
	}

	report, err := RunEval(context.Background(), store, queryFile, baselineFile)
	if err != nil {
		t.Fatalf("run eval: %v", err)
	}
	if report.Hits != 1 || report.Total != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := os.Stat(baselineFile); err != nil {
		t.Fatalf("baseline file missing: %v", err)
	}
}
