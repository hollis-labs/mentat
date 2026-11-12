// Package schedmap maps a Mentat schedule onto the scheduler module's record (libs core/util/scheduler) and back. The
// scheduler record has no field for a name, a repo or the retry settings, so they travel in its payload under named keys; nothing
// the pre-Substrate schedules table held is lost.
package schedmap

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	libsched "github.com/hollis-labs/libs/core/util/scheduler"
	"github.com/hollis-labs/mentat/internal/domain"
)

// JobType is the queue job type every Mentat job travels under; the payload (an Envelope) says which kind of work it is.
const JobType = "mentat.job"

// DefaultZone is the zone cron fields are read in when none is configured. The pre-Substrate engine read them in the
// server's local time; the scheduler module needs a named zone.
const DefaultZone = "UTC"

// Envelope is the queue payload of every job: what to run, and the retry settings the job row carries.
// A stored schedule keeps the same envelope (plus Schedule) as its scheduler payload, so a fire hands it on unchanged.
type Envelope struct {
	JobID          string          `json:"job_id,omitempty"`
	ScheduleID     string          `json:"schedule_id,omitempty"`
	RepoID         string          `json:"repo_id,omitempty"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	MaxAttempts    int             `json:"max_attempts"`
	RetryBackoff   string          `json:"retry_backoff,omitempty"`
	RetryDelaySecs int             `json:"retry_delay_secs"`
	Attempt        int             `json:"attempt,omitempty"`
	Schedule       *Meta           `json:"schedule,omitempty"`
}

// Meta is what a Mentat schedule has that a scheduler record has no field for.
type Meta struct {
	Name           string     `json:"name"`
	CronExpr       string     `json:"cron_expr"` // as the user wrote it (the scheduler record holds the 5-field form)
	PayloadJSON    string     `json:"payload_json"`
	MaxAttempts    int        `json:"max_attempts"`
	RetryBackoff   string     `json:"retry_backoff"` // recorded; only a fixed delay is acted on
	RetryDelaySecs int        `json:"retry_delay_secs"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LegacyNextRun  *time.Time `json:"legacy_next_run_at,omitempty"` // next_run_at as the pre-Substrate database held it
}

// DefaultJSON is "{}" for an empty payload.
func DefaultJSON(v string) string {
	if strings.TrimSpace(v) == "" {
		return "{}"
	}
	return v
}

// StandardCron turns the expression a user gives into the 5-field form the scheduler module parses. The old
// engine read a leading seconds field; a field of 0 is the same schedule, any other value cannot be expressed.
func StandardCron(expr string) (string, error) {
	fields := strings.Fields(expr)
	switch len(fields) {
	case 5:
		return strings.Join(fields, " "), nil
	case 6:
		if fields[0] != "0" {
			return "", fmt.Errorf("invalid cron expression %q: the scheduler runs at whole minutes, so the seconds field must be 0", expr)
		}
		return strings.Join(fields[1:], " "), nil
	}
	return "", fmt.Errorf("invalid cron expression %q: want 5 fields (or 6 with seconds 0)", expr)
}

// ToRecord builds the scheduler-module record for item. Defaults are applied to item (a missing max_attempts is 1, a
// missing retry delay 1, a missing backoff "fixed"). When item.CreatedAt is set the stored times are kept as given;
// otherwise now is used. NextRun is the first occurrence after now.
func ToRecord(item *domain.Schedule, now time.Time, zone string, legacyNext *time.Time) (libsched.Schedule, error) {
	if zone == "" {
		zone = DefaultZone
	}
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = 1
	}
	if item.RetryDelaySecs <= 0 {
		item.RetryDelaySecs = 1
	}
	if strings.TrimSpace(item.RetryBackoff) == "" {
		item.RetryBackoff = "fixed"
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now.UTC()
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = item.CreatedAt
	}
	std, err := StandardCron(item.CronExpr)
	if err != nil {
		return libsched.Schedule{}, err
	}
	env := Envelope{
		ScheduleID: item.ID, RepoID: item.RepoID, Kind: item.JobKind,
		Payload:     json.RawMessage(DefaultJSON(item.PayloadJSON)),
		MaxAttempts: item.MaxAttempts, RetryBackoff: item.RetryBackoff, RetryDelaySecs: item.RetryDelaySecs,
		Schedule: &Meta{
			Name: item.Name, CronExpr: item.CronExpr, PayloadJSON: DefaultJSON(item.PayloadJSON),
			MaxAttempts: item.MaxAttempts, RetryBackoff: item.RetryBackoff, RetryDelaySecs: item.RetryDelaySecs,
			CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(), LegacyNextRun: legacyNext,
		},
	}
	body, err := json.Marshal(env)
	if err != nil {
		return libsched.Schedule{}, err
	}
	sch := libsched.Schedule{
		ID: item.ID, CronExpr: std, Enabled: item.Enabled, JobType: JobType, Payload: body, Location: zone,
		// The old engine never fired a tick it missed while it was down; do the same.
		Misfire: libsched.MisfireSkip,
	}
	if item.LastRunAt != nil {
		sch.LastRun = item.LastRunAt.UTC()
	}
	if sch.NextRun, err = libsched.NextRunForSchedule(sch, now.UTC()); err != nil {
		return libsched.Schedule{}, err
	}
	return sch, nil
}

// FromRecord is the reverse of ToRecord: the shape the CLI has always returned.
func FromRecord(sch libsched.Schedule) (domain.Schedule, error) {
	var env Envelope
	if err := json.Unmarshal(sch.Payload, &env); err != nil || env.Schedule == nil {
		return domain.Schedule{}, fmt.Errorf("schedule %s was not created by mentat", sch.ID)
	}
	m := env.Schedule
	out := domain.Schedule{
		ID: sch.ID, RepoID: env.RepoID, Name: m.Name, CronExpr: m.CronExpr, JobKind: env.Kind,
		PayloadJSON: m.PayloadJSON, Enabled: sch.Enabled, MaxAttempts: m.MaxAttempts,
		RetryBackoff: m.RetryBackoff, RetryDelaySecs: m.RetryDelaySecs,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if !sch.LastRun.IsZero() {
		t := sch.LastRun.UTC()
		out.LastRunAt = &t
	}
	if !sch.NextRun.IsZero() {
		t := sch.NextRun.UTC()
		out.NextRunAt = &t
	}
	return out, nil
}

// LegacyNextRun returns the next_run_at a migrated schedule kept in its payload, or nil.
func LegacyNextRun(payload []byte) *time.Time {
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil || env.Schedule == nil {
		return nil
	}
	return env.Schedule.LegacyNextRun
}
