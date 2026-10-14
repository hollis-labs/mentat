//go:build treesitter

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

func TestParseGoFile(t *testing.T) {
	path := filepath.Join("testdata", "go", "sample.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	symbols, err := ParseGoFile("stack-explorer", "testdata/go/sample.go", src)
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}
	if len(symbols) != 5 {
		t.Fatalf("len(symbols) = %d, want 5", len(symbols))
	}

	got := map[string]string{}
	for _, sym := range symbols {
		got[sym.QualifiedName] = sym.Kind
		if sym.ContentHash == "" || sym.SignatureHash == "" {
			t.Fatalf("missing hashes for %#v", sym)
		}
	}

	expect := map[string]string{
		"demo.Store":      "type",
		"demo.Version":    "const",
		"demo.CreateRepo": "function",
		"demo.Store.Save": "method",
	}
	for qualifiedName, kind := range expect {
		if got[qualifiedName] != kind {
			t.Fatalf("symbol %q = %q, want %q", qualifiedName, got[qualifiedName], kind)
		}
	}
	if got["demo.global"] != "var" {
		t.Fatalf("symbol %q = %q, want %q", "demo.global", got["demo.global"], "var")
	}
}

func TestGoIngesterNoOpOnUnchangedRepo(t *testing.T) {
	repoPath := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "symbols.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	if err := store.CreateRepo(&domain.Repo{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	source := "package demo\n\ntype Store struct{}\n\nfunc CreateRepo() {}\n\nfunc (s *Store) Save() {}\n"
	if err := os.WriteFile(filepath.Join(repoPath, "demo.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write demo.go: %v", err)
	}

	ingester := NewGoIngester(store)
	req := model.IngestRequest{
		RepoID:   "demo",
		RepoPath: repoPath,
	}

	first, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Inserted == 0 {
		t.Fatalf("first ingest inserted = %d, want > 0", first.Inserted)
	}

	second, err := ingester.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 0 || second.Drifted != 0 {
		t.Fatalf("second ingest = %#v, want zero-diff result", second)
	}
}
