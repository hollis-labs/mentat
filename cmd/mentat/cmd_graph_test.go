package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols"
)

func TestGraphCLI(t *testing.T) {
	dbDir := t.TempDir()
	dbPath = filepath.Join(dbDir, "graph.db")
	tempStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := tempStore.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	line := 12
	left := &symbols.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "function",
		Name:          "Left",
		QualifiedName: "stack-explorer.Left",
		FilePath:      "internal/left.go",
		LineStart:     &line,
		LineEnd:       &line,
		ContentHash:   "left-body",
		SignatureHash: "left-sig",
		Language:      "go",
	}
	right := &symbols.Symbol{
		RepoID:        "stack-explorer",
		Kind:          "function",
		Name:          "Right",
		QualifiedName: "stack-explorer.Right",
		FilePath:      "internal/right.go",
		LineStart:     &line,
		LineEnd:       &line,
		ContentHash:   "right-body",
		SignatureHash: "right-sig",
		Language:      "go",
	}
	if err := tempStore.UpsertSymbol(left); err != nil {
		t.Fatalf("seed left symbol: %v", err)
	}
	if err := tempStore.UpsertSymbol(right); err != nil {
		t.Fatalf("seed right symbol: %v", err)
	}
	now := time.Now().UTC()
	if err := tempStore.UpsertRelationship(&domain.Relationship{
		RepoID:       "stack-explorer",
		SrcSymbolID:  left.ID,
		DstSymbolID:  right.ID,
		Kind:         "references",
		Source:       "scip",
		Weight:       1,
		DiscoveredAt: now,
	}); err != nil {
		t.Fatalf("seed scip relationship: %v", err)
	}
	if err := tempStore.UpsertRelationship(&domain.Relationship{
		RepoID:       "stack-explorer",
		SrcSymbolID:  left.ID,
		DstSymbolID:  right.ID,
		Kind:         "co-changed-with",
		Source:       "git-history",
		Weight:       0.75,
		DiscoveredAt: now,
	}); err != nil {
		t.Fatalf("seed git-history relationship: %v", err)
	}
	_ = tempStore.Close()

	output := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"graph", "neighbors", "stack-explorer.Left", "--repo", "stack-explorer"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute neighbors: %v", err)
		}
		rootCmd.SetArgs([]string{"graph", "co-change", "stack-explorer.Left", "--repo", "stack-explorer"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute co-change: %v", err)
		}
	})

	for _, want := range []string{"stack-explorer.Right", "references", "co-changed-with", "git-history"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
