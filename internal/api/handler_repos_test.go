package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestAddRepoTagsEndpoint(t *testing.T) {
	dbDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRepo(&domain.Repo{ID: "stack-explorer", Name: "Stack Explorer"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	srv := NewServer(store)
	body := bytes.NewBufferString(`{"tags":["go","sdk","go"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/repos/stack-explorer/tags", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add repo tags status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/repos/stack-explorer/", nil)
	rec = httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get repo status = %d body=%s", rec.Code, rec.Body.String())
	}
	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, `"tags":"go,sdk"`) && !strings.Contains(bodyText, `"tags":"sdk,go"`) {
		t.Fatalf("expected repo tags in response: %s", bodyText)
	}
}
