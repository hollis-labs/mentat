package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type findingResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	RepoID      string `json:"repo_id"`
	RepoName    string `json:"repo_name"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

func (s *Server) listFindings(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "title", 25)

	baseQuery := `FROM findings f LEFT JOIN repos r ON r.id = f.repo_id`

	var conditions []string
	var args []any
	if v, ok := p.Filters["repo_id"]; ok {
		conditions = append(conditions, "f.repo_id = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["category"]; ok {
		conditions = append(conditions, "f.category = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["severity"]; ok {
		conditions = append(conditions, "f.severity = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["status"]; ok {
		conditions = append(conditions, "f.status = ?")
		args = append(args, v)
	}
	if p.Search != "" {
		search := "%" + p.Search + "%"
		conditions = append(conditions, "(f.title LIKE ? OR r.name LIKE ?)")
		args = append(args, search, search)
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

	sortMap := map[string]string{"title": "f.title", "severity": "f.severity", "status": "f.status", "category": "f.category"}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "f.created_at"
		p.Direction = "DESC"
	}

	query := fmt.Sprintf(`SELECT f.id, f.title, COALESCE(f.repo_id,''), COALESCE(r.name,''), f.category, f.severity, f.status, f.description %s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	results := make([]findingResponse, 0)
	for rows.Next() {
		var fr findingResponse
		var id int64
		if err := rows.Scan(&id, &fr.Title, &fr.RepoID, &fr.RepoName, &fr.Category, &fr.Severity, &fr.Status, &fr.Description); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		fr.ID = fmt.Sprintf("%d", id)
		results = append(results, fr)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getFinding(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var fr findingResponse
	var dbID int64
	err := s.store.DB().QueryRow(`SELECT f.id, f.title, COALESCE(f.repo_id,''), COALESCE(r.name,''), f.category, f.severity, f.status, f.description
		FROM findings f LEFT JOIN repos r ON r.id = f.repo_id WHERE f.id = ?`, id).
		Scan(&dbID, &fr.Title, &fr.RepoID, &fr.RepoName, &fr.Category, &fr.Severity, &fr.Status, &fr.Description)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "finding not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fr.ID = fmt.Sprintf("%d", dbID)
	writeItem(w, fr)
}

type findingCreateRequest struct {
	RepoID      string `json:"repo_id"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

func (s *Server) createFinding(w http.ResponseWriter, r *http.Request) {
	var req findingCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Status == "" {
		req.Status = "open"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var repoID any
	if req.RepoID != "" {
		repoID = req.RepoID
	}
	result, err := s.store.DB().Exec(`INSERT INTO findings (repo_id, title, category, severity, description, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, repoID, req.Title, req.Category, req.Severity, req.Description, req.Status, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := result.LastInsertId()

	var repoName string
	if req.RepoID != "" {
		_ = s.store.DB().QueryRow("SELECT name FROM repos WHERE id = ?", req.RepoID).Scan(&repoName)
	}
	writeCreated(w, findingResponse{
		ID: fmt.Sprintf("%d", id), Title: req.Title, RepoID: req.RepoID, RepoName: repoName,
		Category: req.Category, Severity: req.Severity, Status: req.Status, Description: req.Description,
	})
}

func (s *Server) updateFinding(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req findingCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var repoID any
	if req.RepoID != "" {
		repoID = req.RepoID
	}
	_, err := s.store.DB().Exec("UPDATE findings SET repo_id=?, title=?, category=?, severity=?, description=?, status=?, updated_at=? WHERE id=?",
		repoID, req.Title, req.Category, req.Severity, req.Description, req.Status, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var fr findingResponse
	var dbID int64
	_ = s.store.DB().QueryRow(`SELECT f.id, f.title, COALESCE(f.repo_id,''), COALESCE(r.name,''), f.category, f.severity, f.status, f.description
		FROM findings f LEFT JOIN repos r ON r.id = f.repo_id WHERE f.id = ?`, id).Scan(&dbID, &fr.Title, &fr.RepoID, &fr.RepoName, &fr.Category, &fr.Severity, &fr.Status, &fr.Description)
	fr.ID = fmt.Sprintf("%d", dbID)
	writeItem(w, fr)
}

func (s *Server) deleteFinding(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.DB().Exec("DELETE FROM findings WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
