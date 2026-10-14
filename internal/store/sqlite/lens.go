package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateLens(l *domain.Lens) error {
	now := time.Now().UTC().Format(time.RFC3339)
	l.CreatedAt, _ = time.Parse(time.RFC3339, now)

	_, err := s.db.Exec(`INSERT INTO lenses (id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		l.ID, l.Name, l.Description, now)
	if err != nil {
		return fmt.Errorf("insert lens: %w", err)
	}
	return nil
}

func (s *Store) AddLensDimension(lensID, dimensionID string, weight float64, sortOrder int) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO lens_dimensions (lens_id, dimension_id, weight, sort_order) VALUES (?, ?, ?, ?)`,
		lensID, dimensionID, weight, sortOrder)
	if err != nil {
		return fmt.Errorf("add lens dimension: %w", err)
	}
	return nil
}

func (s *Store) GetLens(id string) (*domain.Lens, error) {
	l := &domain.Lens{}
	var createdAt string
	err := s.db.QueryRow(`SELECT id, name, description, created_at FROM lenses WHERE id = ?`, id).
		Scan(&l.ID, &l.Name, &l.Description, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("get lens: %w", err)
	}
	l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	rows, err := s.db.Query(`SELECT ld.dimension_id, rd.name, ld.weight, ld.sort_order
		FROM lens_dimensions ld JOIN review_dimensions rd ON rd.id = ld.dimension_id
		WHERE ld.lens_id = ? ORDER BY ld.sort_order`, id)
	if err != nil {
		return nil, fmt.Errorf("get lens dimensions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ld domain.LensDimension
		if err := rows.Scan(&ld.DimensionID, &ld.DimensionName, &ld.Weight, &ld.SortOrder); err != nil {
			return nil, fmt.Errorf("scan lens dimension: %w", err)
		}
		l.Dimensions = append(l.Dimensions, ld)
	}
	return l, rows.Err()
}

func (s *Store) ListLenses() ([]domain.Lens, error) {
	rows, err := s.db.Query(`SELECT l.id, l.name, l.description, l.created_at,
		(SELECT COUNT(*) FROM lens_dimensions ld WHERE ld.lens_id = l.id) as dim_count
		FROM lenses l ORDER BY l.name`)
	if err != nil {
		return nil, fmt.Errorf("list lenses: %w", err)
	}
	defer rows.Close()

	var lenses []domain.Lens
	for rows.Next() {
		var l domain.Lens
		var createdAt string
		var dimCount int
		if err := rows.Scan(&l.ID, &l.Name, &l.Description, &createdAt, &dimCount); err != nil {
			return nil, fmt.Errorf("scan lens: %w", err)
		}
		l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		l.DimCount = dimCount
		lenses = append(lenses, l)
	}
	return lenses, rows.Err()
}

// RecalculateOverallForLens computes the weighted overall using only the lens's dimensions.
func (s *Store) RecalculateOverallForLens(scorecardID int64, lensID string) error {
	query := `UPDATE scorecards SET overall = (
		SELECT COALESCE(SUM(ds.score * ld.weight) / NULLIF(SUM(ld.weight), 0), 0)
		FROM dimension_scores ds
		JOIN lens_dimensions ld ON ld.dimension_id = ds.dimension_id AND ld.lens_id = ?
		WHERE ds.scorecard_id = ?
	), lens_id = ? WHERE id = ?`
	_, err := s.db.Exec(query, lensID, scorecardID, lensID, scorecardID)
	if err != nil {
		return fmt.Errorf("recalculate overall for lens: %w", err)
	}
	return nil
}
