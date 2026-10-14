package sqlite

import (
	"fmt"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateSnapshot(snap *domain.Snapshot) error {
	now := time.Now().UTC().Format(time.RFC3339)
	snap.CapturedAt, _ = time.Parse(time.RFC3339, now)

	result, err := s.db.Exec(`INSERT INTO snapshots (repo_id, captured_at, stars, forks, open_issues, contributors, last_commit_at, commits_30d, loc, files, test_files, dependencies, complexity_avg, raw_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.RepoID, now, snap.Stars, snap.Forks, snap.OpenIssues, snap.Contributors,
		timePtr(snap.LastCommitAt), snap.Commits30d, snap.LoC, snap.Files, snap.TestFiles,
		snap.Dependencies, snap.ComplexityAvg, snap.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	snap.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) ListSnapshots(repoID string, limit int) ([]domain.Snapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, repo_id, captured_at, stars, forks, open_issues, contributors, last_commit_at, commits_30d, loc, files, test_files, dependencies, complexity_avg, raw_json
		FROM snapshots WHERE repo_id = ? ORDER BY captured_at DESC LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var snaps []domain.Snapshot
	for rows.Next() {
		var snap domain.Snapshot
		var capturedAt string
		var lastCommit *string
		if err := rows.Scan(&snap.ID, &snap.RepoID, &capturedAt, &snap.Stars, &snap.Forks,
			&snap.OpenIssues, &snap.Contributors, &lastCommit, &snap.Commits30d,
			&snap.LoC, &snap.Files, &snap.TestFiles, &snap.Dependencies,
			&snap.ComplexityAvg, &snap.RawJSON); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snap.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		if lastCommit != nil {
			t, _ := time.Parse(time.RFC3339, *lastCommit)
			snap.LastCommitAt = &t
		}
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}

func timePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
