package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type comparisonSetResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SubjectCount   int    `json:"subject_count"`
	ReferenceCount int    `json:"reference_count"`
}

func (s *Server) listComparisonSets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().Query(`SELECT cs.id, cs.name,
		COALESCE((SELECT COUNT(*) FROM comparison_set_repos WHERE set_id = cs.id AND role = 'subject'), 0),
		COALESCE((SELECT COUNT(*) FROM comparison_set_repos WHERE set_id = cs.id AND role = 'reference'), 0)
		FROM comparison_sets cs ORDER BY cs.name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]comparisonSetResponse, 0)
	for rows.Next() {
		var cr comparisonSetResponse
		if err := rows.Scan(&cr.ID, &cr.Name, &cr.SubjectCount, &cr.ReferenceCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, cr)
	}
	writeList(w, results, len(results), 1, len(results))
}

func (s *Server) getComparisonSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var cr comparisonSetResponse
	err := s.store.DB().QueryRow(`SELECT cs.id, cs.name,
		COALESCE((SELECT COUNT(*) FROM comparison_set_repos WHERE set_id = cs.id AND role = 'subject'), 0),
		COALESCE((SELECT COUNT(*) FROM comparison_set_repos WHERE set_id = cs.id AND role = 'reference'), 0)
		FROM comparison_sets cs WHERE cs.id = ?`, id).
		Scan(&cr.ID, &cr.Name, &cr.SubjectCount, &cr.ReferenceCount)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "comparison set not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeItem(w, cr)
}

type comparisonSetCreateRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) createComparisonSet(w http.ResponseWriter, r *http.Request) {
	var req comparisonSetCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().Exec("INSERT INTO comparison_sets (id, name, description, created_at) VALUES (?, ?, ?, ?)",
		req.ID, req.Name, req.Description, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, comparisonSetResponse{ID: req.ID, Name: req.Name, SubjectCount: 0, ReferenceCount: 0})
}

func (s *Server) deleteComparisonSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.DB().Exec("DELETE FROM comparison_sets WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Reports ---

type reportResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LensID    string `json:"lens_id"`
	LensName  string `json:"lens_name"`
	RepoCount int    `json:"repo_count"`
	Audience  string `json:"audience"`
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "name", 25)

	baseQuery := `FROM report_configs rc LEFT JOIN lenses l ON l.id = rc.lens_id`
	var conditions []string
	var args []any
	if v, ok := p.Filters["lens_id"]; ok {
		conditions = append(conditions, "rc.lens_id = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["audience"]; ok {
		conditions = append(conditions, "rc.audience = ?")
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

	query := fmt.Sprintf(`SELECT rc.id, rc.name, rc.lens_id, COALESCE(l.name,''), rc.audience %s%s ORDER BY rc.name LIMIT ? OFFSET ?`,
		baseQuery, where)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]reportResponse, 0)
	for rows.Next() {
		var rr reportResponse
		if err := rows.Scan(&rr.ID, &rr.Name, &rr.LensID, &rr.LensName, &rr.Audience); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, rr)
	}
	// Close the cursor before the next statements: the store has one connection per statement
	// in flight, and the repo count below runs queries of its own.
	if err := rows.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range results {
		// Compute repo_count from filters
		repoIDs, err := s.store.ResolveReportRepos(results[i].ID)
		if err == nil {
			results[i].RepoCount = len(repoIDs)
		}
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var rr reportResponse
	err := s.store.DB().QueryRow(`SELECT rc.id, rc.name, rc.lens_id, COALESCE(l.name,''), rc.audience
		FROM report_configs rc LEFT JOIN lenses l ON l.id = rc.lens_id WHERE rc.id = ?`, id).
		Scan(&rr.ID, &rr.Name, &rr.LensID, &rr.LensName, &rr.Audience)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repoIDs, err := s.store.ResolveReportRepos(id)
	if err == nil {
		rr.RepoCount = len(repoIDs)
	}
	writeItem(w, rr)
}

type reportCreateRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LensID   string `json:"lens_id"`
	Audience string `json:"audience"`
}

func (s *Server) createReport(w http.ResponseWriter, r *http.Request) {
	var req reportCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().Exec("INSERT INTO report_configs (id, name, description, lens_id, audience, created_at) VALUES (?, ?, '', ?, ?, ?)",
		req.ID, req.Name, req.LensID, req.Audience, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var lensName string
	s.store.DB().QueryRow("SELECT name FROM lenses WHERE id = ?", req.LensID).Scan(&lensName)
	writeCreated(w, reportResponse{ID: req.ID, Name: req.Name, LensID: req.LensID, LensName: lensName, RepoCount: 0, Audience: req.Audience})
}

func (s *Server) updateReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Check existence
	var exists bool
	err := s.store.DB().QueryRow("SELECT 1 FROM report_configs WHERE id = ?", id).Scan(&exists)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}

	var req reportCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	_, err = s.store.DB().Exec("UPDATE report_configs SET name=?, lens_id=?, audience=? WHERE id=?",
		req.Name, req.LensID, req.Audience, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var rr reportResponse
	s.store.DB().QueryRow(`SELECT rc.id, rc.name, rc.lens_id, COALESCE(l.name,''), rc.audience
		FROM report_configs rc LEFT JOIN lenses l ON l.id = rc.lens_id WHERE rc.id = ?`, id).
		Scan(&rr.ID, &rr.Name, &rr.LensID, &rr.LensName, &rr.Audience)
	repoIDs, err := s.store.ResolveReportRepos(id)
	if err == nil {
		rr.RepoCount = len(repoIDs)
	}
	writeItem(w, rr)
}

func (s *Server) deleteReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteReportConfig(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
