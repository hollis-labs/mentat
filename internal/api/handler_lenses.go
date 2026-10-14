package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/domain"
)

type lensResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	DimensionCount int    `json:"dimension_count"`
}

func (s *Server) listLenses(w http.ResponseWriter, r *http.Request) {
	lenses, err := s.store.ListLenses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := make([]lensResponse, 0, len(lenses))
	for _, l := range lenses {
		results = append(results, lensResponse{
			ID: l.ID, Name: l.Name, Description: l.Description, DimensionCount: l.DimCount,
		})
	}
	writeList(w, results, len(results), 1, len(results))
}

func (s *Server) getLens(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	l, err := s.store.GetLens(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "lens not found")
		return
	}
	writeItem(w, lensResponse{
		ID: l.ID, Name: l.Name, Description: l.Description, DimensionCount: len(l.Dimensions),
	})
}

func (s *Server) createLens(w http.ResponseWriter, r *http.Request) {
	var l domain.Lens
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if l.ID == "" || l.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	if err := s.store.CreateLens(&l); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, lensResponse{ID: l.ID, Name: l.Name, Description: l.Description, DimensionCount: 0})
}

func (s *Server) updateLens(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check existence
	_, err := s.store.GetLens(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "lens not found")
		return
	}

	var l domain.Lens
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	_, err = s.store.DB().Exec("UPDATE lenses SET name=?, description=? WHERE id=?",
		l.Name, l.Description, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated, _ := s.store.GetLens(id)
	writeItem(w, lensResponse{ID: id, Name: updated.Name, Description: updated.Description, DimensionCount: len(updated.Dimensions)})
}

func (s *Server) deleteLens(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := s.store.DB().Exec("DELETE FROM lenses WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Scorecards ---

type scorecardResponse struct {
	ID       string  `json:"id"`
	RepoID   string  `json:"repo_id"`
	RepoName string  `json:"repo_name"`
	LensID   string  `json:"lens_id"`
	LensName string  `json:"lens_name"`
	Overall  float64 `json:"overall"`
	ScoredAt string  `json:"scored_at"`
}

type scorecardCreateRequest struct {
	RepoID string `json:"repo_id"`
	LensID string `json:"lens_id"`
	Notes  string `json:"notes"`
}

func (s *Server) createScorecard(w http.ResponseWriter, r *http.Request) {
	var req scorecardCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.RepoID == "" {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.store.DB().Exec("INSERT INTO scorecards (repo_id, lens_id, overall, notes, scored_at) VALUES (?, ?, 0, ?, ?)",
		req.RepoID, req.LensID, req.Notes, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := result.LastInsertId()

	var repoName, lensName string
	s.store.DB().QueryRow("SELECT name FROM repos WHERE id = ?", req.RepoID).Scan(&repoName)
	if req.LensID != "" {
		s.store.DB().QueryRow("SELECT name FROM lenses WHERE id = ?", req.LensID).Scan(&lensName)
	}

	writeCreated(w, scorecardResponse{
		ID: fmt.Sprintf("%d", id), RepoID: req.RepoID, RepoName: repoName,
		LensID: req.LensID, LensName: lensName, Overall: 0, ScoredAt: now,
	})
}

func (s *Server) listScorecards(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "overall", 25)
	if p.Direction == "ASC" && p.Sort == "overall" {
		p.Direction = "DESC"
	}

	baseQuery := `FROM scorecards sc
		JOIN repos r ON r.id = sc.repo_id
		LEFT JOIN lenses l ON l.id = sc.lens_id`

	var conditions []string
	var args []any
	if v, ok := p.Filters["repo_id"]; ok {
		conditions = append(conditions, "sc.repo_id = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["lens_id"]; ok {
		conditions = append(conditions, "sc.lens_id = ?")
		args = append(args, v)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			where += " AND " + c
		}
	}

	var total int
	if err := s.store.DB().QueryRow("SELECT COUNT(*) "+baseQuery+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sortMap := map[string]string{"overall": "sc.overall", "scored_at": "sc.scored_at", "repo_name": "r.name"}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "sc.overall"
		p.Direction = "DESC"
	}

	query := fmt.Sprintf(`SELECT sc.id, sc.repo_id, r.name, COALESCE(sc.lens_id,''), COALESCE(l.name,''), sc.overall, sc.scored_at %s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]scorecardResponse, 0)
	for rows.Next() {
		var sr scorecardResponse
		var id int64
		if err := rows.Scan(&id, &sr.RepoID, &sr.RepoName, &sr.LensID, &sr.LensName, &sr.Overall, &sr.ScoredAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sr.ID = fmt.Sprintf("%d", id)
		results = append(results, sr)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getScorecard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var sr scorecardResponse
	var dbID int64
	err := s.store.DB().QueryRow(`SELECT sc.id, sc.repo_id, r.name, COALESCE(sc.lens_id,''), COALESCE(l.name,''), sc.overall, sc.scored_at
		FROM scorecards sc JOIN repos r ON r.id = sc.repo_id LEFT JOIN lenses l ON l.id = sc.lens_id WHERE sc.id = ?`, id).
		Scan(&dbID, &sr.RepoID, &sr.RepoName, &sr.LensID, &sr.LensName, &sr.Overall, &sr.ScoredAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "scorecard not found")
		return
	}
	sr.ID = fmt.Sprintf("%d", dbID)
	writeItem(w, sr)
}
