package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/retrieval"
)

func (s *Server) searchKnowledge(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		} else {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
	}

	service := retrieval.NewService(s.store)
	results, err := service.Search(context.Background(), retrieval.SearchOptions{
		Query:  query,
		RepoID: r.URL.Query().Get("repo"),
		Kind:   r.URL.Query().Get("kind"),
		Limit:  limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, results, len(results), 1, limit)
}

func (s *Server) refreshRepoEmbeddings(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "id")
	var req struct {
		Force bool `json:"force"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	report, err := retrieval.RefreshRepoEmbeddings(context.Background(), s.store, repoID, req.Force)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeItem(w, report)
}
