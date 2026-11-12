package audits_test

import (
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := &domain.Repo{ID: "nanite", Name: "Nanite"}
	if err := store.CreateRepo(repo); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	return store
}
