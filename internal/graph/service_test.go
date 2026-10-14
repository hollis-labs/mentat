package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

func TestNeighborsUsesCTEForShallowDepth(t *testing.T) {
	store := openGraphStore(t)
	defer store.Close()
	repoID, rootID, childID, leafID := seedGraphFixture(t, store)

	svc := NewService(store)
	got, err := svc.Neighbors(context.Background(), rootID, Filter{RepoID: repoID, Depth: 2})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(neighbors) = %d, want 2", len(got))
	}
	if got[0].SymbolID != childID || got[0].Depth != 1 {
		t.Fatalf("first neighbor = %#v", got[0])
	}
	if got[1].SymbolID != leafID || got[1].Depth != 2 {
		t.Fatalf("second neighbor = %#v", got[1])
	}
}

func TestNeighborsUsesBFSForDeepDepth(t *testing.T) {
	store := openGraphStore(t)
	defer store.Close()
	repoID, rootID, _, leafID := seedGraphFixture(t, store)
	node4 := seedExtraSymbol(t, store, repoID, "node4")
	node5 := seedExtraSymbol(t, store, repoID, "node5")
	mustUpsertRelationship(t, store, repoID, leafID, node4, "calls", 0.7, "scip")
	mustUpsertRelationship(t, store, repoID, node4, node5, "calls", 0.6, "scip")

	svc := NewService(store)
	got, err := svc.Neighbors(context.Background(), rootID, Filter{RepoID: repoID, Depth: 5})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(neighbors) = %d, want 4", len(got))
	}
	if got[len(got)-1].SymbolID != node5 || got[len(got)-1].Depth != 4 {
		t.Fatalf("last neighbor = %#v", got[len(got)-1])
	}
}

func BenchmarkNeighborsDepth2LargeRepo(b *testing.B) {
	store := openGraphStoreBenchmark(b)
	defer store.Close()
	repoID, rootID, childID, leafID := seedGraphFixtureBenchmark(b, store)
	for i := 0; i < 49997; i++ {
		seedExtraSymbolBenchmark(b, store, repoID, "bulk-symbol-"+itoaBenchmark(i))
	}
	mustUpsertRelationshipBenchmark(b, store, repoID, leafID, seedExtraSymbolBenchmark(b, store, repoID, "neighbor-4"), "references", 1.0, "scip")
	mustUpsertRelationshipBenchmark(b, store, repoID, childID, seedExtraSymbolBenchmark(b, store, repoID, "neighbor-5"), "references", 1.0, "scip")

	svc := NewService(store)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := svc.Neighbors(context.Background(), rootID, Filter{RepoID: repoID, Depth: 2})
		if err != nil {
			b.Fatalf("neighbors: %v", err)
		}
		if len(items) < 2 {
			b.Fatalf("len(neighbors) = %d, want at least 2", len(items))
		}
	}
}

func openGraphStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

func seedGraphFixture(t *testing.T, store *sqlite.Store) (string, int64, int64, int64) {
	t.Helper()
	repoID := "stack-explorer"
	if err := store.CreateRepo(&domain.Repo{ID: repoID, Name: "Stack Explorer"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	rootID := seedExtraSymbol(t, store, repoID, "root")
	childID := seedExtraSymbol(t, store, repoID, "child")
	leafID := seedExtraSymbol(t, store, repoID, "leaf")
	mustUpsertRelationship(t, store, repoID, rootID, childID, "calls", 1.0, "scip")
	mustUpsertRelationship(t, store, repoID, childID, leafID, "calls", 0.5, "scip")
	return repoID, rootID, childID, leafID
}

func seedExtraSymbol(t *testing.T, store *sqlite.Store, repoID, name string) int64 {
	t.Helper()
	lineStart := 1
	lineEnd := 2
	sym := &model.Symbol{
		RepoID:        repoID,
		Kind:          "function",
		Name:          name,
		QualifiedName: "example/" + name,
		FilePath:      name + ".go",
		LineStart:     &lineStart,
		LineEnd:       &lineEnd,
		ContentHash:   name + "-content",
		SignatureHash: name + "-sig",
		Language:      "go",
	}
	if err := store.UpsertSymbol(sym); err != nil {
		t.Fatalf("upsert symbol %s: %v", name, err)
	}
	return sym.ID
}

func mustUpsertRelationship(t *testing.T, store *sqlite.Store, repoID string, src, dst int64, kind string, weight float64, source string) {
	t.Helper()
	if err := store.UpsertRelationship(&domain.Relationship{
		RepoID:       repoID,
		SrcSymbolID:  src,
		DstSymbolID:  dst,
		Kind:         kind,
		Weight:       weight,
		Source:       source,
		DiscoveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert relationship: %v", err)
	}
}

func openGraphStoreBenchmark(b *testing.B) *sqlite.Store {
	b.Helper()
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "graph.db"))
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	return store
}

func seedGraphFixtureBenchmark(b *testing.B, store *sqlite.Store) (string, int64, int64, int64) {
	b.Helper()
	repoID := "stack-explorer"
	if err := store.CreateRepo(&domain.Repo{ID: repoID, Name: "Stack Explorer"}); err != nil {
		b.Fatalf("create repo: %v", err)
	}
	rootID := seedExtraSymbolBenchmark(b, store, repoID, "root")
	childID := seedExtraSymbolBenchmark(b, store, repoID, "child")
	leafID := seedExtraSymbolBenchmark(b, store, repoID, "leaf")
	mustUpsertRelationshipBenchmark(b, store, repoID, rootID, childID, "references", 1.0, "scip")
	mustUpsertRelationshipBenchmark(b, store, repoID, childID, leafID, "references", 1.0, "scip")
	return repoID, rootID, childID, leafID
}

func seedExtraSymbolBenchmark(b *testing.B, store *sqlite.Store, repoID, name string) int64 {
	b.Helper()
	lineStart := 1
	lineEnd := 2
	sym := &model.Symbol{
		RepoID:        repoID,
		Kind:          "function",
		Name:          name,
		QualifiedName: "example/" + name,
		FilePath:      name + ".go",
		LineStart:     &lineStart,
		LineEnd:       &lineEnd,
		ContentHash:   name + "-content",
		SignatureHash: name + "-sig",
		Language:      "go",
	}
	if err := store.UpsertSymbol(sym); err != nil {
		b.Fatalf("upsert symbol %s: %v", name, err)
	}
	return sym.ID
}

func mustUpsertRelationshipBenchmark(b *testing.B, store *sqlite.Store, repoID string, src, dst int64, kind string, weight float64, source string) {
	b.Helper()
	if err := store.UpsertRelationship(&domain.Relationship{
		RepoID:       repoID,
		SrcSymbolID:  src,
		DstSymbolID:  dst,
		Kind:         kind,
		Weight:       weight,
		Source:       source,
		DiscoveredAt: time.Now().UTC(),
	}); err != nil {
		b.Fatalf("upsert relationship: %v", err)
	}
}

func itoaBenchmark(v int) string {
	return fmt.Sprintf("%d", v)
}
