package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) UpsertRelationship(item *domain.Relationship) error {
	discoveredAt := item.DiscoveredAt.UTC()
	if discoveredAt.IsZero() {
		discoveredAt = time.Now().UTC()
	}
	if item.Weight == 0 {
		item.Weight = 1.0
	}
	result, err := s.db.Exec(`INSERT INTO relationships (
repo_id, src_symbol_id, dst_symbol_id, kind, weight, source, discovered_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(src_symbol_id, dst_symbol_id, kind, source) DO UPDATE SET
weight = excluded.weight,
discovered_at = excluded.discovered_at`,
		item.RepoID, item.SrcSymbolID, item.DstSymbolID, item.Kind, item.Weight, item.Source, discoveredAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert relationship: %w", err)
	}
	if item.ID == 0 {
		if id, idErr := result.LastInsertId(); idErr == nil {
			item.ID = id
		}
	}
	item.DiscoveredAt = discoveredAt
	return nil
}

func (s *Store) DeleteRelationshipsByRepoSource(repoID, source string) error {
	if _, err := s.db.Exec(`DELETE FROM relationships WHERE repo_id = ? AND source = ?`, repoID, source); err != nil {
		return fmt.Errorf("delete relationships by repo/source: %w", err)
	}
	return nil
}
