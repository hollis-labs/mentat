package semcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/audits/import/deepreview"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

type Service struct {
	store *sqlite.Store
	jobs  *jobs.Service
}

func NewService(store *sqlite.Store, jobSvc *jobs.Service) *Service {
	return &Service{store: store, jobs: jobSvc}
}

type ActivitySummary struct {
	LatestAuditAt   *time.Time `json:"latest_audit_at,omitempty"`
	LatestFindingAt *time.Time `json:"latest_finding_at,omitempty"`
	LatestScanAt    *time.Time `json:"latest_scan_at,omitempty"`
	OpenFindings    int        `json:"open_findings"`
	AuditCount      int        `json:"audit_count"`
}

type ThemeCount struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Count       int    `json:"count"`
}

type FindingSummary struct {
	ID          int64    `json:"id"`
	RepoID      string   `json:"repo_id,omitempty"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	AuditID     *int64   `json:"audit_id,omitempty"`
	SymbolID    *int64   `json:"symbol_id,omitempty"`
	FilePaths   []string `json:"file_paths,omitempty"`
}

type CodeReferenceSummary struct {
	ID          int64  `json:"id"`
	RepoID      string `json:"repo_id"`
	FilePath    string `json:"file_path"`
	LineStart   *int   `json:"line_start,omitempty"`
	LineEnd     *int   `json:"line_end,omitempty"`
	Description string `json:"description,omitempty"`
	RefType     string `json:"ref_type,omitempty"`
	FindingID   *int64 `json:"finding_id,omitempty"`
	SymbolID    *int64 `json:"symbol_id,omitempty"`
}

type SymbolSummary struct {
	ID            int64  `json:"id"`
	RepoID        string `json:"repo_id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	FilePath      string `json:"file_path"`
	LineStart     int    `json:"line_start"`
	LineEnd       int    `json:"line_end"`
	Language      string `json:"language"`
}

type RepoContextOutput struct {
	Repo           *domain.Repo     `json:"repo,omitempty"`
	TopFindings    []FindingSummary `json:"top_findings"`
	TopThemes      []ThemeCount     `json:"top_themes"`
	RecentAudits   []audits.Audit   `json:"recent_audits"`
	LatestSnapshot *domain.Snapshot `json:"latest_snapshot,omitempty"`
	Activity       ActivitySummary  `json:"activity"`
}

func (s *Service) RepoContext(repoID string, page, pageSize int) (*RepoContextOutput, error) {
	out := &RepoContextOutput{}
	if repoID != "" {
		repo, err := s.store.GetRepo(repoID)
		if err != nil {
			return nil, err
		}
		out.Repo = repo
		out.LatestSnapshot = latestSnapshot(s.store, repoID)
	}

	findings, err := s.listFindings(repoID, "", page, pageSize)
	if err != nil {
		return nil, err
	}
	out.TopFindings = findings

	themes, err := s.listThemes(repoID, pageSize)
	if err != nil {
		return nil, err
	}
	out.TopThemes = themes

	auditStore := audits.NewStore(s.store.DB())
	auditItems, err := auditStore.ListAudits(audits.ListFilter{RepoID: repoID})
	if err != nil {
		return nil, err
	}
	if len(auditItems) > pageSize {
		auditItems = auditItems[:pageSize]
	}
	out.RecentAudits = auditItems

	out.Activity, err = s.activitySummary(repoID)
	if err != nil {
		return nil, err
	}

	return out, nil
}

type PriorArtForFileOutput struct {
	RepoID         string                 `json:"repo_id,omitempty"`
	Path           string                 `json:"path"`
	Findings       []FindingSummary       `json:"findings"`
	Themes         []ThemeCount           `json:"themes"`
	CodeReferences []CodeReferenceSummary `json:"code_references"`
	Symbols        []SymbolSummary        `json:"symbols"`
}

func (s *Service) PriorArtForFile(repoID, path string, page, pageSize int) (*PriorArtForFileOutput, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil, fmt.Errorf("path is required")
	}
	out := &PriorArtForFileOutput{RepoID: repoID, Path: path}

	findings, err := s.listFindingsForFile(repoID, path, page, pageSize)
	if err != nil {
		return nil, err
	}
	out.Findings = findings

	refs, err := s.listCodeRefsForFile(repoID, path, page, pageSize)
	if err != nil {
		return nil, err
	}
	out.CodeReferences = refs

	themes, err := s.listThemesForFile(repoID, path, pageSize)
	if err != nil {
		return nil, err
	}
	out.Themes = themes

	symbolsFound, err := s.store.SearchSymbols(model.SearchFilter{
		RepoID: repoID,
		Query:  path,
		Limit:  pageSize,
	})
	if err != nil {
		return nil, err
	}
	for _, sym := range symbolsFound {
		if filepath.Clean(sym.FilePath) != path {
			continue
		}
		out.Symbols = append(out.Symbols, summarizeSymbol(sym))
	}

	return out, nil
}

type PriorArtForSymbolOutput struct {
	Symbol         *SymbolSummary         `json:"symbol,omitempty"`
	Findings       []FindingSummary       `json:"findings"`
	Themes         []ThemeCount           `json:"themes"`
	CodeReferences []CodeReferenceSummary `json:"code_references"`
}

func (s *Service) PriorArtForSymbol(repoID, qualifiedName string, symbolID int64, page, pageSize int) (*PriorArtForSymbolOutput, error) {
	var (
		sym *model.Symbol
		err error
	)
	switch {
	case symbolID > 0:
		sym, err = s.store.GetSymbol(symbolID)
	case repoID != "" && strings.TrimSpace(qualifiedName) != "":
		sym, err = s.store.FindSymbolByQualifiedName(repoID, strings.TrimSpace(qualifiedName))
	default:
		return nil, fmt.Errorf("symbol_id or repo_id + qualified_name is required")
	}
	if err != nil {
		return nil, err
	}
	if sym == nil {
		return nil, fmt.Errorf("symbol not found")
	}

	out := &PriorArtForSymbolOutput{
		Symbol: &SymbolSummary{
			ID:            sym.ID,
			RepoID:        sym.RepoID,
			Kind:          sym.Kind,
			Name:          sym.Name,
			QualifiedName: sym.QualifiedName,
			FilePath:      sym.FilePath,
			LineStart:     derefInt(sym.LineStart),
			LineEnd:       derefInt(sym.LineEnd),
			Language:      sym.Language,
		},
	}

	out.Findings, err = s.listFindingsForSymbol(sym, page, pageSize)
	if err != nil {
		return nil, err
	}
	out.CodeReferences, err = s.listCodeRefsForSymbol(sym, page, pageSize)
	if err != nil {
		return nil, err
	}
	out.Themes, err = s.listThemesForSymbol(sym, pageSize)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) FindingSearch(query, repoID string, limit int) ([]retrieval.Result, error) {
	return retrieval.NewService(s.store).Search(context.Background(), retrieval.SearchOptions{
		Query:  query,
		RepoID: repoID,
		Kind:   "finding",
		Limit:  limit,
	})
}

func (s *Service) SymbolLookup(repoID, query, kind, language string, limit int) ([]model.Symbol, error) {
	return s.store.SearchSymbols(model.SearchFilter{
		RepoID:   repoID,
		Query:    query,
		Kind:     kind,
		Language: language,
		Limit:    limit,
	})
}

func (s *Service) AuditShow(id int64) (*audits.AuditBundle, error) {
	return audits.NewStore(s.store.DB()).GetAuditBundle(id)
}

func (s *Service) EventStreamSubscribe(ctx context.Context, filter sqlite.EventFilter, limit int, timeout time.Duration, push func(domain.JobEvent)) ([]domain.JobEvent, error) {
	if s.jobs == nil {
		return nil, fmt.Errorf("jobs service unavailable")
	}
	if limit <= 0 {
		limit = 25
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	filter.Limit = limit
	items, err := s.jobs.ListEvents(filter)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		push(item)
		filter.SinceID = item.ID
	}
	if len(items) >= limit {
		return items[:limit], nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch, stop := s.jobs.Subscribe(filter, limit)
	defer stop()
	for len(items) < limit {
		select {
		case <-ctx.Done():
			return items, nil
		case item, ok := <-ch:
			if !ok {
				return items, nil
			}
			items = append(items, item)
			push(item)
		}
	}
	return items, nil
}

func (s *Service) KnowledgeQuery(query, repoID, kind string, limit int) ([]retrieval.Result, error) {
	return retrieval.NewService(s.store).Search(context.Background(), retrieval.SearchOptions{
		Query:  query,
		RepoID: repoID,
		Kind:   kind,
		Limit:  limit,
	})
}

type FindingCreateInput struct {
	RepoID       string `json:"repo_id,omitempty" jsonschema:"optional repo filter for the finding"`
	Title        string `json:"title" jsonschema:"finding title"`
	Category     string `json:"category,omitempty" jsonschema:"gap, strength, opportunity, or risk"`
	Severity     string `json:"severity,omitempty" jsonschema:"critical, high, medium, low, or info"`
	Status       string `json:"status,omitempty" jsonschema:"open, acknowledged, resolved, or wontfix"`
	Description  string `json:"description,omitempty" jsonschema:"finding body text"`
	AuditID      *int64 `json:"audit_id,omitempty" jsonschema:"optional linked audit id"`
	SymbolID     *int64 `json:"symbol_id,omitempty" jsonschema:"optional linked symbol id"`
	BodyMarkdown string `json:"body_markdown,omitempty" jsonschema:"optional markdown body"`
}

func (s *Service) AddFinding(in FindingCreateInput, provenance audits.Provenance) (*FindingSummary, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	if in.Status == "" {
		in.Status = "open"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var repoID any
	if in.RepoID != "" {
		repoID = in.RepoID
	}
	result, err := s.store.DB().Exec(`INSERT INTO findings (
repo_id, title, category, severity, description, status, created_at, updated_at,
audit_id, body_markdown, symbol_id, actor_kind, actor_id, session_id, tool_name, model_name
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repoID, in.Title, in.Category, in.Severity, in.Description, in.Status, now, now,
		in.AuditID, in.BodyMarkdown, in.SymbolID, provenance.ActorKind, provenance.ActorID,
		nullString(provenance.SessionID), nullString(provenance.ToolName), nullString(provenance.ModelName),
	)
	if err != nil {
		return nil, fmt.Errorf("insert finding: %w", err)
	}
	id, _ := result.LastInsertId()
	item, err := s.getFindingSummary(id)
	if err != nil {
		return nil, err
	}
	return item, nil
}

type FindingStatusUpdateInput struct {
	ID     int64  `json:"id" jsonschema:"finding id"`
	Status string `json:"status" jsonschema:"new finding status: open, acknowledged, resolved, or wontfix"`
}

func (s *Service) UpdateFindingStatus(in FindingStatusUpdateInput, provenance audits.Provenance) (*FindingSummary, error) {
	if in.ID <= 0 {
		return nil, fmt.Errorf("id is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		return nil, fmt.Errorf("status is required")
	}
	if err := domain.ValidateFindingStatus(status); err != nil {
		return nil, err
	}
	in.Status = status
	_, err := s.store.DB().Exec(`UPDATE findings
SET status = ?, updated_at = ?, actor_kind = ?, actor_id = ?, session_id = ?, tool_name = ?, model_name = ?
WHERE id = ?`,
		in.Status, time.Now().UTC().Format(time.RFC3339), provenance.ActorKind, provenance.ActorID,
		nullString(provenance.SessionID), nullString(provenance.ToolName), nullString(provenance.ModelName), in.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update finding status: %w", err)
	}
	return s.getFindingSummary(in.ID)
}

type AuditImportInput struct {
	Folder string `json:"folder" jsonschema:"folder containing one or more deep-review audits"`
	RepoID string `json:"repo_id" jsonschema:"repo id to attach imported audits to"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"when true, parse only without writing"`
}

type AuditImportResult struct {
	RepoID    string   `json:"repo_id"`
	Folder    string   `json:"folder"`
	DryRun    bool     `json:"dry_run"`
	Imported  []int64  `json:"imported_audit_ids,omitempty"`
	Summaries []string `json:"summaries"`
}

func (s *Service) AuditImport(in AuditImportInput, provenance audits.Provenance) (*AuditImportResult, error) {
	if strings.TrimSpace(in.Folder) == "" || strings.TrimSpace(in.RepoID) == "" {
		return nil, fmt.Errorf("folder and repo_id are required")
	}
	dirs, err := auditDirectories(in.Folder)
	if err != nil {
		return nil, err
	}
	out := &AuditImportResult{RepoID: in.RepoID, Folder: in.Folder, DryRun: in.DryRun}
	auditStore := audits.NewStore(s.store.DB())
	for _, dir := range dirs {
		bundle, err := deepreview.ParseDir(dir, in.RepoID, provenance)
		if err != nil {
			return nil, err
		}
		if in.DryRun {
			out.Summaries = append(out.Summaries, fmt.Sprintf("would import %s (%d findings)", bundle.Audit.Scope, len(bundle.Findings)))
			continue
		}
		saved, err := auditStore.ReplaceImportedAudit(*bundle)
		if err != nil {
			return nil, err
		}
		out.Imported = append(out.Imported, saved.Audit.ID)
		out.Summaries = append(out.Summaries, fmt.Sprintf("imported audit #%d %s (%d findings)", saved.Audit.ID, saved.Audit.Scope, len(saved.Findings)))
	}
	return out, nil
}

type AuditExportInput struct {
	AuditID int64  `json:"audit_id" jsonschema:"audit id to export"`
	OutDir  string `json:"out_dir,omitempty" jsonschema:"optional target directory"`
}

type AuditExportResult struct {
	AuditID int64  `json:"audit_id"`
	OutDir  string `json:"out_dir"`
}

func (s *Service) AuditExport(in AuditExportInput) (*AuditExportResult, error) {
	if in.AuditID <= 0 {
		return nil, fmt.Errorf("audit_id is required")
	}
	bundle, err := audits.NewStore(s.store.DB()).GetAuditBundle(in.AuditID)
	if err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, fmt.Errorf("audit not found")
	}
	if strings.TrimSpace(in.OutDir) == "" {
		in.OutDir = filepath.Join(".", fmt.Sprintf("audit-%d-export", in.AuditID))
	}
	if err := audits.ExportBundle(bundle, in.OutDir); err != nil {
		return nil, err
	}
	return &AuditExportResult{AuditID: in.AuditID, OutDir: in.OutDir}, nil
}

// provenanceFromContext derives write-tool provenance from environment
// defaults, overridden by the caller-attributed X-Stack-Explorer-* headers
// on an HTTP call (see provenance_http.go). go-mcp's ToolHandler exposes
// only (ctx, args map[string]any) -- there is no request/session object to
// read a stdio session id or bearer-token identity from, so those two
// narrower signals the previous, official-SDK-generic-AddTool-based
// provenance read (session ID, verified TokenInfo.UserID) are gone; neither
// had test or documentation coverage, and this server does not wire an auth
// provider, so TokenInfo was always nil in practice.
func provenanceFromContext(ctx context.Context, toolName string) audits.Provenance {
	out := audits.Provenance{
		ActorKind: firstNonEmpty(os.Getenv("STACK_EXPLORER_MCP_ACTOR_KIND"), "agent"),
		ActorID:   firstNonEmpty(os.Getenv("STACK_EXPLORER_MCP_ACTOR_ID"), "mcp-client"),
		SessionID: os.Getenv("STACK_EXPLORER_SESSION_ID"),
		ToolName:  toolName,
		ModelName: os.Getenv("STACK_EXPLORER_MCP_MODEL"),
	}
	if headers := provenanceHeadersFromContext(ctx); headers != nil {
		out.ActorKind = firstNonEmpty(headers["X-Stack-Explorer-Actor-Kind"], out.ActorKind)
		out.ActorID = firstNonEmpty(headers["X-Stack-Explorer-Actor-Id"], out.ActorID)
		out.SessionID = firstNonEmpty(headers["X-Stack-Explorer-Session-Id"], out.SessionID)
		out.ModelName = firstNonEmpty(headers["X-Stack-Explorer-Model"], out.ModelName)
	}
	return out
}

func (s *Service) activitySummary(repoID string) (ActivitySummary, error) {
	out := ActivitySummary{}
	var args []any
	findingWhere := ""
	auditWhere := ""
	snapshotWhere := ""
	if repoID != "" {
		findingWhere = " WHERE repo_id = ?"
		auditWhere = " WHERE repo_id = ?"
		snapshotWhere = " WHERE repo_id = ?"
		args = append(args, repoID)
	}
	var latestFinding, latestAudit, latestScan sql.NullString
	if err := s.store.DB().QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END), 0), MAX(updated_at) FROM findings"+findingWhere, args...).Scan(new(int), &out.OpenFindings, &latestFinding); err != nil {
		return out, err
	}
	if err := s.store.DB().QueryRow("SELECT COUNT(*), MAX(started_at) FROM audits"+auditWhere, args...).Scan(&out.AuditCount, &latestAudit); err != nil {
		return out, err
	}
	if err := s.store.DB().QueryRow("SELECT MAX(captured_at) FROM snapshots"+snapshotWhere, args...).Scan(&latestScan); err != nil {
		return out, err
	}
	out.LatestFindingAt = parseNullableTime(latestFinding)
	out.LatestAuditAt = parseNullableTime(latestAudit)
	out.LatestScanAt = parseNullableTime(latestScan)
	return out, nil
}

func (s *Service) listFindings(repoID, status string, page, pageSize int) ([]FindingSummary, error) {
	query := `SELECT f.id, COALESCE(f.repo_id,''), f.title, f.category, f.severity, f.status, f.description, f.audit_id, f.symbol_id
FROM findings f WHERE 1=1`
	var args []any
	if repoID != "" {
		query += " AND f.repo_id = ?"
		args = append(args, repoID)
	}
	if status != "" {
		query += " AND f.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY CASE f.status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END, CASE f.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END, f.updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset(page, pageSize))
	return s.scanFindingSummaries(query, args...)
}

func (s *Service) listFindingsForFile(repoID, path string, page, pageSize int) ([]FindingSummary, error) {
	query := `SELECT DISTINCT f.id, COALESCE(f.repo_id,''), f.title, f.category, f.severity, f.status, f.description, f.audit_id, f.symbol_id
FROM findings f
JOIN code_references cr ON cr.finding_id = f.id
WHERE (cr.file_path = ? OR cr.file_path LIKE ?)`
	args := []any{path, "%/" + path}
	if repoID != "" {
		query += " AND COALESCE(f.repo_id, cr.repo_id, '') = ?"
		args = append(args, repoID)
	}
	query += " ORDER BY CASE f.status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END, f.updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset(page, pageSize))
	return s.scanFindingSummaries(query, args...)
}

func (s *Service) listFindingsForSymbol(sym *model.Symbol, page, pageSize int) ([]FindingSummary, error) {
	query := `SELECT DISTINCT f.id, COALESCE(f.repo_id,''), f.title, f.category, f.severity, f.status, f.description, f.audit_id, f.symbol_id
FROM findings f
LEFT JOIN code_references cr ON cr.finding_id = f.id
WHERE f.symbol_id = ? OR cr.symbol_id = ? OR (COALESCE(f.repo_id, '') = ? AND cr.file_path = ?)
ORDER BY CASE f.status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END, f.updated_at DESC LIMIT ? OFFSET ?`
	return s.scanFindingSummaries(query, sym.ID, sym.ID, sym.RepoID, sym.FilePath, pageSize, offset(page, pageSize))
}

func (s *Service) scanFindingSummaries(query string, args ...any) ([]FindingSummary, error) {
	rows, err := s.store.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FindingSummary
	for rows.Next() {
		var item FindingSummary
		if err := rows.Scan(&item.ID, &item.RepoID, &item.Title, &item.Category, &item.Severity, &item.Status, &item.Description, &item.AuditID, &item.SymbolID); err != nil {
			return nil, err
		}
		paths, err := s.findingPaths(item.ID)
		if err != nil {
			return nil, err
		}
		item.FilePaths = paths
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) findingPaths(findingID int64) ([]string, error) {
	rows, err := s.store.DB().Query(`SELECT DISTINCT file_path FROM code_references WHERE finding_id = ? ORDER BY file_path`, findingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Service) listThemes(repoID string, limit int) ([]ThemeCount, error) {
	query := `SELECT t.name, t.description, COUNT(*) AS c
FROM finding_themes ft
JOIN audit_themes t ON t.id = ft.theme_id
LEFT JOIN findings f ON f.id = ft.finding_id
WHERE 1=1`
	var args []any
	if repoID != "" {
		query += " AND COALESCE(f.repo_id, t.repo_id, '') = ?"
		args = append(args, repoID)
	}
	query += " GROUP BY t.id, t.name, t.description ORDER BY c DESC, t.name LIMIT ?"
	args = append(args, limit)
	return scanThemeCounts(s.store.DB(), query, args...)
}

func (s *Service) listThemesForFile(repoID, path string, limit int) ([]ThemeCount, error) {
	query := `SELECT t.name, t.description, COUNT(DISTINCT ft.finding_id) AS c
FROM finding_themes ft
JOIN audit_themes t ON t.id = ft.theme_id
JOIN code_references cr ON cr.finding_id = ft.finding_id
LEFT JOIN findings f ON f.id = ft.finding_id
WHERE (cr.file_path = ? OR cr.file_path LIKE ?)`
	args := []any{path, "%/" + path}
	if repoID != "" {
		query += " AND COALESCE(f.repo_id, cr.repo_id, t.repo_id, '') = ?"
		args = append(args, repoID)
	}
	query += " GROUP BY t.id, t.name, t.description ORDER BY c DESC, t.name LIMIT ?"
	args = append(args, limit)
	return scanThemeCounts(s.store.DB(), query, args...)
}

func (s *Service) listThemesForSymbol(sym *model.Symbol, limit int) ([]ThemeCount, error) {
	query := `SELECT t.name, t.description, COUNT(DISTINCT ft.finding_id) AS c
FROM finding_themes ft
JOIN audit_themes t ON t.id = ft.theme_id
LEFT JOIN findings f ON f.id = ft.finding_id
LEFT JOIN code_references cr ON cr.finding_id = ft.finding_id
WHERE f.symbol_id = ? OR cr.symbol_id = ? OR cr.file_path = ?
GROUP BY t.id, t.name, t.description ORDER BY c DESC, t.name LIMIT ?`
	return scanThemeCounts(s.store.DB(), query, sym.ID, sym.ID, sym.FilePath, limit)
}

func scanThemeCounts(db *sql.DB, query string, args ...any) ([]ThemeCount, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThemeCount
	for rows.Next() {
		var item ThemeCount
		if err := rows.Scan(&item.Name, &item.Description, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) listCodeRefsForFile(repoID, path string, page, pageSize int) ([]CodeReferenceSummary, error) {
	query := `SELECT id, repo_id, file_path, line_start, line_end, description, ref_type, finding_id, symbol_id
FROM code_references WHERE (file_path = ? OR file_path LIKE ?)`
	args := []any{path, "%/" + path}
	if repoID != "" {
		query += " AND repo_id = ?"
		args = append(args, repoID)
	}
	query += " ORDER BY id LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset(page, pageSize))
	return scanCodeRefSummaries(s.store.DB(), query, args...)
}

func (s *Service) listCodeRefsForSymbol(sym *model.Symbol, page, pageSize int) ([]CodeReferenceSummary, error) {
	query := `SELECT id, repo_id, file_path, line_start, line_end, description, ref_type, finding_id, symbol_id
FROM code_references WHERE symbol_id = ? OR file_path = ?
ORDER BY id LIMIT ? OFFSET ?`
	return scanCodeRefSummaries(s.store.DB(), query, sym.ID, sym.FilePath, pageSize, offset(page, pageSize))
}

func scanCodeRefSummaries(db *sql.DB, query string, args ...any) ([]CodeReferenceSummary, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CodeReferenceSummary
	for rows.Next() {
		var (
			item      CodeReferenceSummary
			lineStart sql.NullInt64
			lineEnd   sql.NullInt64
		)
		if err := rows.Scan(&item.ID, &item.RepoID, &item.FilePath, &lineStart, &lineEnd, &item.Description, &item.RefType, &item.FindingID, &item.SymbolID); err != nil {
			return nil, err
		}
		if lineStart.Valid {
			v := int(lineStart.Int64)
			item.LineStart = &v
		}
		if lineEnd.Valid {
			v := int(lineEnd.Int64)
			item.LineEnd = &v
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) getFindingSummary(id int64) (*FindingSummary, error) {
	items, err := s.scanFindingSummaries(`SELECT f.id, COALESCE(f.repo_id,''), f.title, f.category, f.severity, f.status, f.description, f.audit_id, f.symbol_id FROM findings f WHERE f.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("finding not found")
	}
	return &items[0], nil
}

func summarizeSymbol(sym model.Symbol) SymbolSummary {
	return SymbolSummary{
		ID:            sym.ID,
		RepoID:        sym.RepoID,
		Kind:          sym.Kind,
		Name:          sym.Name,
		QualifiedName: sym.QualifiedName,
		FilePath:      sym.FilePath,
		LineStart:     derefInt(sym.LineStart),
		LineEnd:       derefInt(sym.LineEnd),
		Language:      sym.Language,
	}
}

func latestSnapshot(store *sqlite.Store, repoID string) *domain.Snapshot {
	items, err := store.ListSnapshots(repoID, 1)
	if err != nil || len(items) == 0 {
		return nil
	}
	return &items[0]
}

func auditDirectories(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	index, err := findIndexFile(root)
	if err != nil {
		return nil, err
	}
	if index != "" {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		index, err := findIndexFile(dir)
		if err != nil {
			return nil, err
		}
		if index != "" {
			out = append(out, dir)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no audit directories found in %s", root)
	}
	return out, nil
}

func findIndexFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), "index.md") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", nil
}

func offset(page, pageSize int) int {
	if page < 1 {
		page = 1
	}
	return (page - 1) * pageSize
}

func parseNullableTime(raw sql.NullString) *time.Time {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
