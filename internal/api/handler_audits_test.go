package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestGetAuditEndpoints(t *testing.T) {
	dbDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRepo(&domain.Repo{ID: "nanite", Name: "Nanite"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	auditStore := audits.NewStore(store.DB())
	repoID := "nanite"
	ref := "ref-a"
	bundle, err := auditStore.ReplaceImportedAudit(audits.ImportBundle{
		Audit: &audits.Audit{
			RepoID:       repoID,
			Scope:        "example-scope",
			AuditType:    "deep-review",
			Auditor:      "nanite-reviewer",
			Status:       "completed",
			Provenance:   audits.Provenance{ActorKind: "importer", ActorID: "stack-explorer-cli"},
			AuditedAtRef: &ref,
		},
		Findings: []audits.Finding{
			{
				Finding:      domain.Finding{RepoID: &repoID, Title: "Example critical issue", Category: "risk", Severity: "critical", Description: "desc", Status: "open"},
				BodyMarkdown: "# [Critical] Example critical issue",
				Provenance:   audits.Provenance{ActorKind: "importer", ActorID: "stack-explorer-cli"},
				Themes:       []audits.Theme{{Name: "Security"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	srv := NewServer(store)

	req := httptest.NewRequest(http.MethodGet, "/api/audits/"+itoa(bundle.Audit.ID), nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get audit status = %d body=%s", rec.Code, rec.Body.String())
	}

	var auditResp itemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &auditResp); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/audits/"+itoa(bundle.Audit.ID)+"/findings", nil)
	rec = httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get findings status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/audits/"+itoa(bundle.Audit.ID)+"/diff?against="+itoa(bundle.Audit.ID), nil)
	rec = httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("diff audit status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func itoa(id int64) string {
	return fmt.Sprintf("%d", id)
}
