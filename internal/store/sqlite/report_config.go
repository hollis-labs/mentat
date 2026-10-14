package sqlite

import (
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

func (s *Store) CreateReportConfig(rc *domain.ReportConfig) error {
	now := time.Now().UTC().Format(time.RFC3339)
	rc.CreatedAt, _ = time.Parse(time.RFC3339, now)

	_, err := s.db.Exec(`INSERT INTO report_configs (id, name, description, lens_id, audience, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		rc.ID, rc.Name, rc.Description, rc.LensID, rc.Audience, now)
	if err != nil {
		return fmt.Errorf("insert report config: %w", err)
	}
	return nil
}

func (s *Store) AddReportConfigFilter(configID, filterType, filterValue string) error {
	_, err := s.db.Exec(`INSERT INTO report_config_filters (config_id, filter_type, filter_value) VALUES (?, ?, ?)`,
		configID, filterType, filterValue)
	if err != nil {
		return fmt.Errorf("add report filter: %w", err)
	}
	return nil
}

func (s *Store) GetReportConfig(id string) (*domain.ReportConfig, error) {
	rc := &domain.ReportConfig{}
	var createdAt string
	err := s.db.QueryRow(`SELECT id, name, description, lens_id, audience, created_at FROM report_configs WHERE id = ?`, id).
		Scan(&rc.ID, &rc.Name, &rc.Description, &rc.LensID, &rc.Audience, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("get report config: %w", err)
	}
	rc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	rows, err := s.db.Query(`SELECT id, config_id, filter_type, filter_value FROM report_config_filters WHERE config_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("get report filters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var f domain.ReportConfigFilter
		if err := rows.Scan(&f.ID, &f.ConfigID, &f.FilterType, &f.FilterValue); err != nil {
			return nil, fmt.Errorf("scan report filter: %w", err)
		}
		rc.Filters = append(rc.Filters, f)
	}
	return rc, rows.Err()
}

func (s *Store) ListReportConfigs() ([]domain.ReportConfig, error) {
	rows, err := s.db.Query(`SELECT rc.id, rc.name, rc.description, rc.lens_id, rc.audience, rc.created_at,
		(SELECT COUNT(*) FROM report_config_filters f WHERE f.config_id = rc.id) as filter_count
		FROM report_configs rc ORDER BY rc.name`)
	if err != nil {
		return nil, fmt.Errorf("list report configs: %w", err)
	}
	defer rows.Close()

	var configs []domain.ReportConfig
	for rows.Next() {
		var rc domain.ReportConfig
		var createdAt string
		var filterCount int
		if err := rows.Scan(&rc.ID, &rc.Name, &rc.Description, &rc.LensID, &rc.Audience, &createdAt, &filterCount); err != nil {
			return nil, fmt.Errorf("scan report config: %w", err)
		}
		rc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		_ = filterCount
		configs = append(configs, rc)
	}
	return configs, rows.Err()
}

// ResolveReportRepos returns repo IDs matching the report's filters (union of all filters).
func (s *Store) ResolveReportRepos(configID string) ([]string, error) {
	filters, err := s.db.Query(`SELECT filter_type, filter_value FROM report_config_filters WHERE config_id = ?`, configID)
	if err != nil {
		return nil, fmt.Errorf("get filters: %w", err)
	}
	defer filters.Close()

	var conditions []string
	var args []any
	for filters.Next() {
		var fType, fValue string
		if err := filters.Scan(&fType, &fValue); err != nil {
			return nil, fmt.Errorf("scan filter: %w", err)
		}
		switch fType {
		case "category":
			conditions = append(conditions, "r.category = ?")
			args = append(args, fValue)
		case "tag":
			conditions = append(conditions, "r.id IN (SELECT repo_id FROM repo_tags WHERE tag_id = ?)")
			args = append(args, fValue)
		case "stack":
			conditions = append(conditions, "r.stack = ?")
			args = append(args, fValue)
		case "repo":
			conditions = append(conditions, "r.id = ?")
			args = append(args, fValue)
		case "is_own":
			conditions = append(conditions, "r.is_own = 1")
		}
	}

	if len(conditions) == 0 {
		return nil, fmt.Errorf("report has no filters")
	}

	query := "SELECT DISTINCT r.id FROM repos r WHERE " + strings.Join(conditions, " OR ") + " ORDER BY r.id"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve repos: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan repo id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) DeleteReportConfig(id string) error {
	_, err := s.db.Exec("DELETE FROM report_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete report config: %w", err)
	}
	return nil
}
