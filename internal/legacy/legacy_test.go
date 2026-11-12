package legacy_test

import (
	"time"

	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/hollis-labs/libs/core/util/scheduler/sqlstore"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/schedmap"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/legacy"
	"github.com/hollis-labs/mentat/internal/persistence"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// buildLegacy writes a database in the pre-Substrate layout with synthetic rows: the final schema
// (what the 24 old migrations produced), the hand-rolled schema_migrations table at version 27 and
// no goose table. AUTOINCREMENT counters are pushed past the largest id, as deletions do.
func buildLegacy(t *testing.T, path string, orphan, duplicateEmbedding bool) {
	t.Helper()
	fsys, root := persistence.Migrations()
	ddl, err := fs.ReadFile(fsys, root+"/sqlite/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, line := range strings.Split(string(ddl), "\n") {
		if !strings.HasPrefix(line, "-- +goose") {
			keep = append(keep, line)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	schema := strings.Join(keep, "\n")
	if duplicateEmbedding {
		schema = strings.Replace(schema, ",\n    UNIQUE(target_kind, target_id, model)", "", 1)
	}
	mustExec(t, db, schema)
	mustExec(t, db, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`)
	mustExec(t, db, `INSERT INTO schema_migrations VALUES (1,'2026-01-01T00:00:00Z'),(27,'2026-02-01T00:00:00Z')`)

	mustExec(t, db, `INSERT INTO repos (id, name, url, created_at, updated_at) VALUES ('r1','Alpha','https://example.test/alpha','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('r2','Beta','https://example.test/beta','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO tags (id, name) VALUES ('t1','go'),('t2','mcp')`)
	mustExec(t, db, `INSERT INTO repo_tags (repo_id, tag_id) VALUES ('r1','t1'),('r2','t2')`)
	mustExec(t, db, `INSERT INTO audits (repo_id, scope, audit_type, actor_kind, actor_id, started_at, created_at, updated_at) VALUES ('r1','full','deep','agent','synthetic','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	for i := 1; i <= 40; i++ {
		mustExec(t, db, `INSERT INTO findings (repo_id, title, category, severity, description, status, audit_id, body_markdown, created_at, updated_at) VALUES ('r1', ?, 'gap', 'high', ?, 'open', 1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			fmt.Sprintf("Finding %d about retrieval and caching", i), fmt.Sprintf("description %d mentions scheduler", i), "body with sparkle words")
	}
	for i := 1; i <= 60; i++ {
		mustExec(t, db, `INSERT INTO symbols (repo_id, name, qualified_name, kind, language, file_path, docstring, content_hash, signature_hash, created_at, updated_at) VALUES ('r1', ?, ?, 'func', 'go', ?, 'doc text', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			fmt.Sprintf("Func%d", i), fmt.Sprintf("alpha.Func%d", i), fmt.Sprintf("internal/pkg%d/file.go", i%5), fmt.Sprintf("hash%d", i), fmt.Sprintf("sig%d", i))
	}
	for i := 1; i <= 12; i++ {
		mustExec(t, db, `INSERT INTO code_references (repo_id, file_path, description, ref_type, finding_id, created_at) VALUES ('r1', ?, ?, 'finding', ?, '2026-01-01T00:00:00Z')`,
			fmt.Sprintf("internal/pkg%d/handler.go", i%4), fmt.Sprintf("reference %d near the scheduler", i), i)
	}
	for i := 1; i <= 30; i++ {
		mustExec(t, db, `INSERT INTO relationships (repo_id, src_symbol_id, dst_symbol_id, kind, weight, source, discovered_at) VALUES ('r1', ?, ?, 'references', 1.5, 'scip', '2026-01-01T00:00:00Z')`, i, i+1)
	}
	for i := 1; i <= 5; i++ {
		blob := bytes.Repeat([]byte{byte(i), 0, 255, byte(i * 3)}, 64) // contains NULs: must survive byte for byte
		mustExec(t, db, `INSERT INTO embeddings (target_kind, target_id, model, dim, vector, content_hash, created_at) VALUES ('symbol', ?, 'm', 256, ?, 'h', '2026-01-01T00:00:00Z')`, i, blob)
	}
	// The old schedules table (it no longer exists in the new schema): every column holds a distinctive value.
	mustExec(t, db, oldSchedulesDDL)
	mustExec(t, db, `INSERT INTO schedules (id, repo_id, name, cron_expr, job_kind, payload_json, enabled, max_attempts, retry_backoff, retry_delay_secs, last_run_at, next_run_at, created_at, updated_at)
VALUES ('s1','r1','sweep','0 */5 * * * *','git-history-sweep','{"repo_path":"/synthetic/path","force":true}',1,1,'fixed',1,'2026-05-14T09:21:33Z','2026-05-14T09:25:00Z','2026-04-02T11:12:13Z','2026-05-14T09:21:34Z'),
('s2','r2','weekly distinct name','0 17 3 * * 2','symbol-reindex','{"k":"v"}',0,4,'exponential',9,NULL,NULL,'2026-03-03T03:03:03Z','2026-03-04T04:04:04Z')`)
	mustExec(t, db, `INSERT INTO jobs (id, schedule_id, repo_id, kind, status, retry_backoff, max_attempts, retry_delay_secs, created_at, updated_at) VALUES
('j1','s1','r1','git-history-sweep','failed','fixed',1,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),
('j2','s1','r1','git-history-sweep','completed','fixed',1,1,'2026-01-02T00:00:00Z','2026-01-02T00:00:00Z'),
('j3','s-removed-long-ago','r1','git-history-sweep','completed','fixed',1,1,'2026-01-03T00:00:00Z','2026-01-03T00:00:00Z'),
('j4',NULL,'r1','scan','completed','fixed',1,1,'2026-01-04T00:00:00Z','2026-01-04T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO job_events (job_id, schedule_id, repo_id, job_kind, event_type, status, message, created_at) VALUES ('j1','s1','r1','git-history-sweep','failed','failed','git log failed','2026-01-01T00:00:00Z'),('j3','s-removed-long-ago','r1','git-history-sweep','completed','completed','done','2026-01-03T00:00:00Z')`)
	// Deleted rows leave the counters ahead of the largest id.
	mustExec(t, db, `INSERT INTO findings (repo_id, title, category, severity, description, status, created_at, updated_at) VALUES ('r1','temp','gap','low','x','open','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	mustExec(t, db, `DELETE FROM findings WHERE title = 'temp'`)
	mustExec(t, db, `UPDATE sqlite_sequence SET seq = 500 WHERE name = 'findings'`)
	if orphan {
		// The old code enforced foreign keys on only some of its pooled connections.
		mustExec(t, db, `PRAGMA foreign_keys = OFF`)
		mustExec(t, db, `INSERT INTO job_events (job_id, job_kind, event_type, created_at) VALUES ('gone','git-history-sweep','failed','2026-01-01T00:00:00Z')`)
	}
	if duplicateEmbedding {
		// A row the new schema cannot hold (UNIQUE target_kind, target_id, model): the copy must fail
		// part-way and leave the target as it was.
		mustExec(t, db, `INSERT INTO embeddings (target_kind, target_id, model, dim, vector, content_hash, created_at) VALUES ('symbol', 1, 'm', 4, x'00', 'h', '2026-01-01T00:00:00Z')`)
	}
}

const oldSchedulesDDL = `CREATE TABLE schedules (
    id               TEXT PRIMARY KEY,
    repo_id          TEXT REFERENCES repos(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    cron_expr        TEXT NOT NULL,
    job_kind         TEXT NOT NULL,
    payload_json     TEXT NOT NULL DEFAULT '{}',
    enabled          INTEGER NOT NULL DEFAULT 1,
    max_attempts     INTEGER NOT NULL DEFAULT 1,
    retry_backoff    TEXT NOT NULL DEFAULT 'fixed',
    retry_delay_secs INTEGER NOT NULL DEFAULT 1,
    last_run_at      TEXT,
    next_run_at      TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
)`

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%.80s: %v", q, err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func count(t *testing.T, s *sqlite.Store, table string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRunCopiesEverythingAndProvesIt(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.db")
	buildLegacy(t, src, true, false)
	before := fileHash(t, src)

	target, err := sqlite.Open(filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	if n := count(t, target, "review_dimensions"); n == 0 {
		t.Fatal("the new database should start with its seed dimensions")
	}

	var out bytes.Buffer
	rep, err := legacy.Run(context.Background(), legacy.Options{Source: src, Target: filepath.Join(dir, "new.db"), Out: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := fileHash(t, src); got != before {
		t.Fatal("the source file changed")
	}
	rows := map[string]int64{}
	for _, tr := range rep.Tables {
		if tr.SourceRows != tr.TargetRows || tr.SourceDigest != tr.TargetDigest {
			t.Errorf("%s: %d/%d rows, digests %s/%s", tr.Name, tr.SourceRows, tr.TargetRows, tr.SourceDigest, tr.TargetDigest)
		}
		rows[tr.Name] = tr.TargetRows
	}
	for name, want := range map[string]int64{"repos": 2, "findings": 40, "symbols": 60, "relationships": 30, "code_references": 12, "embeddings": 5, "jobs": 4, "job_events": 3, "tags": 2, "repo_tags": 2} {
		if rows[name] != want {
			t.Errorf("%s: %d rows reported, want %d", name, rows[name], want)
		}
		if got := count(t, target, name); int64(got) != want {
			t.Errorf("%s: %d rows in the target, want %d", name, got, want)
		}
	}
	if len(rep.CarriedViolations) != 1 || !strings.Contains(rep.CarriedViolations[0], "job_events") {
		t.Fatalf("carried violations: %v", rep.CarriedViolations)
	}
	if rep.WALBytes != 0 {
		t.Fatalf("WAL left behind: %d bytes", rep.WALBytes)
	}
	if !rep.IntegrityOK || !rep.ForeignKeysOK || !rep.SequencesOK || len(rep.FTS) != 3 {
		t.Fatalf("report: %+v", rep)
	}
	for _, f := range rep.FTS {
		if f.Queries == 0 || f.Hits == 0 {
			t.Errorf("fts %s proved nothing: %+v", f.Name, f)
		}
	}
	if _, ok := rows["schedules"]; ok {
		t.Error("schedules are moved to the scheduler module, not copied as a table")
	}
	assertSchedulesMoved(t, target, rep)
	// Ids and counters survive: a new finding continues after the source's counter, not after max(id).
	var seq int64
	if err := target.DB().QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='findings'`).Scan(&seq); err != nil || seq < 500 {
		t.Fatalf("findings counter = %d, %v", seq, err)
	}
	// Embedding BLOBs are byte-identical (the digest covers it; this checks one directly).
	var blob []byte
	if err := target.DB().QueryRow(`SELECT vector FROM embeddings WHERE target_id = 3`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob, bytes.Repeat([]byte{3, 0, 255, 9}, 64)) {
		t.Fatal("embedding blob differs")
	}
	if !strings.Contains(out.String(), "integrity_check ok") {
		t.Fatalf("report text: %s", out.String())
	}

	// A second run refuses: the target holds data now.
	if _, err := legacy.Run(context.Background(), legacy.Options{Source: src, Target: filepath.Join(dir, "new.db")}); err == nil || !strings.Contains(err.Error(), "already holds data") {
		t.Fatalf("second run: %v", err)
	}
}

func TestRunRollsBackWhenTheCopyFailsPartWay(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.db")
	buildLegacy(t, src, false, true)
	target, err := sqlite.Open(filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	seeds := count(t, target, "review_dimensions")

	_, err = legacy.Run(context.Background(), legacy.Options{Source: src, Target: filepath.Join(dir, "new.db")})
	if err == nil {
		t.Fatal("a source the new schema cannot hold must not be accepted")
	}
	if n := count(t, target, "repos"); n != 0 {
		t.Fatalf("repos = %d after a failed run, want 0", n)
	}
	if n := count(t, target, "review_dimensions"); n != seeds {
		t.Fatalf("seed dimensions = %d after a failed run, want %d", n, seeds)
	}
	// The schedules written before the copy are taken back out.
	if n := count(t, target, "gosched_schedules"); n != 0 {
		t.Fatalf("gosched_schedules = %d after a failed run, want 0", n)
	}
}

// A schedule the scheduler cannot express (a seconds field other than 0) stops the migration before anything is written.
func TestRunRefusesASchedulePerSecond(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.db")
	buildLegacy(t, src, false, false)
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE schedules SET cron_expr = '*/10 * * * * *' WHERE id = 's1'`)
	_ = db.Close()
	target, err := sqlite.Open(filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	if _, err := legacy.Run(context.Background(), legacy.Options{Source: src, Target: filepath.Join(dir, "new.db")}); err == nil || !strings.Contains(err.Error(), "s1") {
		t.Fatalf("run: %v", err)
	}
	if n := count(t, target, "repos"); n != 0 {
		t.Fatalf("repos = %d, want 0", n)
	}
}

func TestRunRefusesWhatIsNotALegacySource(t *testing.T) {
	dir := t.TempDir()
	target, err := sqlite.Open(filepath.Join(dir, "new.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	newPath := filepath.Join(dir, "new.db")

	if _, err := legacy.Run(context.Background(), legacy.Options{Source: newPath, Target: newPath}); err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("same file: %v", err)
	}
	other, err := sqlite.Open(filepath.Join(dir, "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if _, err := legacy.Run(context.Background(), legacy.Options{Source: filepath.Join(dir, "other.db"), Target: newPath}); err == nil || !strings.Contains(err.Error(), "not a pre-Substrate") {
		t.Fatalf("new-layout source: %v", err)
	}
	if _, err := legacy.Run(context.Background(), legacy.Options{Source: filepath.Join(dir, "missing.db"), Target: newPath}); err == nil {
		t.Fatal("a missing source must be refused")
	}
}

// Every field of the old schedules is readable afterwards through the service, in the shape the CLI has always printed, and
// the job history keeps its schedule ids.
func assertSchedulesMoved(t *testing.T, target *sqlite.Store, rep *legacy.Report) {
	t.Helper()
	if rep.Schedules.Migrated != 2 || rep.Schedules.Refs != 3 || rep.Schedules.Distinct != 2 || rep.Schedules.Resolving != 1 {
		t.Fatalf("schedule report = %+v, want 2 moved, 3 refs, 2 distinct, 1 resolving", rep.Schedules)
	}
	sched, err := sqlstore.New(target.SQL())
	if err != nil {
		t.Fatal(err)
	}
	svc := jobs.NewService(jobs.Config{Store: target, Schedules: sched})
	got, err := svc.ListSchedules("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d schedules, want 2", len(got))
	}
	byID := map[string]domain.Schedule{}
	for _, g := range got {
		byID[g.ID] = g
	}
	ts := func(v string) time.Time {
		x, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	s1 := byID["s1"]
	if s1.RepoID != "r1" || s1.Name != "sweep" || s1.CronExpr != "0 */5 * * * *" || s1.JobKind != "git-history-sweep" ||
		s1.PayloadJSON != `{"repo_path":"/synthetic/path","force":true}` || !s1.Enabled || s1.MaxAttempts != 1 || s1.RetryBackoff != "fixed" ||
		s1.RetryDelaySecs != 1 || s1.LastRunAt == nil || !s1.LastRunAt.Equal(ts("2026-05-14T09:21:33Z")) ||
		!s1.CreatedAt.Equal(ts("2026-04-02T11:12:13Z")) || !s1.UpdatedAt.Equal(ts("2026-05-14T09:21:34Z")) || s1.NextRunAt == nil || !s1.NextRunAt.After(time.Now()) {
		t.Fatalf("schedule s1 = %+v", s1)
	}
	s2 := byID["s2"]
	if s2.RepoID != "r2" || s2.Name != "weekly distinct name" || s2.CronExpr != "0 17 3 * * 2" || s2.JobKind != "symbol-reindex" || s2.PayloadJSON != `{"k":"v"}` ||
		s2.Enabled || s2.MaxAttempts != 4 || s2.RetryBackoff != "exponential" || s2.RetryDelaySecs != 9 || s2.LastRunAt != nil ||
		!s2.CreatedAt.Equal(ts("2026-03-03T03:03:03Z")) || !s2.UpdatedAt.Equal(ts("2026-03-04T04:04:04Z")) {
		t.Fatalf("schedule s2 = %+v", s2)
	}
	// The old next_run_at is kept in the scheduler payload.
	rec, ok, err := sched.GetSchedule(context.Background(), "s1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if kept := schedmap.LegacyNextRun(rec.Payload); kept == nil || !kept.Equal(ts("2026-05-14T09:25:00Z")) {
		t.Fatalf("legacy next_run_at = %v", kept)
	}
	// The history rows still carry their ids, including the one whose schedule is gone.
	var n int
	if err := target.DB().QueryRow(`SELECT count(*) FROM jobs WHERE schedule_id IN ('s1','s-removed-long-ago')`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("jobs with a schedule id = %d, %v", n, err)
	}
}
