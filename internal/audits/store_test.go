package audits_test

import (
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/audits"
)

func TestReplaceImportedAuditAndDiff(t *testing.T) {
	store := openTestStore(t)
	auditStore := audits.NewStore(store.DB())

	refA := "ref-a"
	refB := "ref-b"
	baseTime := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)

	first := audits.ImportBundle{
		Audit: &audits.Audit{
			RepoID:       "nanite",
			Scope:        "example-scope",
			AuditType:    "deep-review",
			Auditor:      "nanite-reviewer",
			Status:       "completed",
			Provenance:   audits.Provenance{ActorKind: "importer", ActorID: "stack-explorer-cli"},
			StartedAt:    baseTime,
			FinishedAt:   &baseTime,
			AuditedAtRef: &refA,
		},
		Findings: []audits.Finding{
			{
				Finding: audits.Finding{}.Finding,
			},
		},
	}
	repoID := "nanite"
	first.Findings[0].RepoID = &repoID
	first.Findings[0].Title = "Shared provider callback race"
	first.Findings[0].Category = "risk"
	first.Findings[0].Severity = "high"
	first.Findings[0].Description = "First import"
	first.Findings[0].Status = "open"
	first.Findings[0].BodyMarkdown = "# [High] Shared provider callback race"
	first.Findings[0].Provenance = audits.Provenance{ActorKind: "importer", ActorID: "stack-explorer-cli"}
	first.Findings[0].Themes = []audits.Theme{{Name: "Concurrency"}}

	second := first
	second.Audit = &audits.Audit{
		RepoID:       "nanite",
		Scope:        "example-scope",
		AuditType:    "deep-review",
		Auditor:      "nanite-reviewer",
		Status:       "completed",
		Provenance:   audits.Provenance{ActorKind: "importer", ActorID: "stack-explorer-cli"},
		StartedAt:    baseTime.Add(24 * time.Hour),
		FinishedAt:   timePtr(baseTime.Add(24 * time.Hour)),
		AuditedAtRef: &refB,
	}
	second.Findings = []audits.Finding{first.Findings[0]}
	second.Findings[0].Severity = "critical"

	bundleA, err := auditStore.ReplaceImportedAudit(first)
	if err != nil {
		t.Fatalf("replace first audit: %v", err)
	}
	bundleB, err := auditStore.ReplaceImportedAudit(second)
	if err != nil {
		t.Fatalf("replace second audit: %v", err)
	}
	if len(bundleA.Findings) != 1 || len(bundleB.Findings) != 1 {
		t.Fatalf("unexpected finding counts: %d %d", len(bundleA.Findings), len(bundleB.Findings))
	}

	diff, err := audits.DiffBundles(bundleA, bundleB)
	if err != nil {
		t.Fatalf("diff bundles: %v", err)
	}
	if len(diff.Regressed) != 1 {
		t.Fatalf("regressed = %d, want 1", len(diff.Regressed))
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
