package legacy

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/hollis-labs/libs/core/util/scheduler/sqlstore"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/schedmap"
)

// ScheduleReport is what the schedules step moved and proved.
type ScheduleReport struct {
	Migrated int
	// Job history keeps its schedule_id values as they were: Refs is how many job rows carry one, Distinct how many
	// different ids, Resolving how many of those ids are a schedule that still exists.
	Refs, Distinct, Resolving int64
}

// migrateSchedules converts the source's schedules table into scheduler-module records (libs sqlstore). The records
// are written before the main copy and read back and compared field by field with the source rows; the returned
// undo removes them again if the copy fails afterwards.
func migrateSchedules(ctx context.Context, src *sql.Conn, targetPath, zone string) (rep ScheduleReport, undo func(), err error) {
	undo = func() {}
	rows, err := src.QueryContext(ctx, `SELECT id, COALESCE(repo_id,''), name, cron_expr, job_kind, payload_json, enabled, max_attempts,
retry_backoff, retry_delay_secs, COALESCE(last_run_at,''), COALESCE(next_run_at,''), created_at, updated_at FROM legacy.schedules ORDER BY rowid`)
	if err != nil {
		return rep, undo, fmt.Errorf("read the source schedules: %w", err)
	}
	type old struct {
		item       domain.Schedule
		legacyNext *time.Time
	}
	var all []old
	for rows.Next() {
		var o old
		var enabled int
		var last, next, created, updated string
		if err := rows.Scan(&o.item.ID, &o.item.RepoID, &o.item.Name, &o.item.CronExpr, &o.item.JobKind, &o.item.PayloadJSON, &enabled,
			&o.item.MaxAttempts, &o.item.RetryBackoff, &o.item.RetryDelaySecs, &last, &next, &created, &updated); err != nil {
			_ = rows.Close()
			return rep, undo, err
		}
		o.item.Enabled = enabled != 0
		if o.item.LastRunAt, err = parseOptional(last); err != nil {
			_ = rows.Close()
			return rep, undo, fmt.Errorf("schedule %s last_run_at: %w", o.item.ID, err)
		}
		if o.legacyNext, err = parseOptional(next); err != nil {
			_ = rows.Close()
			return rep, undo, fmt.Errorf("schedule %s next_run_at: %w", o.item.ID, err)
		}
		if o.item.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			_ = rows.Close()
			return rep, undo, fmt.Errorf("schedule %s created_at: %w", o.item.ID, err)
		}
		if o.item.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
			_ = rows.Close()
			return rep, undo, fmt.Errorf("schedule %s updated_at: %w", o.item.ID, err)
		}
		all = append(all, o)
	}
	if err := rows.Err(); err != nil {
		return rep, undo, err
	}
	_ = rows.Close()

	// A second handle, for the scheduler module's own store (the copy holds the first one).
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(targetPath)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return rep, undo, err
	}
	defer func() { _ = db.Close() }()
	if err := sqlstore.Migrate(ctx, db); err != nil {
		return rep, undo, err
	}
	store, err := sqlstore.New(db)
	if err != nil {
		return rep, undo, err
	}
	if existing, err := store.ListSchedules(ctx); err != nil {
		return rep, undo, err
	} else if len(existing) > 0 {
		return rep, undo, fmt.Errorf("the target already holds %d schedules; migrate-legacy only fills a new, empty database", len(existing))
	}

	now := time.Now().UTC()
	var created []string
	undo = func() {
		cleanup, err := sql.Open("sqlite", "file:"+filepath.ToSlash(targetPath)+"?_pragma=busy_timeout(5000)")
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		if s, err := sqlstore.New(cleanup); err == nil {
			for _, id := range created {
				_ = s.DeleteSchedule(context.Background(), id)
			}
		}
	}
	// Convert everything first, so a schedule that cannot be expressed stops the migration before any write.
	for _, o := range all {
		item := o.item
		rec, err := schedmap.ToRecord(&item, now, zone, o.legacyNext)
		if err != nil {
			return rep, undo, fmt.Errorf("schedule %s (%s): %w", o.item.ID, o.item.Name, err)
		}
		if err := store.CreateSchedule(ctx, rec); err != nil {
			return rep, undo, fmt.Errorf("write schedule %s: %w", o.item.ID, err)
		}
		created = append(created, o.item.ID)
	}
	for _, o := range all {
		rec, ok, err := store.GetSchedule(ctx, o.item.ID)
		if err != nil || !ok {
			return rep, undo, fmt.Errorf("schedule %s was not stored (%v)", o.item.ID, err)
		}
		got, err := schedmap.FromRecord(rec)
		if err != nil {
			return rep, undo, err
		}
		if err := sameSchedule(o.item, got); err != nil {
			return rep, undo, fmt.Errorf("schedule %s changed in the move: %w", o.item.ID, err)
		}
		if kept := schedmap.LegacyNextRun(rec.Payload); (kept == nil) != (o.legacyNext == nil) || (kept != nil && !kept.Equal(*o.legacyNext)) {
			return rep, undo, fmt.Errorf("schedule %s: next_run_at was not kept in the payload", o.item.ID)
		}
	}
	rep.Migrated = len(all)
	return rep, undo, nil
}

func parseOptional(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

// sameSchedule compares every configuration field; next_run_at is runtime state (the old engine recomputed it
// at each start) and is kept in the payload instead.
func sameSchedule(want, got domain.Schedule) error {
	switch {
	case want.ID != got.ID:
		return fmt.Errorf("id %q -> %q", want.ID, got.ID)
	case want.RepoID != got.RepoID:
		return fmt.Errorf("repo_id %q -> %q", want.RepoID, got.RepoID)
	case want.Name != got.Name:
		return fmt.Errorf("name %q -> %q", want.Name, got.Name)
	case want.CronExpr != got.CronExpr:
		return fmt.Errorf("cron_expr %q -> %q", want.CronExpr, got.CronExpr)
	case want.JobKind != got.JobKind:
		return fmt.Errorf("job_kind %q -> %q", want.JobKind, got.JobKind)
	case schedmap.DefaultJSON(want.PayloadJSON) != got.PayloadJSON:
		return fmt.Errorf("payload_json %q -> %q", want.PayloadJSON, got.PayloadJSON)
	case want.Enabled != got.Enabled:
		return fmt.Errorf("enabled %v -> %v", want.Enabled, got.Enabled)
	case want.MaxAttempts != got.MaxAttempts:
		return fmt.Errorf("max_attempts %d -> %d", want.MaxAttempts, got.MaxAttempts)
	case want.RetryBackoff != got.RetryBackoff:
		return fmt.Errorf("retry_backoff %q -> %q", want.RetryBackoff, got.RetryBackoff)
	case want.RetryDelaySecs != got.RetryDelaySecs:
		return fmt.Errorf("retry_delay_secs %d -> %d", want.RetryDelaySecs, got.RetryDelaySecs)
	case !want.CreatedAt.Equal(got.CreatedAt):
		return fmt.Errorf("created_at %s -> %s", want.CreatedAt, got.CreatedAt)
	case !want.UpdatedAt.Equal(got.UpdatedAt):
		return fmt.Errorf("updated_at %s -> %s", want.UpdatedAt, got.UpdatedAt)
	case (want.LastRunAt == nil) != (got.LastRunAt == nil) || (want.LastRunAt != nil && !want.LastRunAt.Equal(*got.LastRunAt)):
		return fmt.Errorf("last_run_at %v -> %v", want.LastRunAt, got.LastRunAt)
	}
	return nil
}

// proveScheduleRefs checks, inside the copy transaction, that the schedule ids the job history carries are the
// source's, byte for byte (the same rows carry one, the same set of ids), and counts how many still name a schedule.
func proveScheduleRefs(ctx context.Context, conn *sql.Conn, schedules []string) (refs, distinct, resolving int64, err error) {
	known := map[string]bool{}
	for _, id := range schedules {
		known[id] = true
	}
	for _, table := range []string{"jobs", "job_events"} {
		src, err := scheduleIDs(ctx, conn, "legacy", table)
		if err != nil {
			return 0, 0, 0, err
		}
		dst, err := scheduleIDs(ctx, conn, "main", table)
		if err != nil {
			return 0, 0, 0, err
		}
		if src.rows != dst.rows || src.set != dst.set {
			return 0, 0, 0, fmt.Errorf("%s: schedule_id differs between source (%d rows) and target (%d rows)", table, src.rows, dst.rows)
		}
		if table == "jobs" {
			refs, distinct = src.rows, int64(len(src.ids))
			for _, id := range src.ids {
				if known[id] {
					resolving++
				}
			}
		}
	}
	return refs, distinct, resolving, nil
}

type idSet struct {
	rows int64    // rows with a schedule_id
	ids  []string // the distinct values, sorted
	set  string   // the distinct values joined, for comparison
}

func scheduleIDs(ctx context.Context, conn *sql.Conn, schema, table string) (idSet, error) {
	var out idSet
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(schedule_id) FROM %s.%s`, schema, q(table))).Scan(&out.rows); err != nil {
		return out, err
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT DISTINCT schedule_id FROM %s.%s WHERE schedule_id IS NOT NULL ORDER BY schedule_id`, schema, q(table)))
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return out, err
		}
		out.ids = append(out.ids, id)
		out.set += id + "\x1f"
	}
	return out, rows.Err()
}
