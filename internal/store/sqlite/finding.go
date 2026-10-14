package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateFinding(f *domain.Finding) error {
	now := time.Now().UTC().Format(time.RFC3339)
	f.CreatedAt, _ = time.Parse(time.RFC3339, now)
	f.UpdatedAt = f.CreatedAt

	result, err := s.db.Exec(`INSERT INTO findings (repo_id, title, category, severity, description, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.RepoID, f.Title, f.Category, f.Severity, f.Description, f.Status, now, now)
	if err != nil {
		return fmt.Errorf("insert finding: %w", err)
	}
	f.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) ListFindings(repoID, category, status string) ([]domain.Finding, error) {
	query := `SELECT id, repo_id, title, category, severity, description, status, created_at, updated_at FROM findings WHERE 1=1`
	var args []any

	if repoID != "" {
		query += " AND repo_id = ?"
		args = append(args, repoID)
	}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()

	var findings []domain.Finding
	for rows.Next() {
		var f domain.Finding
		var createdAt, updatedAt string
		if err := rows.Scan(&f.ID, &f.RepoID, &f.Title, &f.Category, &f.Severity, &f.Description, &f.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		findings = append(findings, f)
	}
	return findings, rows.Err()
}
