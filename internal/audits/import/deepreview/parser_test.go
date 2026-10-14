package deepreview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/audits"
)

func TestParseDir(t *testing.T) {
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

	bundle, err := ParseDir(root, "nanite", audits.Provenance{
		ActorKind: "importer",
		ActorID:   "stack-explorer-cli",
		ModelName: "gpt-5.5",
	})
	if err != nil {
		t.Fatalf("parse dir: %v", err)
	}
	if bundle.Audit.Scope != "example-scope" {
		t.Fatalf("scope = %q, want example-scope", bundle.Audit.Scope)
	}
	if len(bundle.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(bundle.Findings))
	}
	finding := bundle.Findings[0]
	if finding.Severity != "critical" {
		t.Fatalf("severity = %q, want critical", finding.Severity)
	}
	if finding.Description != "The parser should preserve the first problem paragraph." {
		t.Fatalf("description = %q", finding.Description)
	}
	if len(finding.CodeRefs) != 1 || finding.CodeRefs[0].FilePath != "internal/chat/engine.go" {
		t.Fatalf("code refs = %#v", finding.CodeRefs)
	}
	if len(finding.Themes) != 1 || finding.Themes[0].Name != "Security" {
		t.Fatalf("themes = %#v", finding.Themes)
	}
	if bundle.Audit.Provenance.ModelName != "gpt-5.5" {
		t.Fatalf("audit model name = %q, want gpt-5.5", bundle.Audit.Provenance.ModelName)
	}
	if finding.Provenance.ModelName != "gpt-5.5" {
		t.Fatalf("finding model name = %q, want gpt-5.5", finding.Provenance.ModelName)
	}
}
