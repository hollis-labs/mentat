package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateRepo(r *domain.Repo) error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.CreatedAt, _ = time.Parse(time.RFC3339, now)
	r.UpdatedAt = r.CreatedAt
	if r.EmbeddingProfile == "" {
		r.EmbeddingProfile = "none"
	}

	_, err := s.db.Exec(`INSERT INTO repos (id, name, url, description, stack, category, is_own, local_path, homepage, license, notes, embedding_profile, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.URL, r.Description, r.Stack, r.Category,
		boolToInt(r.IsOwn), r.LocalPath, r.Homepage, r.License, r.Notes, r.EmbeddingProfile,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("insert repo: %w", err)
	}
	return nil
}

func (s *Store) GetRepo(id string) (*domain.Repo, error) {
	r := &domain.Repo{}
	var isOwn int
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT id, name, url, description, stack, category, is_own, local_path, homepage, license, notes, embedding_profile, created_at, updated_at FROM repos WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.URL, &r.Description, &r.Stack, &r.Category,
			&isOwn, &r.LocalPath, &r.Homepage, &r.License, &r.Notes, &r.EmbeddingProfile,
			&createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	r.IsOwn = isOwn == 1
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return r, nil
}

func (s *Store) UpdateRepo(r *domain.Repo) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if r.EmbeddingProfile == "" {
		r.EmbeddingProfile = "none"
	}
	_, err := s.db.Exec(`UPDATE repos SET name=?, url=?, description=?, stack=?, category=?, is_own=?, local_path=?, homepage=?, license=?, notes=?, embedding_profile=?, updated_at=? WHERE id=?`,
		r.Name, r.URL, r.Description, r.Stack, r.Category,
		boolToInt(r.IsOwn), r.LocalPath, r.Homepage, r.License, r.Notes, r.EmbeddingProfile,
		now, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update repo: %w", err)
	}
	return nil
}

func (s *Store) DeleteRepo(id string) error {
	_, err := s.db.Exec("DELETE FROM repos WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	return nil
}

type RepoFilter struct {
	Category string
	IsOwn    *bool
	Tag      string
	Stack    string
}

func (s *Store) ListRepos(f RepoFilter) ([]domain.Repo, error) {
	query := `SELECT r.id, r.name, r.url, r.description, r.stack, r.category, r.is_own, r.local_path, r.homepage, r.license, r.notes, r.embedding_profile, r.created_at, r.updated_at FROM repos r`
	var conditions []string
	var args []any

	if f.Tag != "" {
		query += ` JOIN repo_tags rt ON rt.repo_id = r.id`
		conditions = append(conditions, "rt.tag_id = ?")
		args = append(args, f.Tag)
	}
	if f.Category != "" {
		conditions = append(conditions, "r.category = ?")
		args = append(args, f.Category)
	}
	if f.IsOwn != nil {
		conditions = append(conditions, "r.is_own = ?")
		args = append(args, boolToInt(*f.IsOwn))
	}
	if f.Stack != "" {
		conditions = append(conditions, "r.stack = ?")
		args = append(args, f.Stack)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY r.category, r.name"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()

	var repos []domain.Repo
	for rows.Next() {
		var r domain.Repo
		var isOwn int
		var createdAt, updatedAt string
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Description, &r.Stack, &r.Category,
			&isOwn, &r.LocalPath, &r.Homepage, &r.License, &r.Notes, &r.EmbeddingProfile,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		r.IsOwn = isOwn == 1
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *Store) SetRepoEmbeddingProfile(repoID, profile string) error {
	_, err := s.db.Exec(`UPDATE repos SET embedding_profile = ?, updated_at = ? WHERE id = ?`,
		profile, time.Now().UTC().Format(time.RFC3339), repoID)
	if err != nil {
		return fmt.Errorf("set repo embedding profile: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
