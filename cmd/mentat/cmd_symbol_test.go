package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols"
)

func TestSymbolSearchShowAndStatsCLI(t *testing.T) {
	dbDir := t.TempDir()
	dbPath = filepath.Join(dbDir, "symbols.db")
	tempStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := tempStore.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	line := 12
	sym := &symbols.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "function",
		Name:          "CreateRepo",
		QualifiedName: "stack-explorer.CreateRepo",
		FilePath:      "internal/store/sqlite/repo.go",
		LineStart:     &line,
		LineEnd:       &line,
		ContentHash:   "body",
		SignatureHash: "sig",
		Language:      "go",
		Visibility:    "public",
	}
	if err := tempStore.UpsertSymbol(sym); err != nil {
		t.Fatalf("seed symbol: %v", err)
	}
	_ = tempStore.Close()

	output := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"symbol", "search", "CreateRepo", "--repo", "stack-explorer"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute search: %v", err)
		}
		rootCmd.SetArgs([]string{"symbol", "show", "stack-explorer.CreateRepo", "--repo", "stack-explorer"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute show: %v", err)
		}
		rootCmd.SetArgs([]string{"symbol", "stats", "stack-explorer"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute stats: %v", err)
		}
	})

	for _, want := range []string{"CreateRepo", "Qualified Name: stack-explorer.CreateRepo", "Total:  1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
