package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
)

// startScanWorker launches a background goroutine that polls for pending scans
// and processes them by fetching repo stats from the GitHub API.
func (s *Server) startScanWorker() {
	go func() {
		for {
			s.processPendingScans()
			time.Sleep(5 * time.Second)
		}
	}()
	log.Println("Scan worker started")
}

func (s *Server) processPendingScans() {
	rows, err := s.store.DB().Query(
		`SELECT id, repo_id, blueprint FROM scans WHERE status = 'pending' ORDER BY created_at ASC LIMIT 5`,
	)
	if err != nil {
		log.Printf("scan worker: query pending: %v", err)
		return
	}
	defer rows.Close()

	type pendingScan struct {
		id        int64
		repoID    string
		blueprint string
	}
	var pending []pendingScan
	for rows.Next() {
		var ps pendingScan
		if err := rows.Scan(&ps.id, &ps.repoID, &ps.blueprint); err != nil {
			log.Printf("scan worker: scan row: %v", err)
			continue
		}
		pending = append(pending, ps)
	}
	if err := rows.Err(); err != nil {
		log.Printf("scan worker: rows err: %v", err)
	}

	for _, ps := range pending {
		s.runScan(ps.id, ps.repoID, ps.blueprint)
	}
}

func (s *Server) runScan(scanID int64, repoID, blueprint string) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Transition to running
	_, err := s.store.DB().Exec(
		`UPDATE scans SET status = 'running', started_at = ?, updated_at = ? WHERE id = ?`,
		now, now, scanID,
	)
	if err != nil {
		log.Printf("scan worker: transition running (scan %d): %v", scanID, err)
		return
	}

	// Fetch repo URL
	var repoURL, repoName string
	err = s.store.DB().QueryRow(`SELECT url, name FROM repos WHERE id = ?`, repoID).Scan(&repoURL, &repoName)
	if err != nil {
		s.failScan(scanID, fmt.Sprintf("repo lookup failed: %v", err))
		return
	}

	// Fetch GitHub stats
	stats, err := fetchGitHubStats(repoURL)
	if err != nil {
		s.failScan(scanID, fmt.Sprintf("github fetch failed: %v", err))
		return
	}

	// Create snapshot
	snap := &domain.Snapshot{
		RepoID:     repoID,
		Stars:      stats.Stars,
		Forks:      stats.Forks,
		OpenIssues: stats.OpenIssues,
	}
	if stats.LastPush != nil {
		snap.LastCommitAt = stats.LastPush
	}
	if stats.RawJSON != "" {
		snap.RawJSON = stats.RawJSON
	}
	if err := s.store.CreateSnapshot(snap); err != nil {
		s.failScan(scanID, fmt.Sprintf("snapshot create failed: %v", err))
		return
	}

	// Transition to completed
	resultJSON, _ := json.Marshal(map[string]interface{}{
		"snapshot_id": snap.ID,
		"repo_name":   repoName,
		"stars":       stats.Stars,
		"forks":       stats.Forks,
		"open_issues": stats.OpenIssues,
	})
	finished := time.Now().UTC().Format(time.RFC3339)
	_, err = s.store.DB().Exec(
		`UPDATE scans SET status = 'completed', result_json = ?, finished_at = ?, updated_at = ? WHERE id = ?`,
		string(resultJSON), finished, finished, scanID,
	)
	if err != nil {
		log.Printf("scan worker: transition completed (scan %d): %v", scanID, err)
		return
	}
	log.Printf("scan worker: completed scan %d for %s", scanID, repoName)
}

func (s *Server) failScan(scanID int64, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().Exec(
		`UPDATE scans SET status = 'failed', error_message = ?, finished_at = ?, updated_at = ? WHERE id = ?`,
		errMsg, now, now, scanID,
	)
	if err != nil {
		log.Printf("scan worker: transition failed (scan %d): %v", scanID, err)
	}
	log.Printf("scan worker: failed scan %d: %s", scanID, errMsg)
}

// gitHubStats holds data fetched from the GitHub API.
type gitHubStats struct {
	Stars      *int
	Forks      *int
	OpenIssues *int
	LastPush   *time.Time
	RawJSON    string
}

// fetchGitHubStats extracts owner/repo from a GitHub URL and calls the API.
func fetchGitHubStats(repoURL string) (*gitHubStats, error) {
	owner, repo := parseGitHubURL(repoURL)
	if owner == "" || repo == "" {
		// Not a GitHub URL — return empty stats (not an error, just no data)
		return &gitHubStats{}, nil
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "stack-explorer/1.0")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github API %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var gh struct {
		Stars      int        `json:"stargazers_count"`
		Forks      int        `json:"forks_count"`
		OpenIssues int        `json:"open_issues_count"`
		PushedAt   *time.Time `json:"pushed_at"`
	}
	if err := json.Unmarshal(body, &gh); err != nil {
		return nil, fmt.Errorf("parse github response: %w", err)
	}

	return &gitHubStats{
		Stars:      intPtr(gh.Stars),
		Forks:      intPtr(gh.Forks),
		OpenIssues: intPtr(gh.OpenIssues),
		LastPush:   gh.PushedAt,
		RawJSON:    string(body),
	}, nil
}

// parseGitHubURL extracts owner and repo from URLs like:
// https://github.com/owner/repo or https://github.com/owner/repo.git
func parseGitHubURL(rawURL string) (string, string) {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimSuffix(rawURL, ".git")
	rawURL = strings.TrimSuffix(rawURL, "/")

	// Try github.com path
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(rawURL, prefix) {
			path := rawURL[len(prefix):]
			parts := strings.SplitN(path, "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1]
			}
		}
	}
	return "", ""
}

func intPtr(v int) *int { return &v }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
