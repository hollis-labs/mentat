package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

type EventFilter struct {
	RepoID     string
	ScheduleID string
	JobID      string
	JobKind    string
	Status     string
	SinceID    int64
	Limit      int
}

func (s *Store) CreateSchedule(item *domain.Schedule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = 1
	}
	if item.RetryDelaySecs <= 0 {
		item.RetryDelaySecs = 1
	}
	if strings.TrimSpace(item.RetryBackoff) == "" {
		item.RetryBackoff = "fixed"
	}
	if _, err := s.db.Exec(`INSERT INTO schedules
(id, repo_id, name, cron_expr, job_kind, payload_json, enabled, max_attempts, retry_backoff, retry_delay_secs, last_run_at, next_run_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, nullableString(item.RepoID), item.Name, item.CronExpr, item.JobKind, defaultJSON(item.PayloadJSON),
		boolToInt(item.Enabled), item.MaxAttempts, item.RetryBackoff, item.RetryDelaySecs,
		timePtrString(item.LastRunAt), timePtrString(item.NextRunAt), now, now,
	); err != nil {
		return fmt.Errorf("insert schedule: %w", err)
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	item.UpdatedAt = item.CreatedAt
	return nil
}

func (s *Store) GetSchedule(id string) (*domain.Schedule, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, name, cron_expr, job_kind, payload_json, enabled, max_attempts, retry_backoff, retry_delay_secs, last_run_at, next_run_at, created_at, updated_at
FROM schedules WHERE id = ?`, id)
	item, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return item, nil
}

func (s *Store) ListSchedules(repoID string, enabledOnly bool) ([]domain.Schedule, error) {
	query := `SELECT id, repo_id, name, cron_expr, job_kind, payload_json, enabled, max_attempts, retry_backoff, retry_delay_secs, last_run_at, next_run_at, created_at, updated_at FROM schedules WHERE 1=1`
	var args []any
	if repoID != "" {
		query += " AND repo_id = ?"
		args = append(args, repoID)
	}
	if enabledOnly {
		query += " AND enabled = 1"
	}
	query += " ORDER BY name, id"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	var items []domain.Schedule
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteSchedule(id string) error {
	if _, err := s.db.Exec(`DELETE FROM schedules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func (s *Store) UpdateScheduleRuntime(id string, lastRunAt, nextRunAt *time.Time, enabled bool) error {
	if _, err := s.db.Exec(`UPDATE schedules SET last_run_at = ?, next_run_at = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		timePtrString(lastRunAt), timePtrString(nextRunAt), boolToInt(enabled), time.Now().UTC().Format(time.RFC3339), id,
	); err != nil {
		return fmt.Errorf("update schedule runtime: %w", err)
	}
	return nil
}

func (s *Store) CreateJob(item *domain.Job) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = 1
	}
	if item.RetryDelaySecs <= 0 {
		item.RetryDelaySecs = 1
	}
	if strings.TrimSpace(item.RetryBackoff) == "" {
		item.RetryBackoff = "fixed"
	}
	if _, err := s.db.Exec(`INSERT INTO jobs
(id, schedule_id, repo_id, kind, status, attempt_count, max_attempts, retry_backoff, retry_delay_secs, payload_json, output_json, error, started_at, finished_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, nullableString(item.ScheduleID), nullableString(item.RepoID), item.Kind, item.Status, item.AttemptCount,
		item.MaxAttempts, item.RetryBackoff, item.RetryDelaySecs, defaultJSON(item.PayloadJSON), defaultJSON(item.OutputJSON),
		item.Error, timePtrString(item.StartedAt), timePtrString(item.FinishedAt), now, now,
	); err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	item.UpdatedAt = item.CreatedAt
	return nil
}

func (s *Store) GetJob(id string) (*domain.Job, error) {
	row := s.db.QueryRow(`SELECT id, schedule_id, repo_id, kind, status, attempt_count, max_attempts, retry_backoff, retry_delay_secs, payload_json, output_json, error, started_at, finished_at, created_at, updated_at
FROM jobs WHERE id = ?`, id)
	item, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}
	return item, nil
}

func (s *Store) ListJobs(repoID, status, kind string, limit int) ([]domain.Job, error) {
	query := `SELECT id, schedule_id, repo_id, kind, status, attempt_count, max_attempts, retry_backoff, retry_delay_secs, payload_json, output_json, error, started_at, finished_at, created_at, updated_at
FROM jobs WHERE 1=1`
	var args []any
	if repoID != "" {
		query += " AND repo_id = ?"
		args = append(args, repoID)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}
	query += " ORDER BY created_at DESC, id DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var items []domain.Job
	for rows.Next() {
		item, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateJobAttempt(id string, attempt int, status string, startedAt *time.Time) error {
	if _, err := s.db.Exec(`UPDATE jobs SET attempt_count = ?, status = ?, started_at = ?, updated_at = ? WHERE id = ?`,
		attempt, status, timePtrString(startedAt), time.Now().UTC().Format(time.RFC3339), id,
	); err != nil {
		return fmt.Errorf("update job attempt: %w", err)
	}
	return nil
}

func (s *Store) FinishJob(id, status, outputJSON, errMsg string, finishedAt *time.Time) error {
	if _, err := s.db.Exec(`UPDATE jobs SET status = ?, output_json = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ?`,
		status, defaultJSON(outputJSON), errMsg, timePtrString(finishedAt), time.Now().UTC().Format(time.RFC3339), id,
	); err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}

func (s *Store) MarkInProgressJobsFailed() error {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin startup recovery: %w", err)
	}
	rows, err := tx.Query(`SELECT id, schedule_id, repo_id, kind FROM jobs WHERE status = 'in_progress'`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("select startup recovery jobs: %w", err)
	}
	defer rows.Close()

	type recoveryJob struct {
		id         string
		scheduleID sql.NullString
		repoID     sql.NullString
		kind       string
	}
	var jobs []recoveryJob
	for rows.Next() {
		var job recoveryJob
		if err := rows.Scan(&job.id, &job.scheduleID, &job.repoID, &job.kind); err != nil {
			tx.Rollback()
			return fmt.Errorf("scan startup recovery job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return fmt.Errorf("iterate startup recovery jobs: %w", err)
	}

	nowText := now.Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE jobs SET status = 'failed', error = 'startup-recovery', finished_at = ?, updated_at = ? WHERE status = 'in_progress'`, nowText, nowText); err != nil {
		tx.Rollback()
		return fmt.Errorf("mark startup recovery jobs: %w", err)
	}

	for _, job := range jobs {
		if _, err := tx.Exec(`INSERT INTO job_events
(job_id, schedule_id, repo_id, job_kind, event_type, status, message, payload_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			job.id, nullableString(job.scheduleID.String), nullableString(job.repoID.String), job.kind,
			"startup_recovery", "failed", "job marked failed during startup recovery", `{"reason":"startup-recovery"}`, nowText,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert startup recovery event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit startup recovery: %w", err)
	}
	return nil
}

func (s *Store) CreateJobEvent(item *domain.JobEvent) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO job_events
(job_id, schedule_id, repo_id, job_kind, event_type, status, message, payload_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.JobID, nullableString(item.ScheduleID), nullableString(item.RepoID), item.JobKind, item.EventType, item.Status, item.Message, defaultJSON(item.PayloadJSON), now,
	)
	if err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}
	item.ID, _ = result.LastInsertId()
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	return nil
}

func (s *Store) ListJobEvents(filter EventFilter) ([]domain.JobEvent, error) {
	query := `SELECT id, job_id, schedule_id, repo_id, job_kind, event_type, status, message, payload_json, created_at FROM job_events WHERE 1=1`
	var args []any
	if filter.RepoID != "" {
		query += " AND repo_id = ?"
		args = append(args, filter.RepoID)
	}
	if filter.ScheduleID != "" {
		query += " AND schedule_id = ?"
		args = append(args, filter.ScheduleID)
	}
	if filter.JobID != "" {
		query += " AND job_id = ?"
		args = append(args, filter.JobID)
	}
	if filter.JobKind != "" {
		query += " AND job_kind = ?"
		args = append(args, filter.JobKind)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.SinceID > 0 {
		query += " AND id > ?"
		args = append(args, filter.SinceID)
	}
	query += " ORDER BY id"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list job events: %w", err)
	}
	defer rows.Close()
	var items []domain.JobEvent
	for rows.Next() {
		item, err := scanJobEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) LatestSuccessfulJob(repoID, kind string) (*domain.Job, error) {
	row := s.db.QueryRow(`SELECT id, schedule_id, repo_id, kind, status, attempt_count, max_attempts, retry_backoff, retry_delay_secs, payload_json, output_json, error, started_at, finished_at, created_at, updated_at
FROM jobs WHERE repo_id = ? AND kind = ? AND status = 'completed' ORDER BY finished_at DESC, created_at DESC LIMIT 1`, repoID, kind)
	item, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest successful job: %w", err)
	}
	return item, nil
}

func (s *Store) TableExists(name string) (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, fmt.Errorf("table exists: %w", err)
	}
	return count > 0, nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanSchedule(scanner jobScanner) (*domain.Schedule, error) {
	var item domain.Schedule
	var repoID, lastRunAt, nextRunAt sql.NullString
	var enabled int
	var createdAt, updatedAt string
	if err := scanner.Scan(&item.ID, &repoID, &item.Name, &item.CronExpr, &item.JobKind, &item.PayloadJSON, &enabled, &item.MaxAttempts, &item.RetryBackoff, &item.RetryDelaySecs, &lastRunAt, &nextRunAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.RepoID = repoID.String
	item.Enabled = enabled == 1
	item.LastRunAt = parseNullTime(lastRunAt)
	item.NextRunAt = parseNullTime(nextRunAt)
	item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &item, nil
}

func scanJob(scanner jobScanner) (*domain.Job, error) {
	var item domain.Job
	var scheduleID, repoID, startedAt, finishedAt sql.NullString
	var createdAt, updatedAt string
	if err := scanner.Scan(&item.ID, &scheduleID, &repoID, &item.Kind, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.RetryBackoff, &item.RetryDelaySecs, &item.PayloadJSON, &item.OutputJSON, &item.Error, &startedAt, &finishedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.ScheduleID = scheduleID.String
	item.RepoID = repoID.String
	item.StartedAt = parseNullTime(startedAt)
	item.FinishedAt = parseNullTime(finishedAt)
	item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &item, nil
}

func scanJobEvent(scanner jobScanner) (*domain.JobEvent, error) {
	var item domain.JobEvent
	var scheduleID, repoID sql.NullString
	var createdAt string
	if err := scanner.Scan(&item.ID, &item.JobID, &scheduleID, &repoID, &item.JobKind, &item.EventType, &item.Status, &item.Message, &item.PayloadJSON, &createdAt); err != nil {
		return nil, err
	}
	item.ScheduleID = scheduleID.String
	item.RepoID = repoID.String
	item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &item, nil
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func timePtrString(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339)
}

func parseNullTime(v sql.NullString) *time.Time {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil
	}
	return &t
}

func defaultJSON(v string) string {
	if strings.TrimSpace(v) == "" {
		return "{}"
	}
	return v
}
