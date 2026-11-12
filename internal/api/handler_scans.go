package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Valid Hadron blueprints for scanning.
var validBlueprints = map[string]string{
	"se-repo-scan":     "Stack Explorer: Repo Scan",
	"se-security-scan": "Stack Explorer: Security Scan",
	"se-feature-audit": "Stack Explorer: Feature Audit",
}

type scanResponse struct {
	ID           string  `json:"id"`
	RepoID       string  `json:"repo_id"`
	RepoName     string  `json:"repo_name"`
	Blueprint    string  `json:"blueprint"`
	Status       string  `json:"status"`
	ResultJSON   string  `json:"result_json,omitempty"`
	ErrorMessage string  `json:"error_message,omitempty"`
	StartedAt    *string `json:"started_at,omitempty"`
	FinishedAt   *string `json:"finished_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func (s *Server) listScans(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "created_at", 25)

	baseQuery := `FROM scans sc LEFT JOIN repos r ON r.id = sc.repo_id`

	var conditions []string
	var args []any
	if v, ok := p.Filters["repo_id"]; ok {
		conditions = append(conditions, "sc.repo_id = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["blueprint"]; ok {
		conditions = append(conditions, "sc.blueprint = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["status"]; ok {
		conditions = append(conditions, "sc.status = ?")
		args = append(args, v)
	}
	if p.Search != "" {
		search := "%" + p.Search + "%"
		conditions = append(conditions, "(r.name LIKE ? OR sc.blueprint LIKE ?)")
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

	sortMap := map[string]string{
		"created_at": "sc.created_at",
		"status":     "sc.status",
		"blueprint":  "sc.blueprint",
		"repo_id":    "sc.repo_id",
	}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "sc.created_at"
		p.Direction = "DESC"
	}

	query := fmt.Sprintf(`SELECT sc.id, sc.repo_id, COALESCE(r.name,''), sc.blueprint, sc.status, sc.result_json, sc.error_message, sc.started_at, sc.finished_at, sc.created_at, sc.updated_at %s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	results := make([]scanResponse, 0)
	for rows.Next() {
		var sr scanResponse
		var id int64
		var startedAt, finishedAt sql.NullString
		if err := rows.Scan(&id, &sr.RepoID, &sr.RepoName, &sr.Blueprint, &sr.Status, &sr.ResultJSON, &sr.ErrorMessage, &startedAt, &finishedAt, &sr.CreatedAt, &sr.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sr.ID = fmt.Sprintf("%d", id)
		if startedAt.Valid {
			sr.StartedAt = &startedAt.String
		}
		if finishedAt.Valid {
			sr.FinishedAt = &finishedAt.String
		}
		results = append(results, sr)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var sr scanResponse
	var dbID int64
	var startedAt, finishedAt sql.NullString
	err := s.store.DB().QueryRow(`SELECT sc.id, sc.repo_id, COALESCE(r.name,''), sc.blueprint, sc.status, sc.result_json, sc.error_message, sc.started_at, sc.finished_at, sc.created_at, sc.updated_at
		FROM scans sc LEFT JOIN repos r ON r.id = sc.repo_id WHERE sc.id = ?`, id).
		Scan(&dbID, &sr.RepoID, &sr.RepoName, &sr.Blueprint, &sr.Status, &sr.ResultJSON, &sr.ErrorMessage, &startedAt, &finishedAt, &sr.CreatedAt, &sr.UpdatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sr.ID = fmt.Sprintf("%d", dbID)
	if startedAt.Valid {
		sr.StartedAt = &startedAt.String
	}
	if finishedAt.Valid {
		sr.FinishedAt = &finishedAt.String
	}
	writeItem(w, sr)
}

type scanCreateRequest struct {
	RepoID    string `json:"repo_id"`
	Blueprint string `json:"blueprint"`
}

func (s *Server) createScan(w http.ResponseWriter, r *http.Request) {
	var req scanCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.RepoID == "" {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if req.Blueprint == "" {
		writeError(w, http.StatusBadRequest, "blueprint is required")
		return
	}
	if _, ok := validBlueprints[req.Blueprint]; !ok {
		keys := make([]string, 0, len(validBlueprints))
		for k := range validBlueprints {
			keys = append(keys, k)
		}
		writeError(w, http.StatusBadRequest, "invalid blueprint; valid options: "+strings.Join(keys, ", "))
		return
	}

	// Verify repo exists
	var repoName string
	err := s.store.DB().QueryRow("SELECT name FROM repos WHERE id = ?", req.RepoID).Scan(&repoName)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusBadRequest, "repo not found: "+req.RepoID)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.store.DB().Exec(`INSERT INTO scans (repo_id, blueprint, status, result_json, error_message, created_at, updated_at)
		VALUES (?, ?, 'pending', '{}', '', ?, ?)`, req.RepoID, req.Blueprint, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := result.LastInsertId()

	writeCreated(w, scanResponse{
		ID:        fmt.Sprintf("%d", id),
		RepoID:    req.RepoID,
		RepoName:  repoName,
		Blueprint: req.Blueprint,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})
}

type scanUpdateRequest struct {
	Status       string `json:"status"`
	ResultJSON   string `json:"result_json"`
	ErrorMessage string `json:"error_message"`
}

func (s *Server) updateScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Check scan exists
	var currentStatus string
	err := s.store.DB().QueryRow("SELECT status FROM scans WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "scan not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var req scanUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Build dynamic SET clause
	sets := []string{"updated_at = ?"}
	args := []any{now}

	if req.Status != "" {
		validStatuses := map[string]bool{"pending": true, "running": true, "completed": true, "failed": true}
		if !validStatuses[req.Status] {
			writeError(w, http.StatusBadRequest, "invalid status; valid options: pending, running, completed, failed")
			return
		}
		sets = append(sets, "status = ?")
		args = append(args, req.Status)

		if req.Status == "running" && currentStatus == "pending" {
			sets = append(sets, "started_at = ?")
			args = append(args, now)
		}
		if req.Status == "completed" || req.Status == "failed" {
			sets = append(sets, "finished_at = ?")
			args = append(args, now)
		}
	}
	if req.ResultJSON != "" {
		sets = append(sets, "result_json = ?")
		args = append(args, req.ResultJSON)
	}
	if req.ErrorMessage != "" {
		sets = append(sets, "error_message = ?")
		args = append(args, req.ErrorMessage)
	}

	args = append(args, id)
	_, err = s.store.DB().Exec(fmt.Sprintf("UPDATE scans SET %s WHERE id = ?", strings.Join(sets, ", ")), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Re-read and return
	var sr scanResponse
	var dbID int64
	var startedAt, finishedAt sql.NullString
	_ = s.store.DB().QueryRow(`SELECT sc.id, sc.repo_id, COALESCE(r.name,''), sc.blueprint, sc.status, sc.result_json, sc.error_message, sc.started_at, sc.finished_at, sc.created_at, sc.updated_at
		FROM scans sc LEFT JOIN repos r ON r.id = sc.repo_id WHERE sc.id = ?`, id).Scan(&dbID, &sr.RepoID, &sr.RepoName, &sr.Blueprint, &sr.Status, &sr.ResultJSON, &sr.ErrorMessage, &startedAt, &finishedAt, &sr.CreatedAt, &sr.UpdatedAt)
	sr.ID = fmt.Sprintf("%d", dbID)
	if startedAt.Valid {
		sr.StartedAt = &startedAt.String
	}
	if finishedAt.Valid {
		sr.FinishedAt = &finishedAt.String
	}
	writeItem(w, sr)
}

func (s *Server) deleteScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.DB().Exec("DELETE FROM scans WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
