package audits_test

import (
	"os"
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

func writeAuditFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte(`# Example scope — deep review

**Date:** 2026-04-10
**Scope:** `+"`example-scope`"+`
**Reviewer agent:** `+"`nanite-reviewer`"+`
**Skill:** `+"`deep-review`"+`

## Scope

Fixture summary.

## Findings

### By severity

- [01 — Example critical issue](01-critical-example-critical-issue.md)

### By topic

**Security**
- [01 — Example critical issue](01-critical-example-critical-issue.md)
`), 0o644); err != nil {
		t.Fatalf("write index fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "01-critical-example-critical-issue.md"), []byte(`# [Critical] Example critical issue

**Scope:** example-scope

## Problem

The parser should preserve the first problem paragraph.

## References

- `+"`internal/chat/engine.go:L10-L14`"+`
`), 0o644); err != nil {
		t.Fatalf("write finding fixture: %v", err)
	}
	return root
}
