package sqlite

import (
	"fmt"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) ListDimensions() ([]domain.ReviewDimension, error) {
	rows, err := s.db.Query(`SELECT id, name, category, description, weight, sort_order FROM review_dimensions ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("list dimensions: %w", err)
	}
	defer rows.Close()

	var dims []domain.ReviewDimension
	for rows.Next() {
		var d domain.ReviewDimension
		if err := rows.Scan(&d.ID, &d.Name, &d.Category, &d.Description, &d.Weight, &d.SortOrder); err != nil {
			return nil, fmt.Errorf("scan dimension: %w", err)
		}
		dims = append(dims, d)
	}
	return dims, rows.Err()
}

func (s *Store) CreateDimension(d *domain.ReviewDimension) error {
	_, err := s.db.Exec(`INSERT INTO review_dimensions (id, name, category, description, weight, sort_order) VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.Category, d.Description, d.Weight, d.SortOrder)
	if err != nil {
		return fmt.Errorf("insert dimension: %w", err)
	}
	return nil
}
