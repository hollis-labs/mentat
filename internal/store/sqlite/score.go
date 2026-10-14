package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateScorecard(sc *domain.Scorecard) error {
	now := time.Now().UTC().Format(time.RFC3339)
	sc.ScoredAt, _ = time.Parse(time.RFC3339, now)

	result, err := s.db.Exec(`INSERT INTO scorecards (repo_id, scored_at, overall, notes) VALUES (?, ?, ?, ?)`,
		sc.RepoID, now, sc.Overall, sc.Notes)
	if err != nil {
		return fmt.Errorf("insert scorecard: %w", err)
	}
	sc.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) SetDimensionScore(ds *domain.DimensionScore) error {
	_, err := s.db.Exec(`INSERT INTO dimension_scores (scorecard_id, dimension_id, score, evidence, notes)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scorecard_id, dimension_id) DO UPDATE SET score=excluded.score, evidence=excluded.evidence, notes=excluded.notes`,
		ds.ScorecardID, ds.DimensionID, ds.Score, ds.Evidence, ds.Notes)
	if err != nil {
		return fmt.Errorf("set dimension score: %w", err)
	}
	return nil
}

func (s *Store) GetLatestScorecard(repoID string) (*domain.Scorecard, error) {
	sc := &domain.Scorecard{}
	var scoredAt string
	err := s.db.QueryRow(`SELECT id, repo_id, scored_at, overall, notes FROM scorecards WHERE repo_id = ? ORDER BY scored_at DESC LIMIT 1`, repoID).
		Scan(&sc.ID, &sc.RepoID, &scoredAt, &sc.Overall, &sc.Notes)
	if err != nil {
		return nil, fmt.Errorf("get latest scorecard: %w", err)
	}
	sc.ScoredAt, _ = time.Parse(time.RFC3339, scoredAt)

	rows, err := s.db.Query(`SELECT id, scorecard_id, dimension_id, score, evidence, notes FROM dimension_scores WHERE scorecard_id = ?`, sc.ID)
	if err != nil {
		return nil, fmt.Errorf("get dimension scores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ds domain.DimensionScore
		if err := rows.Scan(&ds.ID, &ds.ScorecardID, &ds.DimensionID, &ds.Score, &ds.Evidence, &ds.Notes); err != nil {
			return nil, fmt.Errorf("scan dimension score: %w", err)
		}
		sc.Scores = append(sc.Scores, ds)
	}
	return sc, rows.Err()
}

func (s *Store) RecalculateOverall(scorecardID int64) error {
	_, err := s.db.Exec(`UPDATE scorecards SET overall = (
		SELECT COALESCE(SUM(ds.score * rd.weight) / NULLIF(SUM(rd.weight), 0), 0)
		FROM dimension_scores ds
		JOIN review_dimensions rd ON rd.id = ds.dimension_id
		WHERE ds.scorecard_id = ?
	) WHERE id = ?`, scorecardID, scorecardID)
	if err != nil {
		return fmt.Errorf("recalculate overall: %w", err)
	}
	return nil
}
