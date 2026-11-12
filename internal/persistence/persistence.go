// Package persistence owns Mentat's durable schema: one goose migration, embedded here and applied
// by Substrate's database module. Mentat is SQLite only (FTS5 indexes, `?` placeholders).
package persistence

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// LegacyFile is the database file name before Mentat moved onto Substrate.
const LegacyFile = "stack-explorer.db"

// Open says what a command may do about the database file.
type Open int

const (
	// ReadExisting: the file must already exist in the new layout; nothing is ever created.
	ReadExisting Open = iota
	// Create: the file may be created, unless the directory holds only the old database (migrate first).
	Create
	// CreateForMigration: the file may be created even beside the old database (`migrate-legacy` fills it).
	CreateForMigration
)

// Check is run before the database module opens path. It never changes anything: it returns an error telling the
// operator what to do instead of letting the module create or migrate a file.
func Check(path string, how Open) error {
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		return RefuseLegacy(path)
	}
	old := filepath.Join(filepath.Dir(path), LegacyFile)
	_, oldErr := os.Stat(old)
	hasOld := oldErr == nil
	switch {
	case hasOld && how != CreateForMigration:
		return fmt.Errorf("%w (%s): there is no %s beside it. Nothing was created. Copy the old file and run `mentat migrate-legacy --from <copy>` first; see docs/cutover-plan.md",
			ErrLegacyLayout, old, filepath.Base(path))
	case how == ReadExisting:
		return fmt.Errorf("there is no database at %s. Nothing was created. `mentat serve` (or `mentat migrate-legacy`) creates it", path)
	}
	return nil
}
