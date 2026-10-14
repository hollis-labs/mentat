package api

import (
	"net/http"
	"strconv"

	"github.com/hollis-labs/mentat/internal/graph"
)

func (s *Server) listRelationships(w http.ResponseWriter, r *http.Request) {
	srcRaw := r.URL.Query().Get("src")
	if srcRaw == "" {
		writeError(w, http.StatusBadRequest, "src is required")
		return
	}
	srcID, err := strconv.ParseInt(srcRaw, 10, 64)
	if err != nil || srcID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid src")
		return
	}

	depth := 1
	if raw := r.URL.Query().Get("depth"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid depth")
			return
		}
		depth = parsed
	}

	items, err := graph.NewService(s.store).Neighbors(r.Context(), srcID, graph.Filter{
		RepoID: r.URL.Query().Get("repo"),
		Kind:   r.URL.Query().Get("kind"),
		Source: r.URL.Query().Get("source"),
		Depth:  depth,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, items, len(items), 1, len(items))
}
