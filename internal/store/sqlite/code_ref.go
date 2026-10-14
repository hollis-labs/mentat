package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateCodeReference(cr *domain.CodeReference) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cr.CreatedAt, _ = time.Parse(time.RFC3339, now)

	result, err := s.db.Exec(`INSERT INTO code_references (repo_id, file_path, line_start, line_end, description, ref_type, pattern_id, finding_id, symbol_id, anchor_content_hash, stale_since_commit, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cr.RepoID, cr.FilePath, cr.LineStart, cr.LineEnd, cr.Description, cr.RefType, cr.PatternID, cr.FindingID, cr.SymbolID, cr.AnchorContentHash, cr.StaleSinceCommit, now)
	if err != nil {
		return fmt.Errorf("insert code reference: %w", err)
	}
	cr.ID, _ = result.LastInsertId()
	return nil
}
