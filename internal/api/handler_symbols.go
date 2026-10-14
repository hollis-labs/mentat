package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/symbols"
)

func (s *Server) getSymbol(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid symbol id")
		return
	}
	sym, err := s.store.GetSymbol(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sym == nil {
		writeError(w, http.StatusNotFound, "symbol not found")
		return
	}
	writeItem(w, sym)
}

func (s *Server) searchSymbols(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	items, err := s.store.SearchSymbols(symbols.SearchFilter{
		RepoID:   r.URL.Query().Get("repo"),
		Query:    r.URL.Query().Get("q"),
		Kind:     r.URL.Query().Get("kind"),
		Language: r.URL.Query().Get("lang"),
		Limit:    limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, items, len(items), 1, limit)
}
