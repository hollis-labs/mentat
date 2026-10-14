package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"gopkg.in/yaml.v3"
)

type EvalQuery struct {
	Name         string   `yaml:"name" json:"name"`
	Query        string   `yaml:"query" json:"query"`
	RepoID       string   `yaml:"repo_id,omitempty" json:"repo_id,omitempty"`
	Kind         string   `yaml:"kind,omitempty" json:"kind,omitempty"`
	ExpectedRefs []string `yaml:"expected_refs" json:"expected_refs"`
}

type EvalSuite struct {
	Queries []EvalQuery `yaml:"queries" json:"queries"`
}

type EvalCaseResult struct {
	Name     string   `json:"name"`
	Query    string   `json:"query"`
	Hit      bool     `json:"hit"`
	TopRefs  []string `json:"top_refs"`
	Expected []string `json:"expected"`
}

type EvalReport struct {
	RunAt      string           `json:"run_at"`
	QueryFile  string           `json:"query_file"`
	RecallAt10 float64          `json:"recall_at_10"`
	Total      int              `json:"total"`
	Hits       int              `json:"hits"`
	Cases      []EvalCaseResult `json:"cases"`
}

func RunEval(ctx context.Context, store *sqlite.Store, queryFile, baselineFile string) (*EvalReport, error) {
	data, err := os.ReadFile(queryFile)
	if err != nil {
		return nil, fmt.Errorf("read eval queries: %w", err)
	}
	var suite EvalSuite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("parse eval queries: %w", err)
	}

	service := NewService(store)
	report := &EvalReport{
		RunAt:     time.Now().UTC().Format(time.RFC3339),
		QueryFile: queryFile,
		Total:     len(suite.Queries),
	}
	for _, query := range suite.Queries {
		results, err := service.Search(ctx, SearchOptions{
			Query:  query.Query,
			RepoID: query.RepoID,
			Kind:   query.Kind,
			Limit:  10,
		})
		if err != nil {
			return nil, err
		}

		topRefs := make([]string, 0, len(results))
		hit := false
		expected := make(map[string]struct{}, len(query.ExpectedRefs))
		for _, ref := range query.ExpectedRefs {
			expected[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, result := range results {
			topRefs = append(topRefs, result.StableRef)
			if _, ok := expected[result.StableRef]; ok {
				hit = true
			}
		}
		if hit {
			report.Hits++
		}
		report.Cases = append(report.Cases, EvalCaseResult{
			Name:     query.Name,
			Query:    query.Query,
			Hit:      hit,
			TopRefs:  topRefs,
			Expected: query.ExpectedRefs,
		})
	}
	if report.Total > 0 {
		report.RecallAt10 = float64(report.Hits) / float64(report.Total)
	}

	if baselineFile != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal eval baseline: %w", err)
		}
		if err := os.WriteFile(baselineFile, encoded, 0o644); err != nil {
			return nil, fmt.Errorf("write eval baseline: %w", err)
		}
	}
	return report, nil
}
