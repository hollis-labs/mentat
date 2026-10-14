package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/domain"
)

type dimensionResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

func (s *Server) listDimensions(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "name", 25)
	category := p.Filters["category"]

	baseQuery := "FROM review_dimensions"
	var conditions []string
	var args []any
	if category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, category)
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

	query := fmt.Sprintf("SELECT id, name, category, weight, description %s%s ORDER BY sort_order LIMIT ? OFFSET ?", baseQuery, where)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]dimensionResponse, 0)
	for rows.Next() {
		var d dimensionResponse
		if err := rows.Scan(&d.ID, &d.Name, &d.Category, &d.Weight, &d.Description); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, d)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) getDimension(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var d dimensionResponse
	err := s.store.DB().QueryRow("SELECT id, name, category, weight, description FROM review_dimensions WHERE id = ?", id).
		Scan(&d.ID, &d.Name, &d.Category, &d.Weight, &d.Description)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "dimension not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeItem(w, d)
}

func (s *Server) createDimension(w http.ResponseWriter, r *http.Request) {
	var d domain.ReviewDimension
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if d.ID == "" || d.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	if err := s.store.CreateDimension(&d); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, dimensionResponse{ID: d.ID, Name: d.Name, Category: d.Category, Weight: d.Weight, Description: d.Description})
}

func (s *Server) updateDimension(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check existence
	var exists bool
	err := s.store.DB().QueryRow("SELECT 1 FROM review_dimensions WHERE id = ?", id).Scan(&exists)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "dimension not found")
		return
	}

	var d domain.ReviewDimension
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	_, err = s.store.DB().Exec("UPDATE review_dimensions SET name=?, category=?, weight=?, description=? WHERE id=?",
		d.Name, d.Category, d.Weight, d.Description, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeItem(w, dimensionResponse{ID: id, Name: d.Name, Category: d.Category, Weight: d.Weight, Description: d.Description})
}

func (s *Server) deleteDimension(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := s.store.DB().Exec("DELETE FROM review_dimensions WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
