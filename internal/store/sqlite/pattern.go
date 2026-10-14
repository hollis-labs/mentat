package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreatePattern(p *domain.ArchitecturePattern) error {
	now := time.Now().UTC().Format(time.RFC3339)
	p.CreatedAt, _ = time.Parse(time.RFC3339, now)
	p.UpdatedAt = p.CreatedAt

	_, err := s.db.Exec(`INSERT INTO architecture_patterns (id, name, type, category, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Type, p.Category, p.Description, now, now)
	if err != nil {
		return fmt.Errorf("insert pattern: %w", err)
	}
	return nil
}

func (s *Store) LinkRepoPattern(rp *domain.RepoPattern) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO repo_patterns (repo_id, pattern_id, quality, notes) VALUES (?, ?, ?, ?)`,
		rp.RepoID, rp.PatternID, rp.Quality, rp.Notes)
	if err != nil {
		return fmt.Errorf("link repo pattern: %w", err)
	}
	return nil
}

func (s *Store) ListPatterns(patternType string) ([]domain.ArchitecturePattern, error) {
	query := `SELECT id, name, type, category, description, created_at, updated_at FROM architecture_patterns`
	var args []any
	if patternType != "" {
		query += " WHERE type = ?"
		args = append(args, patternType)
	}
	query += " ORDER BY category, name"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list patterns: %w", err)
	}
	defer rows.Close()

	var patterns []domain.ArchitecturePattern
	for rows.Next() {
		var p domain.ArchitecturePattern
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Category, &p.Description, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan pattern: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}
