package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hollis-labs/libs/core/store"
	"github.com/hollis-labs/libs/core/store/migrate"

	"github.com/hollis-labs/mentat/internal/persistence"
	"github.com/hollis-labs/mentat/internal/store/db"
)

// Store is Mentat's data layer over a SQLite database.
type Store struct {
	db   *db.DB
	st   *store.Store
	path string
}

// New wraps the store the Substrate database module opened and migrated. path is the file it
// opened (the database download route needs it).
func New(st *store.Store, path string) *Store {
	return &Store{db: db.New(st), st: st, path: path}
}

// Open opens and migrates the SQLite file at path the way the database module does (libs
// core/store, then the embedded goose migration), for tests and tools that need their own
// handle. It opens a read pool of ONE connection unless readConns says otherwise, so a statement
// issued while a cursor is open hangs the test instead of passing by luck of pool size. The server
// uses New over the module's store (library default: 4 read connections).
func Open(path string, readConns ...int) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	ctx := context.Background()
	cfg := store.Config{Driver: store.DriverSQLite, SQLite: store.SQLiteConfig{Path: path, CreateParentDir: true}}
	cfg.SQLite.ReadConns = 1
	if len(readConns) > 0 {
		cfg.SQLite.ReadConns = readConns[0]
	}
	st, err := store.Open(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	fsys, root := persistence.Migrations()
	if err := migrate.Ensure(ctx, cfg, st, migrate.Source{FS: fsys, Root: root}); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return New(st, path), nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.st.Close()
}

// DB returns the statement handle for direct queries when needed (see package db for the rules).
func (s *Store) DB() *db.DB {
	return s.db
}

// SQL returns the framework store's write handle, for the modules that open their own tables over it
// (the scheduler's reference store) and for tests that do the same.
func (s *Store) SQL() *sql.DB {
	return s.st.DB()
}

// Path returns the filesystem path to the SQLite database file.
func (s *Store) Path() string {
	return s.path
}
