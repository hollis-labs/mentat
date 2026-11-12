// Package db is Mentat's handle on the Substrate database module's store (libs core/store). The
// store has one write connection and a separate read-only pool, so a statement is a read or a
// write by the method that runs it:
//
//   - Query, QueryRow and QueryContext read, from the read pool. They see committed data only.
//   - Exec writes, on the single write connection, and commits before it returns.
//   - WriteTx runs several statements in one write transaction. Inside it, reads see the
//     transaction's own uncommitted writes; use it when a read must follow a write atomically.
//
// Never issue a second statement while a *sql.Rows is open: close the rows (read them all first)
// and then query. With one write connection and a small read pool, a nested statement can
// wait for a connection the open cursor is holding.
package db

import (
	"context"
	"database/sql"

	"github.com/hollis-labs/libs/core/store"
)

// DB runs statements on a Substrate store.
type DB struct {
	st *store.Store
}

// New wraps the database module's store.
func New(st *store.Store) *DB { return &DB{st: st} }

// Store is the underlying framework store.
func (d *DB) Store() *store.Store { return d.st }

// Query reads from the read pool. The caller closes the rows.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.st.Reader().QueryContext(context.Background(), query, args...)
}

// QueryContext is Query with a context.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.st.Reader().QueryContext(ctx, query, args...)
}

// QueryRow reads one row from the read pool.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.st.Reader().QueryRowContext(context.Background(), query, args...)
}

// Exec writes on the single write connection.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.st.ExecContext(context.Background(), query, args...)
}

// Tx is a write transaction.
type Tx struct {
	tx store.Tx
}

// Exec runs a statement in the transaction.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(context.Background(), query, args...)
}

// Query reads inside the transaction, so it sees the transaction's own writes. Close the rows
// before the next statement.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(context.Background(), query, args...)
}

// QueryRow reads one row inside the transaction.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(context.Background(), query, args...)
}

// WriteTx runs fn in one write transaction: it commits when fn returns nil and rolls back on an
// error. The store retries a busy begin.
func (d *DB) WriteTx(fn func(tx *Tx) error) error {
	return d.st.WriteTx(context.Background(), func(_ context.Context, tx store.Tx) error {
		return fn(&Tx{tx: tx})
	})
}
