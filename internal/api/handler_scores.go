package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type scoreResponse struct {
	ID            string  `json:"id"`
	ScorecardID   string  `json:"scorecard_id"`
	DimensionID   string  `json:"dimension_id"`
	DimensionName string  `json:"dimension_name"`
	Score         float64 `json:"score"`
	Evidence      string  `json:"evidence"`
}

func (s *Server) listScores(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "dimension_name", 25)

	baseQuery := `FROM dimension_scores ds
		JOIN review_dimensions rd ON rd.id = ds.dimension_id`

	var conditions []string
	var args []any
	if v, ok := p.Filters["scorecard_id"]; ok {
		conditions = append(conditions, "ds.scorecard_id = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["dimension_id"]; ok {
		conditions = append(conditions, "ds.dimension_id = ?")
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

	query := fmt.Sprintf(`SELECT ds.id, ds.scorecard_id, ds.dimension_id, rd.name, ds.score, ds.evidence %s%s ORDER BY rd.sort_order LIMIT ? OFFSET ?`,
		baseQuery, where)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]scoreResponse, 0)
	for rows.Next() {
		var sr scoreResponse
		var id, scorecardID int64
		if err := rows.Scan(&id, &scorecardID, &sr.DimensionID, &sr.DimensionName, &sr.Score, &sr.Evidence); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sr.ID = fmt.Sprintf("%d", id)
		sr.ScorecardID = fmt.Sprintf("%d", scorecardID)
		results = append(results, sr)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

type scoreCreateRequest struct {
	ScorecardID int64   `json:"scorecard_id"`
	DimensionID string  `json:"dimension_id"`
	Score       float64 `json:"score"`
	Evidence    string  `json:"evidence"`
}

func (s *Server) createScore(w http.ResponseWriter, r *http.Request) {
	var req scoreCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	result, err := s.store.DB().Exec(`INSERT INTO dimension_scores (scorecard_id, dimension_id, score, evidence, notes)
		VALUES (?, ?, ?, ?, '')
		ON CONFLICT(scorecard_id, dimension_id) DO UPDATE SET score=excluded.score, evidence=excluded.evidence`,
		req.ScorecardID, req.DimensionID, req.Score, req.Evidence)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, _ := result.LastInsertId()

	// Get dimension name
	var dimName string
	s.store.DB().QueryRow("SELECT name FROM review_dimensions WHERE id = ?", req.DimensionID).Scan(&dimName)

	writeCreated(w, scoreResponse{
		ID: fmt.Sprintf("%d", id), ScorecardID: fmt.Sprintf("%d", req.ScorecardID),
		DimensionID: req.DimensionID, DimensionName: dimName, Score: req.Score, Evidence: req.Evidence,
	})
}

func (s *Server) updateScore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req scoreCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	_, err := s.store.DB().Exec("UPDATE dimension_scores SET score=?, evidence=? WHERE id=?", req.Score, req.Evidence, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var sr scoreResponse
	var dbID, scorecardID int64
	err = s.store.DB().QueryRow(`SELECT ds.id, ds.scorecard_id, ds.dimension_id, rd.name, ds.score, ds.evidence
		FROM dimension_scores ds JOIN review_dimensions rd ON rd.id = ds.dimension_id WHERE ds.id = ?`, id).
		Scan(&dbID, &scorecardID, &sr.DimensionID, &sr.DimensionName, &sr.Score, &sr.Evidence)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "score not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sr.ID = fmt.Sprintf("%d", dbID)
	sr.ScorecardID = fmt.Sprintf("%d", scorecardID)
	writeItem(w, sr)
}
