package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestListJobsEndpoint(t *testing.T) {
	dbDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()
	if err := store.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	srv := NewServer(store)

	job := &domain.Job{
		ID:          "job-1",
		RepoID:      "stack-explorer",
		Kind:        "git-history-sweep",
		Status:      "completed",
		PayloadJSON: "{}",
		OutputJSON:  "{}",
	}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jobs?repo=stack-explorer&status=completed&kind=git-history-sweep", nil)
	rec := httptest.NewRecorder()
	srv.buildRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"job-1", "git-history-sweep", "completed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("jobs response missing %q: %s", want, body)
		}
	}
}
