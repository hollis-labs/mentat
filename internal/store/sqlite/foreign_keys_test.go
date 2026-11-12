package sqlite

import (
	"path/filepath"
	"testing"
)

// The migration copy runs with foreign keys off so it can carry a source's orphan rows; the app itself must not.
// A write that would create an orphan is refused, on the write connection and inside a write transaction.
func TestForeignKeysAreEnforcedOnWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "fk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	const orphan = `INSERT INTO job_events (job_id, job_kind, event_type, created_at) VALUES ('no-such-job','scan','queued','2026-01-01T00:00:00Z')`
	if _, err := s.DB().Exec(orphan); err == nil {
		t.Fatal("an orphan job_events row was accepted")
	}
	// And again on a connection that has been used before, after other statements ran.
	for i := 0; i < 5; i++ {
		if _, err := s.DB().Exec(orphan); err == nil {
			t.Fatal("an orphan job_events row was accepted on a reused connection")
		}
	}
}
