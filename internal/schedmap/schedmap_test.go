package schedmap

import (
	"encoding/json"
	"testing"
	"time"

	libsched "github.com/hollis-labs/libs/core/util/scheduler"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/robfig/cron/v3"
)

// The pre-Substrate engine parsed six fields (seconds first) with robfig/cron; the scheduler module parses five.
// For every expression an old schedule can hold (seconds 0), the next fire times must be the same.
func TestNextFireTimesMatchTheOldEngine(t *testing.T) {
	old := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	clocks := []time.Time{
		time.Date(2026, 11, 12, 13, 32, 7, 0, time.UTC),
		time.Date(2026, 3, 29, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 55, 0, 0, time.UTC),
	}
	for _, expr := range []string{"0 */5 * * * *", "0 30 2 * * 1-5", "0 0 */6 * * *", "0 15 4 1 * *", "0 0 0 * * 0"} {
		oldSched, err := old.Parse(expr)
		if err != nil {
			t.Fatalf("old parse %q: %v", expr, err)
		}
		item := &domain.Schedule{ID: "s", Name: "n", CronExpr: expr, JobKind: "scan", Enabled: true}
		for _, clock := range clocks {
			rec, err := ToRecord(item, clock, "UTC", nil)
			if err != nil {
				t.Fatalf("%q: %v", expr, err)
			}
			from := clock
			for i := 0; i < 5; i++ {
				want := oldSched.Next(from).UTC()
				got, err := libsched.NextRunForSchedule(rec, from)
				if err != nil {
					t.Fatal(err)
				}
				if !got.Equal(want) {
					t.Fatalf("%q from %s, fire %d: scheduler %s, old engine %s", expr, clock, i+1, got, want)
				}
				from = want
			}
		}
	}
}

func TestStandardCron(t *testing.T) {
	for in, want := range map[string]string{"0 */5 * * * *": "*/5 * * * *", "*/5 * * * *": "*/5 * * * *", " 0  0 3 * * * ": "0 3 * * *"} {
		got, err := StandardCron(in)
		if err != nil || got != want {
			t.Errorf("StandardCron(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"*/10 * * * * *", "", "* * *", "@every 5m"} {
		if got, err := StandardCron(in); err == nil {
			t.Errorf("StandardCron(%q) = %q, want an error", in, got)
		}
	}
}

// Every field of a schedule survives the trip through the scheduler record, each with a distinctive value.
func TestEveryFieldRoundTrips(t *testing.T) {
	last := time.Date(2026, 5, 14, 9, 21, 33, 0, time.UTC)
	legacyNext := time.Date(2026, 5, 14, 9, 25, 0, 0, time.UTC)
	created := time.Date(2026, 4, 2, 11, 12, 13, 0, time.UTC)
	updated := time.Date(2026, 5, 14, 9, 21, 34, 0, time.UTC)
	in := domain.Schedule{
		ID: "sched-7c1f", RepoID: "repo-distinct", Name: "weekly distinct name", CronExpr: "0 17 3 * * 2",
		JobKind: "symbol-reindex", PayloadJSON: `{"repo_path":"/x/y","force":true}`, Enabled: true,
		MaxAttempts: 4, RetryBackoff: "exponential", RetryDelaySecs: 9, LastRunAt: &last, CreatedAt: created, UpdatedAt: updated,
	}
	rec, err := ToRecord(&in, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), "UTC", &legacyNext)
	if err != nil {
		t.Fatal(err)
	}
	out, err := FromRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != in.ID || out.RepoID != in.RepoID || out.Name != in.Name || out.CronExpr != in.CronExpr ||
		out.JobKind != in.JobKind || out.PayloadJSON != in.PayloadJSON || out.Enabled != in.Enabled ||
		out.MaxAttempts != in.MaxAttempts || out.RetryBackoff != in.RetryBackoff || out.RetryDelaySecs != in.RetryDelaySecs ||
		!out.CreatedAt.Equal(created) || !out.UpdatedAt.Equal(updated) || out.LastRunAt == nil || !out.LastRunAt.Equal(last) {
		t.Fatalf("round trip lost a field:\n in  %+v\n out %+v", in, out)
	}
	if out.NextRunAt == nil || !out.NextRunAt.After(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("next run must be recomputed after the migration clock: %v", out.NextRunAt)
	}
	// The original next_run_at is kept in the payload, recoverable.
	var env Envelope
	if err := jsonUnmarshal(rec.Payload, &env); err != nil || env.Schedule.LegacyNextRun == nil || !env.Schedule.LegacyNextRun.Equal(legacyNext) {
		t.Fatalf("legacy next_run_at not kept: %v %+v", err, env.Schedule)
	}
	// Retry settings stay on the envelope a fire hands to the worker.
	if env.MaxAttempts != 4 || env.RetryDelaySecs != 9 || env.RetryBackoff != "exponential" || env.Kind != "symbol-reindex" || env.RepoID != "repo-distinct" {
		t.Fatalf("fire envelope = %+v", env)
	}
	if rec.Misfire != libsched.MisfireSkip || rec.Location != "UTC" || rec.JobType != JobType {
		t.Fatalf("record policy = misfire %q location %q type %q", rec.Misfire, rec.Location, rec.JobType)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
