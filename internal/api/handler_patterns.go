package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type patternResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Description string `json:"description"`
	RepoCount   int    `json:"repo_count"`
}

func (s *Server) listPatterns(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "name", 25)

	baseQuery := `FROM architecture_patterns ap
		LEFT JOIN (SELECT pattern_id, COUNT(*) as cnt FROM repo_patterns GROUP BY pattern_id) rpc ON rpc.pattern_id = ap.id`

	var conditions []string
	var args []any
	if v, ok := p.Filters["type"]; ok {
		conditions = append(conditions, "ap.type = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["category"]; ok {
		conditions = append(conditions, "ap.category = ?")
		args = append(args, v)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.store.DB().QueryRow("SELECT COUNT(*) "+baseQuery+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sortMap := map[string]string{"name": "ap.name", "category": "ap.category", "repo_count": "COALESCE(rpc.cnt,0)"}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "ap.name"
	}

	query := fmt.Sprintf(`SELECT ap.id, ap.name, ap.type, ap.category, ap.description, COALESCE(rpc.cnt,0) %s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]patternResponse, 0)
	for rows.Next() {
		var pr patternResponse
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Type, &pr.Category, &pr.Description, &pr.RepoCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, pr)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getPattern(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var pr patternResponse
	err := s.store.DB().QueryRow(`SELECT ap.id, ap.name, ap.type, ap.category, ap.description,
		(SELECT COUNT(*) FROM repo_patterns WHERE pattern_id = ap.id)
		FROM architecture_patterns ap WHERE ap.id = ?`, id).
		Scan(&pr.ID, &pr.Name, &pr.Type, &pr.Category, &pr.Description, &pr.RepoCount)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "pattern not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeItem(w, pr)
}

type patternCreateRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

func (s *Server) createPattern(w http.ResponseWriter, r *http.Request) {
	var req patternCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().Exec(`INSERT INTO architecture_patterns (id, name, type, category, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.Name, req.Type, req.Category, req.Description, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, patternResponse{ID: req.ID, Name: req.Name, Type: req.Type, Category: req.Category, Description: req.Description, RepoCount: 0})
}

func (s *Server) updatePattern(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req patternCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().Exec("UPDATE architecture_patterns SET name=?, type=?, category=?, description=?, updated_at=? WHERE id=?",
		req.Name, req.Type, req.Category, req.Description, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return updated
	var pr patternResponse
	s.store.DB().QueryRow(`SELECT ap.id, ap.name, ap.type, ap.category, ap.description,
		(SELECT COUNT(*) FROM repo_patterns WHERE pattern_id = ap.id) FROM architecture_patterns ap WHERE ap.id = ?`, id).
		Scan(&pr.ID, &pr.Name, &pr.Type, &pr.Category, &pr.Description, &pr.RepoCount)
	writeItem(w, pr)
}

func (s *Server) deletePattern(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.DB().Exec("DELETE FROM architecture_patterns WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
