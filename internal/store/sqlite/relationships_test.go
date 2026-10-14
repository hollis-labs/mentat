package sqlite_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestRelationshipsMigrationCreatesExpectedSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relationships.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	ok, err := store.TableExists("relationships")
	if err != nil {
		t.Fatalf("table exists: %v", err)
	}
	if !ok {
		t.Fatalf("relationships table missing")
	}

	assertIndex := func(name string) {
		t.Helper()
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatalf("lookup index %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("index %s missing", name)
		}
	}

	assertIndex("idx_relationships_repo_kind")
	assertIndex("idx_relationships_src_kind")
	assertIndex("idx_relationships_dst_kind")
	assertIndex("idx_relationships_source_kind")

	var createSQL string
	if err := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'relationships'`).Scan(&createSQL); err != nil {
		t.Fatalf("load relationships DDL: %v", err)
	}
	if createSQL == "" {
		t.Fatalf("relationships DDL missing")
	}
	if want := "UNIQUE(src_symbol_id, dst_symbol_id, kind, source)"; !strings.Contains(createSQL, want) {
		t.Fatalf("relationships DDL missing %q: %s", want, createSQL)
	}
}
