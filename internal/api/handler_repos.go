package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/domain"
)

// repoResponse is the JSON shape the frontend expects.
type repoResponse struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	URL              string  `json:"url"`
	Description      string  `json:"description"`
	Stack            string  `json:"stack"`
	Category         string  `json:"category"`
	IsOwn            bool    `json:"is_own"`
	LocalPath        string  `json:"local_path"`
	EmbeddingProfile string  `json:"embedding_profile"`
	Score            float64 `json:"score"`
	LoC              int     `json:"loc"`
	Tags             string  `json:"tags"`
}

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "name", 25)

	sortMap := map[string]string{
		"name":     "r.name",
		"category": "r.category",
		"stack":    "r.stack",
		"score":    "latest_score",
		"loc":      "latest_loc",
	}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "r.name"
	}

	// Build query with JOINs for computed fields
	baseQuery := `FROM repos r
		LEFT JOIN (
			SELECT repo_id, overall as latest_score
			FROM scorecards
			WHERE id IN (SELECT MAX(id) FROM scorecards GROUP BY repo_id)
		) sc ON sc.repo_id = r.id
		LEFT JOIN (
			SELECT repo_id, loc as latest_loc
			FROM snapshots
			WHERE id IN (SELECT MAX(id) FROM snapshots GROUP BY repo_id)
		) sn ON sn.repo_id = r.id`

	var conditions []string
	var args []any

	// Filters
	if v, ok := p.Filters["stack"]; ok {
		conditions = append(conditions, "r.stack = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["category"]; ok {
		conditions = append(conditions, "r.category = ?")
		args = append(args, v)
	}
	if v, ok := p.Filters["is_own"]; ok {
		if v == "true" || v == "1" {
			conditions = append(conditions, "r.is_own = 1")
		} else {
			conditions = append(conditions, "r.is_own = 0")
		}
	}

	// Search
	if p.Search != "" {
		search := "%" + p.Search + "%"
		conditions = append(conditions, `(r.name LIKE ? OR r.description LIKE ? OR r.id IN (
			SELECT rt.repo_id FROM repo_tags rt JOIN tags t ON t.id = rt.tag_id WHERE t.name LIKE ?
		))`)
		args = append(args, search, search, search)
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	// Count
	var total int
	countQuery := "SELECT COUNT(DISTINCT r.id) " + baseQuery + where
	if err := s.store.DB().QueryRow(countQuery, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Fetch page
	selectQuery := fmt.Sprintf(`SELECT r.id, r.name, r.url, r.description, r.stack, r.category, r.is_own, r.local_path,
		r.embedding_profile,
		COALESCE(sc.latest_score, 0) as latest_score,
		COALESCE(sn.latest_loc, 0) as latest_loc
		%s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)

	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(selectQuery, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	repos := make([]repoResponse, 0)
	for rows.Next() {
		var rr repoResponse
		var isOwn int
		if err := rows.Scan(&rr.ID, &rr.Name, &rr.URL, &rr.Description, &rr.Stack, &rr.Category,
			&isOwn, &rr.LocalPath, &rr.EmbeddingProfile, &rr.Score, &rr.LoC); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rr.IsOwn = isOwn == 1
		repos = append(repos, rr)
	}

	// Fetch tags for each repo
	for i := range repos {
		tags, err := s.store.ListRepoTags(repos[i].ID)
		if err != nil {
			continue
		}
		names := make([]string, len(tags))
		for j, t := range tags {
			names[j] = t.Name
		}
		repos[i].Tags = strings.Join(names, ",")
	}

	writeList(w, repos, total, p.Page, p.PageSize)
}

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	repo, err := s.store.GetRepo(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repo == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	rr := repoResponse{
		ID: repo.ID, Name: repo.Name, URL: repo.URL, Description: repo.Description,
		Stack: repo.Stack, Category: repo.Category, IsOwn: repo.IsOwn, LocalPath: repo.LocalPath, EmbeddingProfile: repo.EmbeddingProfile,
	}

	// Score from latest scorecard
	sc, err := s.store.GetLatestScorecard(repo.ID)
	if err == nil && sc != nil {
		rr.Score = sc.Overall
	}

	// LoC from latest snapshot
	snaps, err := s.store.ListSnapshots(repo.ID, 1)
	if err == nil && len(snaps) > 0 && snaps[0].LoC != nil {
		rr.LoC = *snaps[0].LoC
	}

	// Tags
	tags, _ := s.store.ListRepoTags(repo.ID)
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	rr.Tags = strings.Join(names, ",")

	writeItem(w, rr)
}

func (s *Server) createRepo(w http.ResponseWriter, r *http.Request) {
	var repo domain.Repo
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if repo.ID == "" || repo.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	if err := s.store.CreateRepo(&repo); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, repoResponse{
		ID: repo.ID, Name: repo.Name, URL: repo.URL, Description: repo.Description,
		Stack: repo.Stack, Category: repo.Category, IsOwn: repo.IsOwn, LocalPath: repo.LocalPath, EmbeddingProfile: repo.EmbeddingProfile,
	})
}

func (s *Server) updateRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.store.GetRepo(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	if err := json.NewDecoder(r.Body).Decode(existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	existing.ID = id // prevent ID change
	if err := s.store.UpdateRepo(existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rr := repoResponse{
		ID: existing.ID, Name: existing.Name, URL: existing.URL, Description: existing.Description,
		Stack: existing.Stack, Category: existing.Category, IsOwn: existing.IsOwn, LocalPath: existing.LocalPath, EmbeddingProfile: existing.EmbeddingProfile,
	}
	writeItem(w, rr)
}

type addRepoTagsRequest struct {
	Tags []string `json:"tags"`
}

func (s *Server) addRepoTags(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	repo, err := s.store.GetRepo(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repo == nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}

	var req addRepoTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Tags) == 0 {
		writeError(w, http.StatusBadRequest, "tags are required")
		return
	}
	for _, tag := range req.Tags {
		name := strings.TrimSpace(tag)
		if name == "" {
			continue
		}
		tagID := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		if err := s.store.EnsureTag(tagID, name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.store.AddRepoTag(id, tagID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	tags, err := s.store.ListRepoTags(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, len(tags))
	for i, item := range tags {
		names[i] = item.Name
	}
	writeItem(w, map[string]any{
		"repo_id": id,
		"tags":    names,
	})
}

func (s *Server) deleteRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteRepo(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Tags ---

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListAllTags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tags == nil {
		tags = []domain.Tag{}
	}
	writeList(w, tags, len(tags), 1, len(tags))
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var t domain.Tag
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if t.ID == "" || t.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	if err := s.store.EnsureTag(t.ID, t.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, t)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := s.store.DB().Exec("DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Snapshots ---

type snapshotResponse struct {
	ID            string   `json:"id"`
	RepoID        string   `json:"repo_id"`
	CapturedAt    string   `json:"captured_at"`
	Stars         *int     `json:"stars"`
	Forks         *int     `json:"forks"`
	LoC           *int     `json:"loc"`
	Files         *int     `json:"files"`
	Contributors  *int     `json:"contributors"`
	Commits30d    *int     `json:"commits_30d"`
	ComplexityAvg *float64 `json:"complexity_avg"`
}

func snapToResponse(snap domain.Snapshot) snapshotResponse {
	return snapshotResponse{
		ID: fmt.Sprintf("%d", snap.ID), RepoID: snap.RepoID,
		CapturedAt: snap.CapturedAt.Format("2006-01-02T15:04:05Z"),
		Stars:      snap.Stars, Forks: snap.Forks, LoC: snap.LoC, Files: snap.Files,
		Contributors: snap.Contributors, Commits30d: snap.Commits30d, ComplexityAvg: snap.ComplexityAvg,
	}
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	p := parseListParams(r, "captured_at", 25)
	repoID := p.Filters["repo_id"]

	// Use raw query for pagination support
	baseQuery := "FROM snapshots"
	var conditions []string
	var args []any
	if repoID != "" {
		conditions = append(conditions, "repo_id = ?")
		args = append(args, repoID)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.store.DB().QueryRow("SELECT COUNT(*) "+baseQuery+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sortMap := map[string]string{"captured_at": "captured_at", "id": "id"}
	sortCol := allowedSort(p.Sort, sortMap)
	if sortCol == "" {
		sortCol = "captured_at"
		p.Direction = "DESC"
	}

	query := fmt.Sprintf(`SELECT id, repo_id, captured_at, stars, forks, loc, files, contributors, commits_30d, complexity_avg %s%s ORDER BY %s %s LIMIT ? OFFSET ?`,
		baseQuery, where, sortCol, p.Direction)
	pageArgs := append(args, p.PageSize, p.offset())
	rows, err := s.store.DB().Query(query, pageArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := make([]snapshotResponse, 0)
	for rows.Next() {
		var snap snapshotResponse
		var id int64
		var capturedAt string
		if err := rows.Scan(&id, &snap.RepoID, &capturedAt, &snap.Stars, &snap.Forks,
			&snap.LoC, &snap.Files, &snap.Contributors, &snap.Commits30d, &snap.ComplexityAvg); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		snap.ID = fmt.Sprintf("%d", id)
		snap.CapturedAt = capturedAt
		results = append(results, snap)
	}
	writeList(w, results, total, p.Page, p.PageSize)
}

func (s *Server) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var snap domain.Snapshot
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if snap.RepoID == "" {
		writeError(w, http.StatusBadRequest, "repo_id is required")
		return
	}
	if err := s.store.CreateSnapshot(&snap); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, snapToResponse(snap))
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var snap snapshotResponse
	var dbID int64
	var capturedAt string
	err := s.store.DB().QueryRow(`SELECT id, repo_id, captured_at, stars, forks, loc, files, contributors, commits_30d, complexity_avg FROM snapshots WHERE id = ?`, id).
		Scan(&dbID, &snap.RepoID, &capturedAt, &snap.Stars, &snap.Forks, &snap.LoC, &snap.Files, &snap.Contributors, &snap.Commits30d, &snap.ComplexityAvg)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	snap.ID = fmt.Sprintf("%d", dbID)
	snap.CapturedAt = capturedAt
	writeItem(w, snap)
}
