// Package legacy copies a pre-Substrate Mentat database (the hand-rolled schema_migrations layout)
// into a database created from the current schema, and proves the copy.
//
// The source is only ever attached read-only; it is never written, so it stays the rollback. The
// target must be a freshly migrated database that holds no data of its own. Every table is copied
// with its original primary keys in one write transaction, the full-text indexes are rebuilt, and
// the transaction commits only when the proofs pass: per-table row counts and a content digest
// match, foreign keys and the integrity check are clean, the AUTOINCREMENT counters are at least
// the source's, and the three FTS5 indexes return the same hits as the source's for a fixed query
// set. Any failure rolls the whole copy back and leaves the target as it was.
package legacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the "sqlite" driver
)

// Options name the two databases.
type Options struct {
	// Source is the pre-Substrate database file. It is attached read-only.
	Source string
	// Target is the database file created from the current schema and still empty of data.
	Target string
	// ScheduleZone is the IANA zone the migrated schedules' cron fields are read in (default UTC).
	ScheduleZone string
	// Out receives the human-readable report (optional).
	Out io.Writer
}

// TableReport is the proof for one table.
type TableReport struct {
	Name         string
	SourceRows   int64
	TargetRows   int64
	SourceDigest string
	TargetDigest string
}

// FTSReport is the proof for one full-text index.
type FTSReport struct {
	Name    string
	Queries int
	Hits    int
}

// Report is the outcome of a run that committed.
type Report struct {
	Tables        []TableReport
	FTS           []FTSReport
	IntegrityOK   bool
	ForeignKeysOK bool
	// CarriedViolations are foreign-key violations the source already had, carried unchanged
	// ("table rowid -> parent table"). Their number is the same in source and target.
	CarriedViolations []string
	SequencesOK       bool
	Schedules         ScheduleReport
	TargetBytes       int64 // database file plus WAL after the final checkpoint
	WALBytes          int64
	Started           time.Time
	Duration          time.Duration
	SourceBytes       int64
}

// crashAfterSchedules is set by tests to stop a run between the schedule step and the transaction, without the undo.
var crashAfterSchedules func() bool

// Run copies Source into Target. It returns an error, with the target unchanged, on any failed
// check.
func Run(ctx context.Context, o Options) (*Report, error) {
	start := time.Now()
	if o.Out == nil {
		o.Out = io.Discard
	}
	srcInfo, err := os.Stat(o.Source)
	if err != nil {
		return nil, fmt.Errorf("source database: %w", err)
	}
	if samePath(o.Source, o.Target) {
		return nil, errors.New("the source and the target are the same file; the old database is never opened in place")
	}
	if err := refuseNonLegacy(ctx, o.Source); err != nil {
		return nil, err
	}

	// One connection: ATTACH and the whole copy have to share it.
	// Foreign keys are checked at the end, not row by row: the old code only enforced them on some
	// of its pooled connections, so a source can hold rows whose parent is gone. Those rows are
	// data and are carried over unchanged; the proof is that the target has exactly the source's
	// violations and no others.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(o.Target)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(OFF)")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open target: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// The source is attached read-only through a URI: SQLite refuses any write to it.
	srcURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(o.Source), RawQuery: "mode=ro"}).String()
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS legacy", srcURI); err != nil {
		return nil, fmt.Errorf("attach the source read-only: %w", err)
	}
	tables, err := planTables(ctx, conn)
	if err != nil {
		return nil, err
	}
	if err := requireVirgin(ctx, conn, tables); err != nil {
		return nil, err
	}
	// The schedules table is not copied: its rows become records of the scheduler module (see schedules.go).
	var scheduleIDs []string
	var schedRep ScheduleReport
	undoSchedules := func() {}
	if hasTable(ctx, conn, "legacy", "schedules") {
		if schedRep, undoSchedules, err = migrateSchedules(ctx, conn, o.Target, o.ScheduleZone); err != nil {
			undoSchedules()
			return nil, err
		}
		ids, err := columnStrings(ctx, conn, "SELECT id FROM legacy.schedules")
		if err != nil {
			undoSchedules()
			return nil, err
		}
		scheduleIDs = ids
	}
	if crashAfterSchedules != nil && crashAfterSchedules() {
		// Test only: the process dies here, so the undo never runs.
		return nil, errors.New("simulated crash after the schedule step")
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		undoSchedules()
		return nil, fmt.Errorf("lock the target (is `mentat serve` running on it?): %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			undoSchedules()
		}
	}()

	if err := requireVirgin(ctx, conn, tables); err != nil {
		return nil, err
	}
	// Seed rows the schema migration inserted give way to the source's rows, children first.
	for i := len(tables) - 1; i >= 0; i-- {
		if tables[i].seeded {
			if _, err := conn.ExecContext(ctx, "DELETE FROM main."+q(tables[i].name)); err != nil {
				return nil, fmt.Errorf("clear seed rows of %s: %w", tables[i].name, err)
			}
		}
	}
	for _, t := range tables {
		cols := strings.Join(quoteAll(t.cols), ", ")
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("INSERT INTO main.%s (%s) SELECT %s FROM legacy.%s ORDER BY rowid", q(t.name), cols, cols, q(t.name))); err != nil {
			return nil, fmt.Errorf("copy %s: %w", t.name, err)
		}
	}
	if err := copySequences(ctx, conn); err != nil {
		return nil, err
	}
	for _, f := range ftsTables {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("INSERT INTO main.%s(%s) VALUES('rebuild')", q(f.index), q(f.index))); err != nil {
			return nil, fmt.Errorf("rebuild %s: %w", f.index, err)
		}
	}

	rep := &Report{Started: start, SourceBytes: srcInfo.Size()}
	for _, t := range tables {
		tr, err := proveTable(ctx, conn, t)
		if err != nil {
			return nil, err
		}
		rep.Tables = append(rep.Tables, tr)
	}
	if rep.FTS, err = proveFTS(ctx, conn); err != nil {
		return nil, err
	}
	if rep.CarriedViolations, err = proveIntegrity(ctx, conn); err != nil {
		return nil, err
	}
	rep.IntegrityOK, rep.ForeignKeysOK = true, true
	rep.Schedules = schedRep
	if rep.Schedules.Refs, rep.Schedules.Distinct, rep.Schedules.Resolving, err = proveScheduleRefs(ctx, conn, scheduleIDs); err != nil {
		return nil, err
	}
	if err := proveSequences(ctx, conn); err != nil {
		return nil, err
	}
	rep.SequencesOK = true

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	committed = true
	// Leave no WAL behind: fold it into the database file and truncate it.
	if _, err := conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}
	rep.TargetBytes, rep.WALBytes = fileSizes(o.Target)
	rep.Duration = time.Since(start)
	rep.write(o.Out)
	return rep, nil
}

func (r *Report) write(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%-26s %10s %10s  %s\n", "table", "source", "target", "content digest")
	for _, t := range r.Tables {
		status := "equal " + t.SourceDigest[:12]
		if t.SourceDigest != t.TargetDigest {
			status = "DIFFERENT"
		}
		_, _ = fmt.Fprintf(w, "%-26s %10d %10d  %s\n", t.Name, t.SourceRows, t.TargetRows, status)
	}
	for _, f := range r.FTS {
		_, _ = fmt.Fprintf(w, "fts %-22s %d queries, %d hits, same rowids in source and target\n", f.Name, f.Queries, f.Hits)
	}
	_, _ = fmt.Fprintf(w, "integrity_check ok, autoincrement counters >= source\n")
	if len(r.CarriedViolations) == 0 {
		_, _ = fmt.Fprintf(w, "foreign_key_check clean\n")
	} else {
		_, _ = fmt.Fprintf(w, "foreign_key_check: %d violation(s) carried over unchanged from the source: %s\n", len(r.CarriedViolations), strings.Join(r.CarriedViolations, "; "))
	}
	_, _ = fmt.Fprintf(w, "schedules: %d moved to the scheduler module, every field read back equal; job history keeps %d schedule_id values (%d distinct, %d still name a schedule), unchanged\n",
		r.Schedules.Migrated, r.Schedules.Refs, r.Schedules.Distinct, r.Schedules.Resolving)
	_, _ = fmt.Fprintf(w, "source %d bytes, copied in %s\n", r.SourceBytes, r.Duration.Round(time.Millisecond))
	_, _ = fmt.Fprintf(w, "target %d bytes after checkpoint, WAL %d bytes\n", r.TargetBytes, r.WALBytes)
}

// fileSizes returns the size of the database file and of its WAL (0 when absent).
func fileSizes(path string) (db, wal int64) {
	if fi, err := os.Stat(path); err == nil {
		db = fi.Size()
	}
	if fi, err := os.Stat(path + "-wal"); err == nil {
		wal = fi.Size()
	}
	return db, wal
}

// --- planning ---------------------------------------------------------------------------------

type table struct {
	name   string
	cols   []string
	deps   []string
	seeded bool
}

// seedTables are filled by the schema migration itself.
var seedTables = map[string]bool{"review_dimensions": true, "lenses": true, "lens_dimensions": true}

var ftsTables = []struct{ index, content, textCols string }{
	{"findings_fts", "findings", "title,description,body_markdown"},
	{"symbols_fts", "symbols", "name,qualified_name,file_path,docstring"},
	{"code_references_fts", "code_references", "file_path,description"},
}

var ftsShadow = regexp.MustCompile(`_fts(_data|_idx|_docsize|_config|_content)?$`)

func q(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = q(s)
	}
	return out
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

// refuseNonLegacy checks that the source is a pre-Substrate Mentat database.
func refuseNonLegacy(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}).String())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&n); err != nil {
		return fmt.Errorf("inspect the source: %w", err)
	}
	if n == 0 {
		return errors.New("the source has no schema_migrations table: it is not a pre-Substrate Mentat database")
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("the source already uses the Substrate layout; nothing to migrate")
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version < 27 {
		return fmt.Errorf("the source schema is at version %d; run the previous release against it first so it reaches 27", version)
	}
	return nil
}

func planTables(ctx context.Context, conn *sql.Conn) ([]table, error) {
	srcNames, err := listTables(ctx, conn, "legacy")
	if err != nil {
		return nil, err
	}
	mainNames, err := listTables(ctx, conn, "main")
	if err != nil {
		return nil, err
	}
	inMain := map[string]bool{}
	for _, n := range mainNames {
		inMain[n] = true
	}
	var tables []table
	for _, n := range srcNames {
		if n == "schema_migrations" || n == "schedules" {
			continue
		}
		if !inMain[n] {
			return nil, fmt.Errorf("the source table %s has no counterpart in the new schema; nothing is dropped silently", n)
		}
		srcInfo, err := columns(ctx, conn, "legacy", n)
		if err != nil {
			return nil, err
		}
		mainInfo, err := columns(ctx, conn, "main", n)
		if err != nil {
			return nil, err
		}
		// Names, declared types, NOT NULL and primary-key position all have to match: a changed affinity would
		// otherwise surface only as a digest mismatch after the copy.
		if a, b := describeColumns(srcInfo), describeColumns(mainInfo); a != b {
			return nil, fmt.Errorf("table %s differs: source columns [%s], new columns [%s]", n, a, b)
		}
		mainCols := make([]string, len(mainInfo))
		for i, c := range mainInfo {
			mainCols[i] = c.name
		}
		deps, err := foreignKeyTargets(ctx, conn, n)
		if err != nil {
			return nil, err
		}
		tables = append(tables, table{name: n, cols: mainCols, deps: deps, seeded: seedTables[n]})
	}
	return topoSort(tables)
}

// listTables returns the data tables of a schema: no SQLite internals, no FTS shadow tables, no
// goose bookkeeping, no tables of the identity module.
func listTables(ctx context.Context, conn *sql.Conn, schema string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT name, sql FROM %s.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%%' ORDER BY name`, schema))
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var name string
		var ddl sql.NullString
		if err := rows.Scan(&name, &ddl); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if (ddl.Valid && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ddl.String)), "CREATE VIRTUAL")) || ftsShadow.MatchString(name) {
			continue
		}
		if name == "goose_db_version" || strings.HasPrefix(name, "identity_") || strings.HasPrefix(name, "hollis_") {
			continue
		}
		out = append(out, name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

// colInfo is one row of pragma table_info.
type colInfo struct {
	name    string
	typ     string
	notNull bool
	pk      int
}

func describeColumns(cols []colInfo) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = fmt.Sprintf("%s %s notnull=%t pk=%d", c.name, c.typ, c.notNull, c.pk)
	}
	return strings.Join(parts, ", ")
}

func columns(ctx context.Context, conn *sql.Conn, schema, name string) ([]colInfo, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA %s.table_info(%s)`, schema, q(name)))
	if err != nil {
		return nil, err
	}
	var cols []colInfo
	for rows.Next() {
		var cid int
		var c colInfo
		var notnull int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &c.name, &c.typ, &notnull, &dflt, &c.pk); err != nil {
			_ = rows.Close()
			return nil, err
		}
		c.notNull = notnull != 0
		cols = append(cols, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return cols, rows.Err()
}

func foreignKeyTargets(ctx context.Context, conn *sql.Conn, name string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA main.foreign_key_list(%s)`, q(name)))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var id, seq int
		var target, from string
		var to, onUpdate, onDelete, match sql.NullString
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if target != name && !seen[target] {
			seen[target] = true
			out = append(out, target)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

// topoSort orders tables so a table follows the tables it references.
func topoSort(tables []table) ([]table, error) {
	byName := map[string]table{}
	for _, t := range tables {
		byName[t.name] = t
	}
	var names []string
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	state := map[string]int{}
	var out []table
	var visit func(string) error
	visit = func(n string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("foreign keys form a cycle at %s", n)
		case 2:
			return nil
		}
		state[n] = 1
		t, ok := byName[n]
		if !ok {
			state[n] = 2
			return nil // a referenced table that is not copied (none expected)
		}
		deps := append([]string(nil), t.deps...)
		sort.Strings(deps)
		for _, d := range deps {
			if err := visit(d); err != nil {
				return err
			}
		}
		state[n] = 2
		out = append(out, t)
		return nil
	}
	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// requireVirgin refuses a target that already holds data (outside the seeded tables), which also
// makes a second run refuse.
func requireVirgin(ctx context.Context, conn *sql.Conn, tables []table) error {
	for _, t := range tables {
		if t.seeded {
			continue
		}
		var n int64
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM main."+q(t.name)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("the target already holds data (%s has %d rows); migrate-legacy only fills a new, empty database", t.name, n)
		}
	}
	return nil
}

// --- sequences ---------------------------------------------------------------------------------

func copySequences(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT name, seq FROM legacy.sqlite_sequence`)
	if err != nil {
		return nil // a source without AUTOINCREMENT tables has no sqlite_sequence
	}
	type seq struct {
		name string
		n    int64
	}
	var all []seq
	for rows.Next() {
		var s seq
		if err := rows.Scan(&s.name, &s.n); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, s)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, s := range all {
		res, err := conn.ExecContext(ctx, `UPDATE main.sqlite_sequence SET seq = max(seq, ?) WHERE name = ?`, s.n, s.name)
		if err != nil {
			return fmt.Errorf("sequence %s: %w", s.name, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := conn.ExecContext(ctx, `INSERT INTO main.sqlite_sequence(name, seq) VALUES (?, ?)`, s.name, s.n); err != nil {
				return fmt.Errorf("sequence %s: %w", s.name, err)
			}
		}
	}
	return nil
}

func proveSequences(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT l.name, l.seq, COALESCE(m.seq, -1) FROM legacy.sqlite_sequence l LEFT JOIN main.sqlite_sequence m ON m.name = l.name`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var src, dst int64
		if err := rows.Scan(&name, &src, &dst); err != nil {
			return err
		}
		if dst < src {
			return fmt.Errorf("autoincrement counter of %s is %d in the target, %d in the source", name, dst, src)
		}
	}
	return rows.Err()
}

// --- proofs ------------------------------------------------------------------------------------

func proveTable(ctx context.Context, conn *sql.Conn, t table) (TableReport, error) {
	tr := TableReport{Name: t.name}
	var err error
	if tr.SourceRows, tr.SourceDigest, err = digest(ctx, conn, "legacy", t); err != nil {
		return tr, err
	}
	if tr.TargetRows, tr.TargetDigest, err = digest(ctx, conn, "main", t); err != nil {
		return tr, err
	}
	if tr.SourceRows != tr.TargetRows {
		return tr, fmt.Errorf("%s: %d rows in the source, %d in the target", t.name, tr.SourceRows, tr.TargetRows)
	}
	if tr.SourceDigest != tr.TargetDigest {
		return tr, fmt.Errorf("%s: row contents differ between the source and the target", t.name)
	}
	if err := proveInSQL(ctx, conn, t); err != nil {
		return tr, err
	}
	return tr, nil
}

// proveInSQL is a second proof that does not go through the Go driver (digest does, and a driver conversion applies to
// both sides alike): SQLite itself compares the two tables with EXCEPT in both directions, and the number of values of
// each storage class (typeof) per column has to be equal.
func proveInSQL(ctx context.Context, conn *sql.Conn, t table) error {
	cols := strings.Join(quoteAll(t.cols), ", ")
	for _, dir := range [][2]string{{"legacy", "main"}, {"main", "legacy"}} {
		var n int64
		query := fmt.Sprintf("SELECT count(*) FROM (SELECT %s FROM %s.%s EXCEPT SELECT %s FROM %s.%s)", cols, dir[0], q(t.name), cols, dir[1], q(t.name))
		if err := conn.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return fmt.Errorf("%s: compare %s with %s: %w", t.name, dir[0], dir[1], err)
		}
		if n != 0 {
			return fmt.Errorf("%s: %d rows in %s have no identical row in %s", t.name, n, dir[0], dir[1])
		}
	}
	for _, c := range t.cols {
		var counts [2]string
		for i, schema := range []string{"legacy", "main"} {
			rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT typeof(%s), count(*) FROM %s.%s GROUP BY 1 ORDER BY 1", q(c), schema, q(t.name)))
			if err != nil {
				return fmt.Errorf("%s.%s: typeof counts: %w", t.name, c, err)
			}
			var parts []string
			for rows.Next() {
				var typ string
				var n int64
				if err := rows.Scan(&typ, &n); err != nil {
					_ = rows.Close()
					return err
				}
				parts = append(parts, fmt.Sprintf("%s=%d", typ, n))
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}
			counts[i] = strings.Join(parts, " ")
		}
		if counts[0] != counts[1] {
			return fmt.Errorf("%s.%s: storage classes differ: source [%s], target [%s]", t.name, c, counts[0], counts[1])
		}
	}
	return nil
}

// digest hashes every value of every row in rowid order, each tagged with its storage class, so
// two tables with equal digests hold identical rows, BLOBs byte for byte.
func digest(ctx context.Context, conn *sql.Conn, schema string, t table) (int64, string, error) {
	cols := strings.Join(quoteAll(t.cols), ", ")
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s.%s ORDER BY rowid", cols, schema, q(t.name)))
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = rows.Close() }()
	h := sha256.New()
	vals := make([]any, len(t.cols))
	ptrs := make([]any, len(t.cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var n int64
	var buf [8]byte
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return 0, "", err
		}
		for _, v := range vals {
			switch x := v.(type) {
			case nil:
				h.Write([]byte{0})
			case int64:
				h.Write([]byte{1})
				binary.BigEndian.PutUint64(buf[:], uint64(x))
				h.Write(buf[:])
			case float64:
				h.Write([]byte{2})
				binary.BigEndian.PutUint64(buf[:], math.Float64bits(x))
				h.Write(buf[:])
			case string:
				h.Write([]byte{3})
				binary.BigEndian.PutUint64(buf[:], uint64(len(x)))
				h.Write(buf[:])
				h.Write([]byte(x))
			case []byte:
				h.Write([]byte{4})
				binary.BigEndian.PutUint64(buf[:], uint64(len(x)))
				h.Write(buf[:])
				h.Write(x)
			case bool:
				h.Write([]byte{5})
				if x {
					h.Write([]byte{1})
				} else {
					h.Write([]byte{0})
				}
			case time.Time:
				h.Write([]byte{6})
				h.Write([]byte(x.UTC().Format(time.RFC3339Nano)))
			default:
				return 0, "", fmt.Errorf("%s.%s: unexpected value type %T", schema, t.name, v)
			}
		}
		h.Write([]byte{'\n'})
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

var word = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]{3,}`)

// proveFTS runs a fixed query set on the source's index and the rebuilt one and compares the hits.
// The terms are the words of the first rows of the content table in id order, so the set is the
// same on every run over the same data.
func proveFTS(ctx context.Context, conn *sql.Conn) ([]FTSReport, error) {
	var out []FTSReport
	for _, f := range ftsTables {
		terms, err := ftsTerms(ctx, conn, f.content, f.textCols)
		if err != nil {
			return nil, err
		}
		fr := FTSReport{Name: f.index}
		for _, term := range terms {
			src, err := ftsHits(ctx, conn, "legacy", f.index, term)
			if err != nil {
				return nil, fmt.Errorf("%s on the source for %q: %w", f.index, term, err)
			}
			dst, err := ftsHits(ctx, conn, "main", f.index, term)
			if err != nil {
				return nil, fmt.Errorf("%s on the target for %q: %w", f.index, term, err)
			}
			if fmt.Sprint(src) != fmt.Sprint(dst) {
				return nil, fmt.Errorf("%s: the query %q hits %d rows in the source and %d in the target", f.index, term, len(src), len(dst))
			}
			fr.Queries++
			fr.Hits += len(dst)
		}
		// The index agrees with its content table.
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("INSERT INTO main.%s(%s, rank) VALUES('integrity-check', 1)", q(f.index), q(f.index))); err != nil {
			return nil, fmt.Errorf("%s integrity-check: %w", f.index, err)
		}
		out = append(out, fr)
	}
	return out, nil
}

func ftsTerms(ctx context.Context, conn *sql.Conn, content, cols string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM main.%s ORDER BY id LIMIT 300`, strings.ReplaceAll(cols, ",", "||' '||"), q(content)))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var terms []string
	for rows.Next() {
		var text sql.NullString
		if err := rows.Scan(&text); err != nil {
			_ = rows.Close()
			return nil, err
		}
		for _, w := range word.FindAllString(text.String, -1) {
			w = strings.ToLower(w)
			if !seen[w] {
				seen[w] = true
				terms = append(terms, w)
			}
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sort.Strings(terms)
	var picked []string
	for i := 0; i < len(terms) && len(picked) < 25; i += max(1, len(terms)/25) {
		picked = append(picked, terms[i])
	}
	return picked, rows.Err()
}

func ftsHits(ctx context.Context, conn *sql.Conn, schema, index, term string) ([]int64, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT rowid FROM %s.%s WHERE %s MATCH ? ORDER BY rowid`, schema, q(index), q(index)), `"`+term+`"`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return ids, rows.Err()
}

// proveIntegrity runs integrity_check on the target and compares its foreign-key violations with
// the source's: they must be the same rows (the source's orphans are carried, nothing new appears).
// It returns the carried violations.
func proveIntegrity(ctx context.Context, conn *sql.Conn) ([]string, error) {
	var res string
	if err := conn.QueryRowContext(ctx, "PRAGMA main.integrity_check").Scan(&res); err != nil {
		return nil, err
	}
	if res != "ok" {
		return nil, fmt.Errorf("integrity_check on the target: %s", res)
	}
	src, err := foreignKeyViolations(ctx, conn, "legacy")
	if err != nil {
		return nil, err
	}
	dst, err := foreignKeyViolations(ctx, conn, "main")
	if err != nil {
		return nil, err
	}
	if strings.Join(src, "\n") != strings.Join(dst, "\n") {
		return nil, fmt.Errorf("foreign_key_check differs: the source has %d violation(s), the target %d", len(src), len(dst))
	}
	return dst, nil
}

// foreignKeyViolations lists "table rowid -> parent" for every violation in a schema, sorted.
func foreignKeyViolations(ctx context.Context, conn *sql.Conn, schema string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA "+schema+".foreign_key_check")
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var tbl, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&tbl, &rowid, &parent, &fkid); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s rowid %d -> %s", tbl, rowid.Int64, parent))
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, rows.Err()
}

func hasTable(ctx context.Context, conn *sql.Conn, schema, name string) bool {
	var n int
	err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.sqlite_master WHERE type='table' AND name=?`, schema), name).Scan(&n)
	return err == nil && n > 0
}

func columnStrings(ctx context.Context, conn *sql.Conn, query string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
