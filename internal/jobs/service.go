package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/audits/import/deepreview"
	"github.com/hollis-labs/mentat/internal/domain"
	segit "github.com/hollis-labs/mentat/internal/git"
	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols"
	"github.com/hollis-labs/mentat/internal/symbols/model"
	"github.com/robfig/cron/v3"
)

const (
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusSkipped    = "skipped"
)

type ExecRunner interface {
	CombinedOutput(ctx context.Context, name string, args []string, dir string) ([]byte, error)
}

type osExecRunner struct{}

func (osExecRunner) CombinedOutput(ctx context.Context, name string, args []string, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

type subscriber struct {
	filter sqlite.EventFilter
	ch     chan domain.JobEvent
}

type Config struct {
	Store   *sqlite.Store
	Workers int
	Exec    ExecRunner
}

type Service struct {
	store   *sqlite.Store
	exec    ExecRunner
	workers int

	cron *cron.Cron

	mu          sync.Mutex
	started     bool
	scheduleIDs map[string]cron.EntryID
	subscribers map[int]subscriber
	nextSubID   int
	queue       chan *domain.Job
	wg          sync.WaitGroup
}

type jobContextKey struct{}

var cronParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func NewService(cfg Config) *Service {
	workers := cfg.Workers
	if workers <= 0 {
		workers = 2
	}
	runner := cfg.Exec
	if runner == nil {
		runner = osExecRunner{}
	}
	return &Service{
		store:       cfg.Store,
		exec:        runner,
		workers:     workers,
		cron:        cron.New(cron.WithSeconds()),
		scheduleIDs: make(map[string]cron.EntryID),
		subscribers: make(map[int]subscriber),
		queue:       make(chan *domain.Job, 128),
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	if err := s.hydrateSchedules(); err != nil {
		return err
	}
	s.cron.Start()
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	s.mu.Unlock()
	close(s.queue)
	cronCtx := s.cron.Stop()
	<-cronCtx.Done()
	s.wg.Wait()
	s.mu.Lock()
	for id, sub := range s.subscribers {
		delete(s.subscribers, id)
		close(sub.ch)
	}
	s.mu.Unlock()
}

func (s *Service) Subscribe(filter sqlite.EventFilter, buffer int) (<-chan domain.JobEvent, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan domain.JobEvent, buffer)
	s.mu.Lock()
	id := s.nextSubID
	s.nextSubID++
	s.subscribers[id] = subscriber{filter: filter, ch: ch}
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if sub, ok := s.subscribers[id]; ok {
			delete(s.subscribers, id)
			close(sub.ch)
		}
		s.mu.Unlock()
	}
	return ch, cancel
}

func (s *Service) AddSchedule(item *domain.Schedule) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if err := validateCron(item.CronExpr); err != nil {
		return err
	}
	nextRun, err := nextRun(item.CronExpr, time.Now().UTC())
	if err != nil {
		return err
	}
	item.Enabled = true
	item.NextRunAt = &nextRun
	if err := s.store.CreateSchedule(item); err != nil {
		return err
	}
	return s.registerSchedule(item)
}

func (s *Service) RemoveSchedule(id string) error {
	s.mu.Lock()
	entryID, ok := s.scheduleIDs[id]
	if ok {
		delete(s.scheduleIDs, id)
		s.cron.Remove(entryID)
	}
	s.mu.Unlock()
	return s.store.DeleteSchedule(id)
}

func (s *Service) ListSchedules(repoID string) ([]domain.Schedule, error) {
	return s.store.ListSchedules(repoID, false)
}

func (s *Service) TriggerSchedule(ctx context.Context, scheduleID string) (*domain.Job, error) {
	schedule, err := s.store.GetSchedule(scheduleID)
	if err != nil {
		return nil, err
	}
	if schedule == nil {
		return nil, fmt.Errorf("schedule not found: %s", scheduleID)
	}
	return s.enqueueSchedule(ctx, schedule)
}

func (s *Service) TriggerJob(ctx context.Context, kind, repoID, payloadJSON string, maxAttempts int, retryBackoff string, retryDelaySecs int) (*domain.Job, error) {
	job := &domain.Job{
		ID:             uuid.NewString(),
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
	s.emit(job, "queued", StatusQueued, "job queued", nil)
	select {
	case s.queue <- job:
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) WaitForTerminal(ctx context.Context, jobID string) (*domain.Job, error) {
	filter := sqlite.EventFilter{JobID: jobID}
	ch, cancel := s.Subscribe(filter, 16)
	defer cancel()
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
		case _, ok := <-ch:
			if !ok {
				return s.store.GetJob(jobID)
			}
		}
	}
}

func (s *Service) ListEvents(filter sqlite.EventFilter) ([]domain.JobEvent, error) {
	return s.store.ListJobEvents(filter)
}

func (s *Service) hydrateSchedules() error {
	items, err := s.store.ListSchedules("", true)
	if err != nil {
		return err
	}
	for i := range items {
		item := items[i]
		if err := s.registerSchedule(&item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) registerSchedule(item *domain.Schedule) error {
	entryID, err := s.cron.AddFunc(item.CronExpr, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := s.enqueueSchedule(ctx, item); err != nil {
			job := &domain.Job{ID: uuid.NewString(), ScheduleID: item.ID, RepoID: item.RepoID, Kind: item.JobKind}
			s.emit(job, "schedule_error", StatusFailed, err.Error(), map[string]any{"schedule_id": item.ID})
		}
	})
	if err != nil {
		return fmt.Errorf("register schedule %s: %w", item.ID, err)
	}
	nextRun, err := nextRun(item.CronExpr, time.Now().UTC())
	if err == nil {
		item.NextRunAt = &nextRun
		_ = s.store.UpdateScheduleRuntime(item.ID, item.LastRunAt, item.NextRunAt, true)
	}
	s.mu.Lock()
	s.scheduleIDs[item.ID] = entryID
	s.mu.Unlock()
	return nil
}

func (s *Service) enqueueSchedule(ctx context.Context, schedule *domain.Schedule) (*domain.Job, error) {
	now := time.Now().UTC()
	nextRun, err := nextRun(schedule.CronExpr, now)
	if err != nil {
		return nil, err
	}
	job := &domain.Job{
		ID:             uuid.NewString(),
		ScheduleID:     schedule.ID,
		RepoID:         schedule.RepoID,
		Kind:           schedule.JobKind,
		Status:         StatusQueued,
		MaxAttempts:    schedule.MaxAttempts,
		RetryBackoff:   schedule.RetryBackoff,
		RetryDelaySecs: schedule.RetryDelaySecs,
		PayloadJSON:    schedule.PayloadJSON,
		OutputJSON:     "{}",
	}
	if err := s.store.CreateJob(job); err != nil {
		return nil, err
	}
	schedule.LastRunAt = &now
	schedule.NextRunAt = &nextRun
	_ = s.store.UpdateScheduleRuntime(schedule.ID, schedule.LastRunAt, schedule.NextRunAt, true)
	s.emit(job, "queued", StatusQueued, "job queued", map[string]any{"schedule_id": schedule.ID})
	select {
	case s.queue <- job:
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-s.queue:
			if !ok {
				return
			}
			s.runJob(ctx, job)
		}
	}
}

func (s *Service) runJob(ctx context.Context, job *domain.Job) {
	payload := map[string]any{}
	_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)
	for attempt := 1; attempt <= max(1, job.MaxAttempts); attempt++ {
		now := time.Now().UTC()
		_ = s.store.UpdateJobAttempt(job.ID, attempt, StatusInProgress, &now)
		s.emit(job, "started", StatusInProgress, fmt.Sprintf("attempt %d started", attempt), map[string]any{"attempt": attempt})

		execCtx := context.WithValue(ctx, jobContextKey{}, job)
		output, status, err := s.execute(execCtx, job.Kind, job.RepoID, payload)
		if err == nil && status == "" {
			status = StatusCompleted
		}
		if err == nil {
			done := time.Now().UTC()
			outputJSON := mustJSON(output)
			_ = s.store.FinishJob(job.ID, status, outputJSON, "", &done)
			s.emit(job, "completed", status, "job completed", output)
			return
		}

		s.emit(job, "attempt_failed", StatusInProgress, err.Error(), map[string]any{"attempt": attempt})
		if attempt < max(1, job.MaxAttempts) {
			delay := calcRetryDelay(job.RetryBackoff, job.RetryDelaySecs, attempt)
			s.emit(job, "retry_scheduled", StatusInProgress, delay.String(), map[string]any{"attempt": attempt, "delay": delay.String()})
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				err = ctx.Err()
			}
			if ctx.Err() == nil {
				continue
			}
		}
		done := time.Now().UTC()
		_ = s.store.FinishJob(job.ID, StatusFailed, "{}", err.Error(), &done)
		s.emit(job, "failed", StatusFailed, err.Error(), nil)
		return
	}
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

func (s *Service) emit(job *domain.Job, eventType, status, message string, payload map[string]any) {
	item := &domain.JobEvent{
		JobID:       job.ID,
		ScheduleID:  job.ScheduleID,
		RepoID:      job.RepoID,
		JobKind:     job.Kind,
		EventType:   eventType,
		Status:      status,
		Message:     message,
		PayloadJSON: mustJSON(payload),
	}
	_ = s.store.CreateJobEvent(item)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subscribers {
		if matchesFilter(item, sub.filter) {
			select {
			case sub.ch <- *item:
			default:
			}
		}
	}
}

func matchesFilter(item *domain.JobEvent, filter sqlite.EventFilter) bool {
	if filter.RepoID != "" && filter.RepoID != item.RepoID {
		return false
	}
	if filter.ScheduleID != "" && filter.ScheduleID != item.ScheduleID {
		return false
	}
	if filter.JobID != "" && filter.JobID != item.JobID {
		return false
	}
	if filter.JobKind != "" && filter.JobKind != item.JobKind {
		return false
	}
	if filter.Status != "" && filter.Status != item.Status {
		return false
	}
	if filter.SinceID > 0 && item.ID <= filter.SinceID {
		return false
	}
	return true
}

func validateCron(expr string) error {
	_, err := cronParser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

func nextRun(expr string, from time.Time) (time.Time, error) {
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

func calcRetryDelay(backoff string, baseSeconds int, attempt int) time.Duration {
	if baseSeconds <= 0 {
		baseSeconds = 1
	}
	if attempt <= 0 {
		attempt = 1
	}
	base := time.Duration(baseSeconds) * time.Second
	switch strings.ToLower(strings.TrimSpace(backoff)) {
	case "linear":
		return time.Duration(attempt) * base
	case "exponential":
		return time.Duration(1<<(attempt-1)) * base
	default:
		return base
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
	report, err := retrieval.RefreshRepoEmbeddings(ctx, s.store, repoID, boolPayload(payload, "force"))
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

	tx, err := s.store.DB().Begin()
	if err != nil {
		return nil, "", fmt.Errorf("begin git history sweep tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM relationships WHERE repo_id = ? AND source = ?`, repoID, "git-history"); err != nil {
		return nil, "", fmt.Errorf("delete relationships by repo/source: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO relationships (
repo_id, src_symbol_id, dst_symbol_id, kind, weight, source, discovered_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(src_symbol_id, dst_symbol_id, kind, source) DO UPDATE SET
weight = excluded.weight,
discovered_at = excluded.discovered_at`)
	if err != nil {
		return nil, "", fmt.Errorf("prepare git history relationship upsert: %w", err)
	}
	defer stmt.Close()

	relationshipCount := 0
	symbolPairs := 0
	discoveredAt := time.Now().UTC()
	processedPairs := 0
	for idx, pair := range pairs {
		leftSymbols, err := s.store.ListSymbolsByFile(repoID, pair.Left)
		if err != nil {
			return nil, "", err
		}
		rightSymbols, err := s.store.ListSymbolsByFile(repoID, pair.Right)
		if err != nil {
			return nil, "", err
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
				if _, err := stmt.Exec(repoID, left.ID, right.ID, "co-changed-with", pair.Weight, "git-history", discoveredAt.Format(time.RFC3339)); err != nil {
					return nil, "", fmt.Errorf("upsert git history relationship: %w", err)
				}
				relationshipCount++
			}
		}
		if job != nil && processedPairs%250 == 0 {
			s.emit(job, "progress", StatusInProgress, fmt.Sprintf("processed %d/%d file pairs", idx+1, len(pairs)), map[string]any{
				"processed_pairs":     processedPairs,
				"total_pairs":         len(pairs),
				"relationships_added": relationshipCount,
			})
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit git history sweep: %w", err)
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
