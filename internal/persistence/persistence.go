// Package persistence owns Mentat's durable schema: one goose migration, embedded here and applied
// by Substrate's database module. Mentat is SQLite only (FTS5 indexes, `?` placeholders).
package persistence

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"

	_ "modernc.org/sqlite" // registers the "sqlite" driver the guard uses
)

//go:embed migrations
var migrations embed.FS

// Migrations is the embedded migration tree: Root "migrations" holds one directory per dialect
// (here only sqlite).
func Migrations() (fsys embed.FS, root string) { return migrations, "migrations" }

// ErrLegacyLayout means the database file holds the layout used before Mentat moved onto
// Substrate. It is never opened in place: `mentat migrate-legacy` copies it into a new file.
var ErrLegacyLayout = errors.New("the database uses the pre-Substrate Mentat layout")

// RefuseLegacy returns ErrLegacyLayout (wrapped with what to do) when the SQLite file at path
// already exists with the old layout: the hand-rolled schema_migrations table and no
// goose_db_version. A missing or empty file is fine. Run it before the database module opens the
// file, because opening migrates.
func RefuseLegacy(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	// Only reads: the guard never changes the file it is judging.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var legacy, goose int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&legacy); err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'").Scan(&goose); err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if legacy > 0 && goose == 0 {
		return fmt.Errorf("%w (%s): this file is not opened in place; point database.sqlite.path at a new file and run `mentat migrate-legacy --from %s`. See docs/cutover-plan.md",
			ErrLegacyLayout, path, path)
	}
	return nil
}
