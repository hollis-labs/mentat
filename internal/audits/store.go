package audits

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateAudit(a *Audit) error {
	now := time.Now().UTC()
	if a.StartedAt.IsZero() {
		a.StartedAt = now
	}
	a.CreatedAt = now
	a.UpdatedAt = now
	scopePaths, err := json.Marshal(a.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}
	result, err := s.db.Exec(`INSERT INTO audits (
		repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.RepoID, a.Scope, string(scopePaths), a.AuditType, a.Auditor, a.AuditedAtRef, a.SummaryMarkdown,
		a.Verdict, a.Status, a.SupersedesID, a.Provenance.ActorKind, a.Provenance.ActorID,
		nullIfEmpty(a.Provenance.SessionID), nullIfEmpty(a.Provenance.ToolName), a.Provenance.ModelName,
		a.StartedAt.Format(time.RFC3339), timePtrString(a.FinishedAt), a.CreatedAt.Format(time.RFC3339), a.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	a.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) UpdateAudit(a *Audit) error {
	a.UpdatedAt = time.Now().UTC()
	scopePaths, err := json.Marshal(a.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}
	_, err = s.db.Exec(`UPDATE audits SET
		scope=?, scope_paths=?, audit_type=?, auditor=?, audited_at_ref=?, summary_markdown=?,
		verdict=?, status=?, supersedes_id=?, actor_kind=?, actor_id=?, session_id=?, tool_name=?,
		model_name=?, started_at=?, finished_at=?, updated_at=?
		WHERE id=?`,
		a.Scope, string(scopePaths), a.AuditType, a.Auditor, a.AuditedAtRef, a.SummaryMarkdown,
		a.Verdict, a.Status, a.SupersedesID, a.Provenance.ActorKind, a.Provenance.ActorID,
		nullIfEmpty(a.Provenance.SessionID), nullIfEmpty(a.Provenance.ToolName), a.Provenance.ModelName,
		a.StartedAt.Format(time.RFC3339), timePtrString(a.FinishedAt), a.UpdatedAt.Format(time.RFC3339), a.ID,
	)
	if err != nil {
		return fmt.Errorf("update audit: %w", err)
	}
	return nil
}

func (s *Store) FindAuditForImport(repoID, scope string, auditedAtRef *string) (*Audit, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
		FROM audits WHERE repo_id = ? AND scope = ? AND COALESCE(audited_at_ref, '') = COALESCE(?, '') LIMIT 1`,
		repoID, scope, auditedAtRef)
	a, err := scanAudit(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find audit for import: %w", err)
	}
	return a, nil
}

func (s *Store) GetAudit(id int64) (*Audit, error) {
	row := s.db.QueryRow(`SELECT id, repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
		FROM audits WHERE id = ?`, id)
	a, err := scanAudit(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get audit: %w", err)
	}
	return a, nil
}

func (s *Store) ListAudits(filter ListFilter) ([]Audit, error) {
	query := `SELECT id, repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
		FROM audits WHERE 1=1`
	var args []any
	if filter.RepoID != "" {
		query += " AND repo_id = ?"
		args = append(args, filter.RepoID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	query += " ORDER BY started_at DESC, id DESC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audits: %w", err)
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceImportedAudit(bundle ImportBundle) (*AuditBundle, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	existing, err := findAuditForImportTx(tx, bundle.Audit.RepoID, bundle.Audit.Scope, bundle.Audit.AuditedAtRef)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		bundle.Audit.ID = existing.ID
		bundle.Audit.CreatedAt = existing.CreatedAt
		if err := updateAuditTx(tx, bundle.Audit); err != nil {
			return nil, err
		}
		if _, err := tx.Exec("DELETE FROM findings WHERE audit_id = ?", existing.ID); err != nil {
			return nil, fmt.Errorf("delete existing findings: %w", err)
		}
	} else {
		if err := createAuditTx(tx, bundle.Audit); err != nil {
			return nil, err
		}
	}

	for i := range bundle.Findings {
		bundle.Findings[i].AuditID = &bundle.Audit.ID
		if bundle.Findings[i].RepoID == nil {
			bundle.Findings[i].RepoID = &bundle.Audit.RepoID
		}
		if bundle.Findings[i].AuditedAtRef == nil {
			bundle.Findings[i].AuditedAtRef = bundle.Audit.AuditedAtRef
		}
		bundle.Findings[i].CreatedAt = time.Now().UTC()
		bundle.Findings[i].UpdatedAt = bundle.Findings[i].CreatedAt
		id, err := insertFindingTx(tx, &bundle.Findings[i])
		if err != nil {
			return nil, err
		}
		bundle.Findings[i].ID = id
		for _, ref := range bundle.Findings[i].CodeRefs {
			ref.FindingID = &id
			if ref.RepoID == "" {
				ref.RepoID = bundle.Audit.RepoID
			}
			if _, err := tx.Exec(`INSERT INTO code_references (repo_id, file_path, line_start, line_end, description, ref_type, pattern_id, finding_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				ref.RepoID, ref.FilePath, ref.LineStart, ref.LineEnd, ref.Description, ref.RefType, ref.PatternID, ref.FindingID, time.Now().UTC().Format(time.RFC3339)); err != nil {
				return nil, fmt.Errorf("insert code reference: %w", err)
			}
		}
		for _, theme := range bundle.Findings[i].Themes {
			themeID, err := upsertThemeTx(tx, bundle.Audit.RepoID, theme.Name, theme.Description)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO finding_themes (finding_id, theme_id) VALUES (?, ?)`, id, themeID); err != nil {
				return nil, fmt.Errorf("link finding theme: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit import tx: %w", err)
	}
	return s.GetAuditBundle(bundle.Audit.ID)
}

func (s *Store) GetAuditBundle(id int64) (*AuditBundle, error) {
	audit, err := s.GetAudit(id)
	if err != nil {
		return nil, err
	}
	if audit == nil {
		return nil, nil
	}
	findings, err := s.ListAuditFindings(id)
	if err != nil {
		return nil, err
	}
	themeMap := map[int64]Theme{}
	for _, finding := range findings {
		for _, theme := range finding.Themes {
			themeMap[theme.ID] = theme
		}
	}
	themes := make([]Theme, 0, len(themeMap))
	for _, theme := range themeMap {
		themes = append(themes, theme)
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	return &AuditBundle{Audit: audit, Findings: findings, Themes: themes}, nil
}

func (s *Store) ListAuditFindings(auditID int64) ([]Finding, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, title, category, severity, description, status, created_at, updated_at,
		audit_id, body_markdown, audited_at_ref, is_out_of_scope, symbol_id, actor_kind, actor_id, session_id, tool_name, model_name
		FROM findings WHERE audit_id = ? ORDER BY severity_rank(severity), id`, auditID)
	if err != nil {
		rows, err = s.db.Query(`SELECT id, repo_id, title, category, severity, description, status, created_at, updated_at,
			audit_id, body_markdown, audited_at_ref, is_out_of_scope, symbol_id, actor_kind, actor_id, session_id, tool_name, model_name
			FROM findings WHERE audit_id = ? ORDER BY id`, auditID)
		if err != nil {
			return nil, fmt.Errorf("list audit findings: %w", err)
		}
	}
	defer rows.Close()
	var findings []Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		refs, err := s.listFindingCodeRefs(f.ID)
		if err != nil {
			return nil, err
		}
		f.CodeRefs = refs
		themes, err := s.listFindingThemes(f.ID)
		if err != nil {
			return nil, err
		}
		f.Themes = themes
		findings = append(findings, *f)
	}
	return findings, rows.Err()
}

func (s *Store) listFindingCodeRefs(findingID int64) ([]domain.CodeReference, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, file_path, line_start, line_end, description, ref_type, pattern_id, finding_id, created_at
		FROM code_references WHERE finding_id = ? ORDER BY id`, findingID)
	if err != nil {
		return nil, fmt.Errorf("list finding code refs: %w", err)
	}
	defer rows.Close()
	var refs []domain.CodeReference
	for rows.Next() {
		var ref domain.CodeReference
		var createdAt string
		if err := rows.Scan(&ref.ID, &ref.RepoID, &ref.FilePath, &ref.LineStart, &ref.LineEnd, &ref.Description, &ref.RefType, &ref.PatternID, &ref.FindingID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan code ref: %w", err)
		}
		ref.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Store) listFindingThemes(findingID int64) ([]Theme, error) {
	rows, err := s.db.Query(`SELECT t.id, t.repo_id, t.name, t.description, t.created_at, t.updated_at
		FROM audit_themes t
		JOIN finding_themes ft ON ft.theme_id = t.id
		WHERE ft.finding_id = ?
		ORDER BY t.name`, findingID)
	if err != nil {
		return nil, fmt.Errorf("list finding themes: %w", err)
	}
	defer rows.Close()
	var themes []Theme
	for rows.Next() {
		var theme Theme
		var repoID sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&theme.ID, &repoID, &theme.Name, &theme.Description, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan theme: %w", err)
		}
		if repoID.Valid {
			theme.RepoID = &repoID.String
		}
		theme.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		theme.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		themes = append(themes, theme)
	}
	return themes, rows.Err()
}

func insertFindingTx(tx *sql.Tx, f *Finding) (int64, error) {
	result, err := tx.Exec(`INSERT INTO findings (
		repo_id, title, category, severity, description, status, created_at, updated_at,
		audit_id, body_markdown, audited_at_ref, is_out_of_scope, symbol_id,
		actor_kind, actor_id, session_id, tool_name, model_name
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.RepoID, f.Title, f.Category, f.Severity, f.Description, f.Status,
		f.CreatedAt.Format(time.RFC3339), f.UpdatedAt.Format(time.RFC3339),
		f.AuditID, f.BodyMarkdown, f.AuditedAtRef, boolToInt(f.IsOutOfScope), f.SymbolID,
		f.Provenance.ActorKind, f.Provenance.ActorID, nullIfEmpty(f.Provenance.SessionID),
		nullIfEmpty(f.Provenance.ToolName), f.Provenance.ModelName,
	)
	if err != nil {
		return 0, fmt.Errorf("insert finding: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}

func findAuditForImportTx(tx *sql.Tx, repoID, scope string, auditedAtRef *string) (*Audit, error) {
	row := tx.QueryRow(`SELECT id, repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
		FROM audits WHERE repo_id = ? AND scope = ? AND COALESCE(audited_at_ref, '') = COALESCE(?, '') LIMIT 1`,
		repoID, scope, auditedAtRef)
	a, err := scanAudit(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find audit for import: %w", err)
	}
	return a, nil
}

func createAuditTx(tx *sql.Tx, a *Audit) error {
	now := time.Now().UTC()
	if a.StartedAt.IsZero() {
		a.StartedAt = now
	}
	a.CreatedAt = now
	a.UpdatedAt = now
	scopePaths, err := json.Marshal(a.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}
	result, err := tx.Exec(`INSERT INTO audits (
		repo_id, scope, scope_paths, audit_type, auditor, audited_at_ref, summary_markdown,
		verdict, status, supersedes_id, actor_kind, actor_id, session_id, tool_name, model_name,
		started_at, finished_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.RepoID, a.Scope, string(scopePaths), a.AuditType, a.Auditor, a.AuditedAtRef, a.SummaryMarkdown,
		a.Verdict, a.Status, a.SupersedesID, a.Provenance.ActorKind, a.Provenance.ActorID,
		nullIfEmpty(a.Provenance.SessionID), nullIfEmpty(a.Provenance.ToolName), a.Provenance.ModelName,
		a.StartedAt.Format(time.RFC3339), timePtrString(a.FinishedAt), a.CreatedAt.Format(time.RFC3339), a.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	a.ID, _ = result.LastInsertId()
	return nil
}

func updateAuditTx(tx *sql.Tx, a *Audit) error {
	a.UpdatedAt = time.Now().UTC()
	scopePaths, err := json.Marshal(a.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}
	if _, err := tx.Exec(`UPDATE audits SET
		scope=?, scope_paths=?, audit_type=?, auditor=?, audited_at_ref=?, summary_markdown=?,
		verdict=?, status=?, supersedes_id=?, actor_kind=?, actor_id=?, session_id=?, tool_name=?,
		model_name=?, started_at=?, finished_at=?, updated_at=?
		WHERE id=?`,
		a.Scope, string(scopePaths), a.AuditType, a.Auditor, a.AuditedAtRef, a.SummaryMarkdown,
		a.Verdict, a.Status, a.SupersedesID, a.Provenance.ActorKind, a.Provenance.ActorID,
		nullIfEmpty(a.Provenance.SessionID), nullIfEmpty(a.Provenance.ToolName), a.Provenance.ModelName,
		a.StartedAt.Format(time.RFC3339), timePtrString(a.FinishedAt), a.UpdatedAt.Format(time.RFC3339), a.ID,
	); err != nil {
		return fmt.Errorf("update audit: %w", err)
	}
	return nil
}

func upsertThemeTx(tx *sql.Tx, repoID, name, description string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`INSERT INTO audit_themes (repo_id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, name) DO UPDATE SET description = excluded.description, updated_at = excluded.updated_at`,
		repoID, name, description, now, now); err != nil {
		return 0, fmt.Errorf("upsert theme: %w", err)
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM audit_themes WHERE repo_id = ? AND name = ?`, repoID, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("select theme: %w", err)
	}
	return id, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAudit(row scanner) (*Audit, error) {
	var a Audit
	var scopePaths string
	var auditedAtRef sql.NullString
	var verdict sql.NullString
	var supersedesID sql.NullInt64
	var sessionID sql.NullString
	var toolName sql.NullString
	var modelName sql.NullString
	var finishedAt sql.NullString
	var createdAt, updatedAt, startedAt string
	if err := row.Scan(
		&a.ID, &a.RepoID, &a.Scope, &scopePaths, &a.AuditType, &a.Auditor, &auditedAtRef, &a.SummaryMarkdown,
		&verdict, &a.Status, &supersedesID, &a.Provenance.ActorKind, &a.Provenance.ActorID, &sessionID,
		&toolName, &modelName, &startedAt, &finishedAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopePaths), &a.ScopePaths)
	if auditedAtRef.Valid {
		a.AuditedAtRef = &auditedAtRef.String
	}
	if verdict.Valid {
		a.Verdict = &verdict.String
	}
	if supersedesID.Valid {
		a.SupersedesID = &supersedesID.Int64
	}
	a.Provenance.SessionID = sessionID.String
	a.Provenance.ToolName = toolName.String
	a.Provenance.ModelName = modelName.String
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if finishedAt.Valid {
		t, _ := time.Parse(time.RFC3339, finishedAt.String)
		a.FinishedAt = &t
	}
	return &a, nil
}

func scanFinding(row scanner) (*Finding, error) {
	var f Finding
	var repoID sql.NullString
	var createdAt, updatedAt string
	var auditID sql.NullInt64
	var auditedAtRef sql.NullString
	var symbolID sql.NullInt64
	var sessionID, toolName, modelName sql.NullString
	var isOutOfScope int
	if err := row.Scan(&f.ID, &repoID, &f.Title, &f.Category, &f.Severity, &f.Description, &f.Status, &createdAt, &updatedAt,
		&auditID, &f.BodyMarkdown, &auditedAtRef, &isOutOfScope, &symbolID,
		&f.Provenance.ActorKind, &f.Provenance.ActorID, &sessionID, &toolName, &modelName); err != nil {
		return nil, err
	}
	if repoID.Valid {
		f.RepoID = &repoID.String
	}
	if auditID.Valid {
		f.AuditID = &auditID.Int64
	}
	if auditedAtRef.Valid {
		f.AuditedAtRef = &auditedAtRef.String
	}
	if symbolID.Valid {
		f.SymbolID = &symbolID.Int64
	}
	f.IsOutOfScope = isOutOfScope == 1
	f.Provenance.SessionID = sessionID.String
	f.Provenance.ToolName = toolName.String
	f.Provenance.ModelName = modelName.String
	f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &f, nil
}

func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func timePtrString(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
