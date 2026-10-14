package domain

import "time"

type ReportConfig struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	LensID      string               `json:"lens_id"`
	Audience    string               `json:"audience"`
	CreatedAt   time.Time            `json:"created_at"`
	Filters     []ReportConfigFilter `json:"filters,omitempty"`
}

type ReportConfigFilter struct {
	ID          int64  `json:"id"`
	ConfigID    string `json:"config_id"`
	FilterType  string `json:"filter_type"` // "category", "tag", "stack", "repo", "is_own"
	FilterValue string `json:"filter_value"`
}
