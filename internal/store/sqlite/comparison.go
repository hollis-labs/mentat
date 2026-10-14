package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateComparisonSet(cs *domain.ComparisonSet) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cs.CreatedAt, _ = time.Parse(time.RFC3339, now)

	_, err := s.db.Exec(`INSERT INTO comparison_sets (id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		cs.ID, cs.Name, cs.Description, now)
	if err != nil {
		return fmt.Errorf("insert comparison set: %w", err)
	}
	return nil
}

func (s *Store) AddComparisonSetRepo(setID, repoID, role string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO comparison_set_repos (set_id, repo_id, role) VALUES (?, ?, ?)`,
		setID, repoID, role)
	if err != nil {
		return fmt.Errorf("add comparison set repo: %w", err)
	}
	return nil
}

func (s *Store) ListComparisonSets() ([]domain.ComparisonSet, error) {
	rows, err := s.db.Query(`SELECT id, name, description, created_at FROM comparison_sets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list comparison sets: %w", err)
	}
	defer rows.Close()

	var sets []domain.ComparisonSet
	for rows.Next() {
		var cs domain.ComparisonSet
		var createdAt string
		if err := rows.Scan(&cs.ID, &cs.Name, &cs.Description, &createdAt); err != nil {
			return nil, fmt.Errorf("scan comparison set: %w", err)
		}
		cs.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		sets = append(sets, cs)
	}
	return sets, rows.Err()
}
