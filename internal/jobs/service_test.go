package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	libqueue "github.com/hollis-labs/libs/core/util/queue"
	"github.com/hollis-labs/libs/core/util/queue/driver/memory"
	libsched "github.com/hollis-labs/libs/core/util/scheduler"
	"github.com/hollis-labs/libs/core/util/scheduler/sqlstore"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

type fakeExec struct {
	output []byte
	err    error
	calls  int
}

func (f *fakeExec) CombinedOutput(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
	f.calls++
	return f.output, f.err
}

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

func TestStartupRecoveryMarksInProgressFailed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	job := &domain.Job{ID: "job-1", Kind: "scan", Status: StatusInProgress, PayloadJSON: "{}"}
	if err := store.CreateJob(job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	store, err = sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// serve runs the recovery at start; opening a database no longer does.
	if err := store.MarkInProgressJobsFailed(); err != nil {
		t.Fatalf("startup recovery: %v", err)
	}
	got, err := store.GetJob("job-1")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != StatusFailed || got.Error != "startup-recovery" {
		t.Fatalf("unexpected recovery job state: %+v", got)
	}
	events, err := store.ListJobEvents(sqlite.EventFilter{JobID: "job-1"})
	if err != nil {
		t.Fatalf("list job events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].EventType != "startup_recovery" || events[0].Status != StatusFailed {
		t.Fatalf("unexpected recovery event: %+v", events[0])
	}
}

// harness wires a service the way `serve` does: jobs go onto a queue, a worker runs them, schedules live in the
// scheduler module's reference store. The queue is the library's in-memory driver.
type harness struct {
	svc   *Service
	store *sqlite.Store
	sched *sqlstore.Store
}

func newHarness(t *testing.T, runner ExecRunner) *harness {
	t.Helper()
	store := openTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	if err := sqlstore.Migrate(context.Background(), store.SQL()); err != nil {
		t.Fatalf("migrate scheduler store: %v", err)
	}
	sched, err := sqlstore.New(store.SQL())
	if err != nil {
		t.Fatalf("scheduler store: %v", err)
	}
	q := memory.New()
	svc := NewService(Config{Store: store, Exec: runner, Queue: q, Schedules: sched, PollInterval: 10 * time.Millisecond, RetryUnit: 20 * time.Millisecond})
	w := libqueue.NewWorker(q, libqueue.WorkerOpts{PollInterval: 5 * time.Millisecond, MaxTries: 3})
	w.Register(JobType, svc.Handle)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Start(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return &harness{svc: svc, store: store, sched: sched}
}

func (h *harness) wait(t *testing.T, jobID string) *domain.Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job, err := h.svc.WaitForTerminal(ctx, jobID)
	if err != nil {
		t.Fatalf("wait for job %s: %v", jobID, err)
	}
	return job
}

func TestScheduleTriggerRunsTheJobAndEmitsEvents(t *testing.T) {
	exec := &fakeExec{output: []byte("ok")}
	h := newHarness(t, exec)
	repoPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := h.store.CreateRepo(&domain.Repo{ID: "repo1", Name: "repo1", LocalPath: repoPath}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	schedule := &domain.Schedule{
		ID: "sched-1", RepoID: "repo1", Name: "nightly", CronExpr: "0 0 * * * *", JobKind: "scan",
		PayloadJSON: `{"blueprint":"blueprints/se-repo-scan.yaml","repo_path":"` + repoPath + `"}`,
	}
	if err := h.svc.AddSchedule(schedule); err != nil {
		t.Fatalf("add schedule: %v", err)
	}

	ch, stop := h.svc.Subscribe(sqlite.EventFilter{ScheduleID: "sched-1"}, 16)
	defer stop()
	job, err := h.svc.TriggerSchedule(context.Background(), "sched-1")
	if err != nil {
		t.Fatalf("trigger schedule: %v", err)
	}
	got := h.wait(t, job.ID)
	if got.ScheduleID != "sched-1" || got.Status != StatusCompleted {
		t.Fatalf("job = %+v, want completed under sched-1", got)
	}
	var types []string
	timeout := time.After(5 * time.Second)
	for len(types) < 3 {
		select {
		case ev := <-ch:
			types = append(types, ev.EventType)
		case <-timeout:
			t.Fatalf("events seen: %v", types)
		}
	}
	if types[0] != "queued" || types[len(types)-1] != "completed" {
		t.Fatalf("event order = %v", types)
	}
	if exec.calls == 0 {
		t.Fatalf("expected fake exec to run")
	}
}

// A job with attempts left is queued again after its fixed delay, and ends failed with the last error once they run out.
func TestRetryUsesTheFixedDelayAndStopsAtMaxAttempts(t *testing.T) {
	exec := &fakeExec{err: errors.New("boom")}
	h := newHarness(t, exec)
	repoPath := t.TempDir()
	if err := h.store.CreateRepo(&domain.Repo{ID: "repo1", Name: "repo1", LocalPath: repoPath}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	job, err := h.svc.TriggerJob(context.Background(), "git-history-sweep", "repo1", "{}", 3, "exponential", 1)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	got := h.wait(t, job.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "boom") {
		t.Fatalf("job = %+v, want failed with boom", got)
	}
	if exec.calls != 3 {
		t.Fatalf("attempts = %d, want 3", exec.calls)
	}
	events, err := h.store.ListJobEvents(sqlite.EventFilter{JobID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.EventType == "retry_scheduled" && !strings.Contains(ev.Message, "20ms") {
			t.Fatalf("retry delay is not the fixed one (exponential must not grow): %q", ev.Message)
		}
	}
}

// The production sweep fails most runs (a repo whose path is gone). A failed job must end failed with a clear
// error, the worker must stay alive, and the next job must run.
func TestSweepWithAMissingPathFailsClearlyAndTheWorkerSurvives(t *testing.T) {
	h := newHarness(t, nil) // the real runner: a missing directory is what git would hit
	gone := filepath.Join(t.TempDir(), "removed-checkout")
	if err := h.store.CreateRepo(&domain.Repo{ID: "gone", Name: "gone", LocalPath: gone}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	other := t.TempDir()
	if err := h.store.CreateRepo(&domain.Repo{ID: "other", Name: "other", LocalPath: other}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	first, err := h.svc.TriggerJob(context.Background(), "git-history-sweep", "gone", "{}", 1, "fixed", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(t, first.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "git log failed") || !strings.Contains(got.Error, "removed-checkout") {
		t.Fatalf("first job = status %q error %q, want failed naming the missing path", got.Status, got.Error)
	}
	second, err := h.svc.TriggerJob(context.Background(), "git-history-sweep", "other", "{}", 1, "fixed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.wait(t, second.ID); got.Status != StatusFailed && got.Status != StatusCompleted {
		t.Fatalf("second job did not finish: %+v", got)
	}
}

// A job delivered again after it already ended (for example failed by startup recovery) is dropped, not run twice.
func TestRedeliveredFinishedJobIsNotRunAgain(t *testing.T) {
	exec := &fakeExec{output: []byte("ok")}
	h := newHarness(t, exec)
	job := &domain.Job{ID: "done-1", Kind: "git-history-sweep", Status: StatusFailed, PayloadJSON: "{}"}
	if err := h.store.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(envelope{JobID: "done-1", Kind: "git-history-sweep", Attempt: 1})
	if err := h.svc.Handle(context.Background(), &libqueue.QueuedJob{Payload: body}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if exec.calls != 0 {
		t.Fatalf("ran a finished job again")
	}
}

func TestSubscribeSeesEventsWrittenElsewhere(t *testing.T) {
	h := newHarness(t, &fakeExec{})
	ch, stop := h.svc.Subscribe(sqlite.EventFilter{}, 4)
	defer stop()
	job := &domain.Job{ID: "ev-1", Kind: "scan", Status: StatusQueued, PayloadJSON: "{}"}
	if err := h.store.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	h.svc.emit(job, "queued", StatusQueued, "queued", nil)
	select {
	case ev := <-ch:
		if ev.JobID != "ev-1" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event delivered")
	}
}

func TestRunGitHistorySweepCreatesRelationships(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	repoPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := store.CreateRepo(&domain.Repo{ID: "repo1", Name: "repo1", LocalPath: repoPath}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	lineStart := 1
	lineEnd := 1
	left := &model.Symbol{
		RepoID:        "repo1",
		Kind:          "function",
		Name:          "Left",
		QualifiedName: "example.Left",
		FilePath:      "left.go",
		LineStart:     &lineStart,
		LineEnd:       &lineEnd,
		ContentHash:   "left-content",
		SignatureHash: "left-sig",
		Language:      "go",
	}
	right := &model.Symbol{
		RepoID:        "repo1",
		Kind:          "function",
		Name:          "Right",
		QualifiedName: "example.Right",
		FilePath:      "right.go",
		LineStart:     &lineStart,
		LineEnd:       &lineEnd,
		ContentHash:   "right-content",
		SignatureHash: "right-sig",
		Language:      "go",
	}
	if err := store.UpsertSymbol(left); err != nil {
		t.Fatalf("upsert left symbol: %v", err)
	}
	if err := store.UpsertSymbol(right); err != nil {
		t.Fatalf("upsert right symbol: %v", err)
	}

	exec := &fakeExec{output: []byte("aaa111\nleft.go\nright.go\n")}
	svc := NewService(Config{Store: store, Exec: exec})
	got, status, err := svc.runGitHistorySweep(context.Background(), "repo1", map[string]any{"repo_path": repoPath})
	if err != nil {
		t.Fatalf("run git history sweep: %v", err)
	}
	if status != StatusCompleted {
		t.Fatalf("status = %s, want %s", status, StatusCompleted)
	}
	if got["relationships_added"].(int) != 1 {
		t.Fatalf("relationships_added = %#v, want 1", got["relationships_added"])
	}

	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM relationships WHERE repo_id = ? AND kind = 'co-changed-with' AND source = 'git-history'`, "repo1").Scan(&count); err != nil {
		t.Fatalf("count relationships: %v", err)
	}
	if count != 1 {
		t.Fatalf("relationship count = %d, want 1", count)
	}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }
func (c fixedClock) NewTicker(time.Duration) libsched.Ticker {
	return idleTicker{}
}

type idleTicker struct{}

func (idleTicker) C() <-chan time.Time { return nil }
func (idleTicker) Stop()               {}

type countingRunner struct {
	jobs []libsched.Job
}

func (r *countingRunner) Enqueue(_ context.Context, job libsched.Job) error {
	r.jobs = append(r.jobs, job)
	return nil
}

// After downtime the old engine fired nothing for the ticks it missed. The scheduler must do the same: a
// schedule whose next run is hours in the past is skipped to its next future occurrence, and a tick on time fires once.
func TestTicksMissedWhileDownAreNotFired(t *testing.T) {
	h := newHarness(t, &fakeExec{})
	item := &domain.Schedule{ID: "sweep", Name: "git-history-sweep", CronExpr: "0 */5 * * * *", JobKind: "git-history-sweep", Enabled: true}
	if err := h.svc.AddSchedule(item); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := h.sched.GetSchedule(context.Background(), "sweep")
	if err != nil || !ok {
		t.Fatalf("get schedule: %v %v", ok, err)
	}
	// The process was down: three hours later the stored next run is long past.
	later := rec.NextRun.Add(3 * time.Hour).Add(90 * time.Second)
	runner := &countingRunner{}
	eng := libsched.New(h.sched, runner, libsched.WithClock(fixedClock{now: later}))
	if err := eng.TickNow(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(runner.jobs) != 0 {
		t.Fatalf("fired %d missed ticks, want none", len(runner.jobs))
	}
	rec, _, _ = h.sched.GetSchedule(context.Background(), "sweep")
	if !rec.NextRun.After(later) || rec.NextRun.Sub(later) > 5*time.Minute {
		t.Fatalf("next run %s is not the next occurrence after %s", rec.NextRun, later)
	}
	// On time: the next occurrence fires exactly once, carrying the job's envelope.
	on := rec.NextRun.Add(2 * time.Second)
	eng = libsched.New(h.sched, runner, libsched.WithClock(fixedClock{now: on}))
	if err := eng.TickNow(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if err := eng.TickNow(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(runner.jobs) != 1 || runner.jobs[0].JobType != JobType {
		t.Fatalf("fires = %+v, want exactly one %s", runner.jobs, JobType)
	}
	var env envelope
	if err := json.Unmarshal(runner.jobs[0].Payload, &env); err != nil || env.Kind != "git-history-sweep" || env.ScheduleID != "sweep" {
		t.Fatalf("fire payload = %s (%v)", runner.jobs[0].Payload, err)
	}
}
