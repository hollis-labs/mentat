package domain

import "time"

// Scan represents a Hadron blueprint execution against a repo.
type Scan struct {
	ID           int64      `json:"id"`
	RepoID       string     `json:"repo_id"`
	Blueprint    string     `json:"blueprint"` // "se-repo-scan", "se-security-scan", "se-feature-audit"
	Status       string     `json:"status"`    // "pending", "running", "completed", "failed"
	ResultJSON   string     `json:"result_json"`
	ErrorMessage string     `json:"error_message"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
