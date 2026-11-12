package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	libqueue "github.com/hollis-labs/libs/core/util/queue"
	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/audits/import/deepreview"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/embed"
	segit "github.com/hollis-labs/mentat/internal/git"
	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/hollis-labs/mentat/internal/schedmap"
	"github.com/hollis-labs/mentat/internal/store/db"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

const (
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

// JobType is the queue job type every Mentat job travels under.
const JobType = schedmap.JobType

type ExecRunner interface {
	CombinedOutput(ctx context.Context, name string, args []string, dir string) ([]byte, error)
}

type osExecRunner struct{}

func (osExecRunner) CombinedOutput(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// Queue is the part of the queue module the service uses to hand a job to the worker.
type Queue interface {
	Push(ctx context.Context, jobType string, payload []byte, opts ...libqueue.PushOption) error
}

type Config struct {
	Store     *sqlite.Store
	Embedder  *embed.Manager
	Exec      ExecRunner
	Queue     Queue
	Schedules ScheduleStore
	// PollInterval is how often Subscribe looks for new events (default 250ms).
	PollInterval time.Duration
	// RetryUnit is the length of one retry_delay_secs unit (default one second; tests shorten it).
	RetryUnit time.Duration
	// ScheduleZone is the IANA zone cron fields are read in (default UTC).
	ScheduleZone string
}

// Service creates jobs and runs them. Jobs are queued on the queue module and executed by its worker
// (only `serve` starts the worker); schedules live in the scheduler module's store and fire onto the same queue.
type Service struct {
	store     *sqlite.Store
	embedder  *embed.Manager
	exec      ExecRunner
	queue     Queue
	schedules ScheduleStore
	poll      time.Duration
	unit      time.Duration
	zone      string
}

type jobContextKey struct{}

func NewService(cfg Config) *Service {
	runner := cfg.Exec
	if runner == nil {
		runner = osExecRunner{}
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	unit := cfg.RetryUnit
	if unit <= 0 {
		unit = time.Second
	}
	zone := cfg.ScheduleZone
	if zone == "" {
		zone = schedmap.DefaultZone
	}
	return &Service{zone: zone, store: cfg.Store, embedder: cfg.Embedder, exec: runner, queue: cfg.Queue, schedules: cfg.Schedules, poll: poll, unit: unit}
}

type envelope = schedmap.Envelope

func (s *Service) push(ctx context.Context, env envelope, opts ...libqueue.PushOption) error {
	if s.queue == nil {
		return errors.New("jobs: no queue configured")
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return s.queue.Push(ctx, JobType, body, opts...)
}

// TriggerJob records a queued job and hands it to the queue. It returns at once; the worker in `serve` runs it.
func (s *Service) TriggerJob(ctx context.Context, kind, repoID, payloadJSON string, maxAttempts int, retryBackoff string, retryDelaySecs int) (*domain.Job, error) {
	return s.trigger(ctx, "", kind, repoID, payloadJSON, maxAttempts, retryBackoff, retryDelaySecs)
}

func (s *Service) trigger(ctx context.Context, scheduleID, kind, repoID, payloadJSON string, maxAttempts int, retryBackoff string, retryDelaySecs int) (*domain.Job, error) {
	job := &domain.Job{
		ID:             uuid.NewString(),
		ScheduleID:     scheduleID,
		RepoID:         repoID,
		Kind:           kind,
		Status:         StatusQueued,
		MaxAttempts:    maxAttempts,
		RetryBackoff:   retryBackoff,
		RetryDelaySecs: retryDelaySecs,
		PayloadJSON:    payloadJSON,
		OutputJSON:     "{}",
	}
	if err := s.store.CreateJob(job); err != nil {
		return nil, err
	}
	var queuedPayload map[string]any
	if scheduleID != "" {
		queuedPayload = map[string]any{"schedule_id": scheduleID}
	}
	s.emit(job, "queued", StatusQueued, "job queued", queuedPayload)
	if err := s.push(ctx, envelopeOf(job, 1)); err != nil {
		now := time.Now().UTC()
		_ = s.store.FinishJob(job.ID, StatusFailed, "{}", err.Error(), &now)
		s.emit(job, "failed", StatusFailed, err.Error(), nil)
		return nil, err
	}
	return job, nil
}

func envelopeOf(job *domain.Job, attempt int) envelope {
	return envelope{
		JobID: job.ID, ScheduleID: job.ScheduleID, RepoID: job.RepoID, Kind: job.Kind,
		Payload: json.RawMessage(schedmap.DefaultJSON(job.PayloadJSON)), MaxAttempts: job.MaxAttempts,
		RetryBackoff: job.RetryBackoff, RetryDelaySecs: job.RetryDelaySecs, Attempt: attempt,
	}
}

// Handle is the queue handler for JobType. It runs one attempt. A failed attempt that has attempts left is
// queued again with the job's fixed retry delay (the queue worker's own retry is not used: its delay is one value for
// the whole worker, while each job carries its own). It returns an error only when the job could not be recorded,
// which lets the queue deliver it again.
func (s *Service) Handle(ctx context.Context, qj *libqueue.QueuedJob) error {
	var env envelope
	if err := json.Unmarshal(qj.Payload, &env); err != nil {
		return nil // a payload nobody can read will not become readable by retrying
	}
	job, err := s.jobFor(&env)
	if err != nil {
		return err
	}
	if terminalStatus(job.Status) {
		return nil // delivered again after the job already ended (for example, failed by startup recovery)
	}
	attempt := max(1, env.Attempt)
	payload := map[string]any{}
	_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)

	now := time.Now().UTC()
	_ = s.store.UpdateJobAttempt(job.ID, attempt, StatusInProgress, &now)
	s.emit(job, "started", StatusInProgress, fmt.Sprintf("attempt %d started", attempt), map[string]any{"attempt": attempt})

	output, status, err := s.execute(context.WithValue(ctx, jobContextKey{}, job), job.Kind, job.RepoID, payload)
	if err == nil {
		if status == "" {
			status = StatusCompleted
		}
		done := time.Now().UTC()
		_ = s.store.FinishJob(job.ID, status, mustJSON(output), "", &done)
		s.emit(job, "completed", status, "job completed", output)
		return nil
	}

	s.emit(job, "attempt_failed", StatusInProgress, err.Error(), map[string]any{"attempt": attempt})
	if attempt < max(1, job.MaxAttempts) && ctx.Err() == nil {
		delay := retryDelay(job.RetryDelaySecs, s.unit)
		s.emit(job, "retry_scheduled", StatusInProgress, delay.String(), map[string]any{"attempt": attempt, "delay": delay.String()})
		next := envelopeOf(job, attempt+1)
		if perr := s.push(ctx, next, libqueue.WithDelay(delay)); perr == nil {
			return nil
		} else {
			err = fmt.Errorf("%w (and the retry could not be queued: %v)", err, perr)
		}
	}
	done := time.Now().UTC()
	_ = s.store.FinishJob(job.ID, StatusFailed, "{}", err.Error(), &done)
	s.emit(job, "failed", StatusFailed, err.Error(), nil)
	return nil
}

// jobFor returns the job row an envelope refers to, creating it for a scheduled fire (which has none yet).
func (s *Service) jobFor(env *envelope) (*domain.Job, error) {
	if env.JobID != "" {
		job, err := s.store.GetJob(env.JobID)
		if err != nil {
			return nil, err
		}
		if job == nil {
			return nil, fmt.Errorf("job %s not found", env.JobID)
		}
		return job, nil
	}
	job := &domain.Job{
		ID:             uuid.NewString(),
		ScheduleID:     env.ScheduleID,
		RepoID:         env.RepoID,
		Kind:           env.Kind,
		Status:         StatusQueued,
		MaxAttempts:    env.MaxAttempts,
		RetryBackoff:   env.RetryBackoff,
		RetryDelaySecs: env.RetryDelaySecs,
		PayloadJSON:    schedmap.DefaultJSON(string(env.Payload)),
		OutputJSON:     "{}",
	}
	if err := s.store.CreateJob(job); err != nil {
		return nil, err
	}
	s.emit(job, "queued", StatusQueued, "job queued", map[string]any{"schedule_id": env.ScheduleID})
	// The retry of a scheduled job continues the same row.
	env.JobID = job.ID
	return job, nil
}

func retryDelay(secs int, unit time.Duration) time.Duration {
	if secs <= 0 {
		secs = 1
	}
	return time.Duration(secs) * unit
}

// Subscribe delivers events newer than the filter's SinceID (or newer than the latest event now, when it is zero) that match
// the filter. It polls the job_events table, so it sees events written by any process.
func (s *Service) Subscribe(filter sqlite.EventFilter, buffer int) (<-chan domain.JobEvent, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan domain.JobEvent, buffer)
	done := make(chan struct{})
	var once sync.Once
	cancel := func() { once.Do(func() { close(done) }) }
	if filter.SinceID == 0 {
		if latest, err := s.store.LatestJobEventID(); err == nil {
			filter.SinceID = latest
		}
	}
	filter.Limit = buffer
	go func() {
		defer close(ch)
		tick := time.NewTicker(s.poll)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			items, err := s.store.ListJobEvents(filter)
			if err != nil {
				continue
			}
			for _, item := range items {
				select {
				case ch <- item:
					filter.SinceID = item.ID
				case <-done:
					return
				}
			}
		}
	}()
	return ch, cancel
}

func (s *Service) WaitForTerminal(ctx context.Context, jobID string) (*domain.Job, error) {
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	for {
		job, err := s.store.GetJob(jobID)
		if err != nil {
			return nil, err
		}
		if job != nil && terminalStatus(job.Status) {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

func (s *Service) ListEvents(filter sqlite.EventFilter) ([]domain.JobEvent, error) {
	return s.store.ListJobEvents(filter)
}

// emit records a job event. Subscribers pick it up from the table.
func (s *Service) emit(job *domain.Job, eventType, status, message string, payload map[string]any) {
	_ = s.store.CreateJobEvent(&domain.JobEvent{
		JobID:       job.ID,
		ScheduleID:  job.ScheduleID,
		RepoID:      job.RepoID,
		JobKind:     job.Kind,
		EventType:   eventType,
		Status:      status,
		Message:     message,
		PayloadJSON: mustJSON(payload),
	})
}

func (s *Service) execute(ctx context.Context, kind, repoID string, payload map[string]any) (map[string]any, string, error) {
	switch kind {
	case "scan":
		return s.runScan(ctx, repoID, payload)
	case "symbol-reindex":
		return s.runSymbolReindex(ctx, repoID, payload)
	case "embedding-refresh":
		return s.runEmbeddingRefresh(ctx, repoID, payload)
	case "audit-refresh":
		return s.runAuditRefresh(ctx, repoID, payload)
	case "git-history-sweep":
		return s.runGitHistorySweep(ctx, repoID, payload)
	case "complexity-metrics":
		return s.runComplexityMetrics(ctx, repoID, payload)
	default:
		return nil, "", fmt.Errorf("unknown job kind: %s", kind)
	}
}

func mustJSON(v any) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func terminalStatus(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusSkipped:
		return true
	default:
		return false
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func repoPath(repo *domain.Repo, payload map[string]any) (string, error) {
	if path, ok := payload["repo_path"].(string); ok && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if repo.LocalPath != "" {
		return repo.LocalPath, nil
	}
	return "", fmt.Errorf("repo path unknown for %s", repo.ID)
}

func gitHead(ctx context.Context, dir string) string {
	out, err := osExecRunner{}.CombinedOutput(ctx, "git", []string{"rev-parse", "HEAD"}, dir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func boolPayload(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

func stringPayload(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (s *Service) runScan(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	repo, err := s.store.GetRepo(repoID)
	if err != nil {
		return nil, "", err
	}
	if repo == nil {
		return nil, "", fmt.Errorf("repo not found: %s", repoID)
	}
	path, err := repoPath(repo, payload)
	if err != nil {
		return nil, "", err
	}
	blueprint := stringPayload(payload, "blueprint")
	if blueprint == "" {
		blueprint = "blueprints/se-repo-scan.yaml"
	}
	if !filepath.IsAbs(blueprint) {
		cwd, _ := os.Getwd()
		blueprint = filepath.Join(cwd, blueprint)
	}
	args := []string{"run", blueprint, "--input", "repo_path=" + path, "--input", "repo_id=" + repo.ID}
	out, err := s.exec.CombinedOutput(ctx, "hadron", args, path)
	result := map[string]any{
		"repo_id":    repo.ID,
		"repo_path":  path,
		"blueprint":  blueprint,
		"command":    append([]string{"hadron"}, args...),
		"output":     string(out),
		"exit_error": "",
	}
	if err != nil {
		result["exit_error"] = err.Error()
		return result, "", fmt.Errorf("hadron run failed: %w", err)
	}
	return result, StatusCompleted, nil
}

func (s *Service) runSymbolReindex(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	repo, err := s.store.GetRepo(repoID)
	if err != nil {
		return nil, "", err
	}
	if repo == nil {
		return nil, "", fmt.Errorf("repo not found: %s", repoID)
	}
	path, err := repoPath(repo, payload)
	if err != nil {
		return nil, "", err
	}
	head := gitHead(ctx, path)
	latest, err := s.store.LatestSuccessfulJob(repoID, "symbol-reindex")
	if err != nil {
		return nil, "", err
	}
	if latest != nil && !boolPayload(payload, "force") {
		var prev struct {
			Head string `json:"head"`
		}
		_ = json.Unmarshal([]byte(latest.OutputJSON), &prev)
		if prev.Head != "" && prev.Head == head {
			return map[string]any{"repo_id": repoID, "head": head, "skipped": true}, StatusSkipped, nil
		}
	}
	ingester := symbols.NewIngester(symbols.Config{WorkDir: path, Store: s.store})
	result, err := ingester.Ingest(ctx, symbols.IngestRequest{RepoID: repoID, RepoPath: path, CommitRef: head})
	if err != nil {
		return nil, "", err
	}
	return map[string]any{
		"repo_id":   repoID,
		"repo_path": path,
		"head":      head,
		"inserted":  result.Inserted,
		"updated":   result.Updated,
		"drifted":   result.Drifted,
	}, StatusCompleted, nil
}

func (s *Service) runEmbeddingRefresh(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	report, err := retrieval.RefreshRepoEmbeddings(ctx, s.store, s.embedder, repoID, boolPayload(payload, "force"))
	if err != nil {
		return nil, "", err
	}
	return map[string]any{
		"repo_id":           report.RepoID,
		"profile":           report.Profile,
		"model":             report.Model,
		"symbols_embedded":  report.SymbolsEmbedded,
		"findings_embedded": report.FindingsEmbedded,
		"skipped":           report.Skipped,
	}, StatusCompleted, nil
}

func (s *Service) runAuditRefresh(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	root := stringPayload(payload, "audit_path")
	if root == "" {
		return nil, "", fmt.Errorf("audit_path is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("audit_path is not a directory: %s", root)
	}
	store := audits.NewStore(s.store.DB())
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, "", err
	}
	imported := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "index.md")); err != nil {
			continue
		}
		bundle, err := deepreview.ParseDir(dir, repoID, audits.Provenance{
			ActorKind: "scheduler",
			ActorID:   "stack-explorer",
			SessionID: uuid.NewString(),
			ToolName:  "audit-refresh",
		})
		if err != nil {
			return nil, "", err
		}
		if _, err := store.ReplaceImportedAudit(*bundle); err != nil {
			return nil, "", err
		}
		imported++
	}
	return map[string]any{"repo_id": repoID, "audit_path": root, "imported": imported}, StatusCompleted, nil
}

func (s *Service) runGitHistorySweep(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	ok, err := s.store.TableExists("relationships")
	if err != nil {
		return nil, "", err
	}
	repo, err := s.store.GetRepo(repoID)
	if err != nil {
		return nil, "", err
	}
	if repo == nil {
		return nil, "", fmt.Errorf("repo not found: %s", repoID)
	}
	path, err := repoPath(repo, payload)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return map[string]any{"repo_id": repoID, "repo_path": path, "noop": true, "reason": "relationships table not present"}, StatusSkipped, nil
	}
	out, err := s.exec.CombinedOutput(ctx, "git", []string{"log", "--name-only", "--pretty=format:%H", "--since=90 days ago"}, path)
	if err != nil {
		return map[string]any{"repo_id": repoID, "repo_path": path, "output": string(out)}, "", fmt.Errorf("git log failed: %w", err)
	}
	changeSets := segit.ParseNameOnlyLog(string(out))
	pairs := segit.CoChangeWeights(changeSets)
	job, _ := ctx.Value(jobContextKey{}).(*domain.Job)
	if job != nil {
		s.emit(job, "progress", StatusInProgress, "parsed git history", map[string]any{
			"change_sets": len(changeSets),
			"file_pairs":  len(pairs),
		})
	}

	relationshipCount := 0
	symbolPairs := 0
	discoveredAt := time.Now().UTC()
	processedPairs := 0
	// Progress is collected while the transaction runs and emitted after it commits: an event is a write on the
	// store's single write connection, which the open transaction holds, so emitting inside it waits for itself.
	type progress struct {
		message string
		payload map[string]any
	}
	var pending []progress
	// The symbol lookups read committed data from the reader pool; only relationships are written,
	// in one write transaction, so a failed sweep leaves the previous relationships in place.
	err = s.store.DB().WriteTx(func(tx *db.Tx) error {
		if _, err := tx.Exec(`DELETE FROM relationships WHERE repo_id = ? AND source = ?`, repoID, "git-history"); err != nil {
			return fmt.Errorf("delete relationships by repo/source: %w", err)
		}
		for idx, pair := range pairs {
			leftSymbols, err := s.store.ListSymbolsByFile(repoID, pair.Left)
			if err != nil {
				return err
			}
			rightSymbols, err := s.store.ListSymbolsByFile(repoID, pair.Right)
			if err != nil {
				return err
			}
			if len(leftSymbols) == 0 || len(rightSymbols) == 0 {
				continue
			}
			leftSymbols = coChangeEligibleSymbols(leftSymbols)
			rightSymbols = coChangeEligibleSymbols(rightSymbols)
			if len(leftSymbols) == 0 || len(rightSymbols) == 0 {
				continue
			}
			processedPairs++
			symbolPairs += len(leftSymbols) * len(rightSymbols)
			for _, left := range leftSymbols {
				for _, right := range rightSymbols {
					if left.ID == right.ID {
						continue
					}
					if _, err := tx.Exec(`INSERT INTO relationships (
repo_id, src_symbol_id, dst_symbol_id, kind, weight, source, discovered_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(src_symbol_id, dst_symbol_id, kind, source) DO UPDATE SET
weight = excluded.weight,
discovered_at = excluded.discovered_at`,
						repoID, left.ID, right.ID, "co-changed-with", pair.Weight, "git-history", discoveredAt.Format(time.RFC3339)); err != nil {
						return fmt.Errorf("upsert git history relationship: %w", err)
					}
					relationshipCount++
				}
			}
			if job != nil && processedPairs%250 == 0 {
				pending = append(pending, progress{fmt.Sprintf("processed %d/%d file pairs", idx+1, len(pairs)), map[string]any{
					"processed_pairs":     processedPairs,
					"total_pairs":         len(pairs),
					"relationships_added": relationshipCount,
				}})
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("git history sweep: %w", err)
	}
	for _, p := range pending {
		s.emit(job, "progress", StatusInProgress, p.message, p.payload)
	}

	return map[string]any{
		"repo_id":             repoID,
		"repo_path":           path,
		"change_sets":         len(changeSets),
		"file_pairs":          len(pairs),
		"symbol_pairs":        symbolPairs,
		"relationships_added": relationshipCount,
	}, StatusCompleted, nil
}

func coChangeEligibleSymbols(items []model.Symbol) []model.Symbol {
	out := make([]model.Symbol, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case "function", "method", "type", "module", "package":
			out = append(out, item)
		}
	}
	return out
}

func (s *Service) runComplexityMetrics(ctx context.Context, repoID string, payload map[string]any) (map[string]any, string, error) {
	repo, err := s.store.GetRepo(repoID)
	if err != nil {
		return nil, "", err
	}
	if repo == nil {
		return nil, "", fmt.Errorf("repo not found: %s", repoID)
	}
	path, err := repoPath(repo, payload)
	if err != nil {
		return nil, "", err
	}
	out, err := s.exec.CombinedOutput(ctx, "lizard", []string{path}, path)
	if err != nil {
		return map[string]any{"repo_id": repoID, "repo_path": path, "output": string(out)}, "", fmt.Errorf("lizard failed: %w", err)
	}
	snap := &domain.Snapshot{RepoID: repoID, RawJSON: string(out)}
	if err := s.store.CreateSnapshot(snap); err != nil {
		return nil, "", err
	}
	return map[string]any{"repo_id": repoID, "repo_path": path, "snapshot_id": snap.ID, "output": string(out)}, StatusCompleted, nil
}
