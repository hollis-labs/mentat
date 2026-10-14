package audits

import (
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

type Provenance struct {
	ActorKind string `json:"actor_kind"`
	ActorID   string `json:"actor_id"`
	SessionID string `json:"session_id,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ModelName string `json:"model_name,omitempty"`
}

type Audit struct {
	ID              int64      `json:"id"`
	RepoID          string     `json:"repo_id"`
	Scope           string     `json:"scope"`
	ScopePaths      []string   `json:"scope_paths"`
	AuditType       string     `json:"audit_type"`
	Auditor         string     `json:"auditor"`
	AuditedAtRef    *string    `json:"audited_at_ref,omitempty"`
	SummaryMarkdown string     `json:"summary_markdown"`
	Verdict         *string    `json:"verdict,omitempty"`
	Status          string     `json:"status"`
	SupersedesID    *int64     `json:"supersedes_id,omitempty"`
	Provenance      Provenance `json:"provenance"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Finding struct {
	domain.Finding
	AuditID      *int64                 `json:"audit_id,omitempty"`
	BodyMarkdown string                 `json:"body_markdown"`
	AuditedAtRef *string                `json:"audited_at_ref,omitempty"`
	IsOutOfScope bool                   `json:"is_out_of_scope"`
	SymbolID     *int64                 `json:"symbol_id,omitempty"`
	Provenance   Provenance             `json:"provenance"`
	CodeRefs     []domain.CodeReference `json:"code_refs,omitempty"`
	Themes       []Theme                `json:"themes,omitempty"`
}

type Theme struct {
	ID          int64     `json:"id"`
	RepoID      *string   `json:"repo_id,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AuditBundle struct {
	Audit    *Audit    `json:"audit"`
	Findings []Finding `json:"findings"`
	Themes   []Theme   `json:"themes"`
}

type ListFilter struct {
	RepoID string
	Status string
}

type ImportBundle struct {
	Audit    *Audit
	Findings []Finding
}
