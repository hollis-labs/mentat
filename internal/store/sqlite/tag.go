package sqlite

import (
	"fmt"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) EnsureTag(id, name string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO tags (id, name) VALUES (?, ?)", id, name)
	if err != nil {
		return fmt.Errorf("ensure tag: %w", err)
	}
	return nil
}

func (s *Store) AddRepoTag(repoID, tagID string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO repo_tags (repo_id, tag_id) VALUES (?, ?)", repoID, tagID)
	if err != nil {
		return fmt.Errorf("add repo tag: %w", err)
	}
	return nil
}

func (s *Store) RemoveRepoTag(repoID, tagID string) error {
	_, err := s.db.Exec("DELETE FROM repo_tags WHERE repo_id = ? AND tag_id = ?", repoID, tagID)
	if err != nil {
		return fmt.Errorf("remove repo tag: %w", err)
	}
	return nil
}

func (s *Store) ListRepoTags(repoID string) ([]domain.Tag, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name FROM tags t JOIN repo_tags rt ON rt.tag_id = t.id WHERE rt.repo_id = ? ORDER BY t.name`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list repo tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (s *Store) ListAllTags() ([]domain.Tag, error) {
	rows, err := s.db.Query(`SELECT id, name FROM tags ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	var tags []domain.Tag
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}
