package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// A long read must not hold up a write: reads come from their own pool, writes from the single
// write connection. A cursor stays open (a read in progress) while a write commits.
func TestLongReadDoesNotBlockAWrite(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "rw.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := store.DB()
	if _, err := d.Exec(`INSERT INTO tags (id, name) VALUES ('a','a'), ('b','b')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err := d.Query(`SELECT id FROM tags ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no row")
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.Exec(`INSERT INTO tags (id, name) VALUES ('c','c')`)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write while a read is open: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a write waited behind an open read")
	}
	// The open cursor still reads its snapshot to the end.
	n := 1
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("cursor returned %d rows", n)
	}
}
