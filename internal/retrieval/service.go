package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

type SearchOptions struct {
	Query  string
	RepoID string
	Kind   string
	Limit  int
}

type Result struct {
	Kind          string   `json:"kind"`
	ID            int64    `json:"id"`
	RepoID        string   `json:"repo_id"`
	Title         string   `json:"title"`
	Path          string   `json:"path,omitempty"`
	LineStart     *int     `json:"line_start,omitempty"`
	LineEnd       *int     `json:"line_end,omitempty"`
	Snippet       string   `json:"snippet,omitempty"`
	Score         float64  `json:"score"`
	LexicalRank   int      `json:"lexical_rank,omitempty"`
	SemanticRank  int      `json:"semantic_rank,omitempty"`
	SemanticScore float64  `json:"semantic_score,omitempty"`
	StableRef     string   `json:"stable_ref"`
	Models        []string `json:"models,omitempty"`
}

type Service struct {
	store    *sqlite.Store
	embedder *embed.Manager
}

func NewService(store *sqlite.Store) *Service {
	return &Service{
		store:    store,
		embedder: embed.NewManager(),
	}
}

func (s *Service) Search(ctx context.Context, opts SearchOptions) ([]Result, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	lexical, err := s.lexicalSearch(opts)
	if err != nil {
		return nil, err
	}
	semantic, err := s.semanticSearch(ctx, opts)
	if err != nil {
		return nil, err
	}

	return fuseResults(lexical, semantic, opts.Limit), nil
}

func (s *Service) lexicalSearch(opts SearchOptions) ([]Result, error) {
	limit := max(opts.Limit*3, 20)
	var results []Result
	if opts.Kind == "" || opts.Kind == "finding" {
		items, err := s.searchFindingsFTS(opts.Query, opts.RepoID, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	if opts.Kind == "" || opts.Kind == "symbol" {
		items, err := s.searchSymbolsFTS(opts.Query, opts.RepoID, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	if opts.Kind == "" || opts.Kind == "code-ref" {
		items, err := s.searchCodeRefsFTS(opts.Query, opts.RepoID, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	for i := range results {
		results[i].LexicalRank = i + 1
	}
	return results, nil
}

func (s *Service) semanticSearch(ctx context.Context, opts SearchOptions) ([]Result, error) {
	candidates, err := s.embeddingCandidates(opts)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	type queryVector struct {
		model  string
		vector []float32
	}
	queryVectors := map[string]queryVector{}
	for _, candidate := range candidates {
		if _, ok := queryVectors[candidate.Model]; ok {
			continue
		}
		cfg, err := s.embedder.ResolveModel(candidate.Model)
		if err != nil {
			continue
		}
		vec, err := s.embedder.Embed(ctx, cfg.Provider, candidate.Model, opts.Query)
		if err != nil {
			return nil, err
		}
		queryVectors[candidate.Model] = queryVector{model: candidate.Model, vector: vec}
	}

	if len(queryVectors) == 0 {
		return nil, nil
	}

	var results []Result
	for _, candidate := range candidates {
		qv, ok := queryVectors[candidate.Model]
		if !ok {
			continue
		}
		score := cosine(qv.vector, candidate.Vector)
		if score <= 0 {
			continue
		}
		candidate.Result.SemanticScore = score
		candidate.Result.Models = []string{candidate.Model}
		results = append(results, candidate.Result)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].SemanticScore > results[j].SemanticScore })
	if len(results) > opts.Limit*5 {
		results = results[:opts.Limit*5]
	}
	for i := range results {
		results[i].SemanticRank = i + 1
	}
	return results, nil
}

type embeddingCandidate struct {
	Model  string
	Vector []float32
	Result Result
}

func (s *Service) embeddingCandidates(opts SearchOptions) ([]embeddingCandidate, error) {
	query := `SELECT e.target_kind, e.target_id, e.model, e.vector,
COALESCE(sym.repo_id, f.repo_id, cr.repo_id, '') AS repo_id,
COALESCE(sym.qualified_name, f.title, cr.file_path, '') AS title,
COALESCE(sym.file_path, cr.file_path, '') AS path,
sym.line_start, sym.line_end,
COALESCE(sym.docstring, f.description, cr.description, '') AS snippet
FROM embeddings e
LEFT JOIN symbols sym ON e.target_kind = 'symbol' AND sym.id = e.target_id
LEFT JOIN findings f ON e.target_kind = 'finding' AND f.id = e.target_id
LEFT JOIN code_references cr ON e.target_kind = 'code-ref' AND cr.id = e.target_id
WHERE 1 = 1`
	var args []any
	if opts.RepoID != "" {
		query += ` AND COALESCE(sym.repo_id, f.repo_id, cr.repo_id, '') = ?`
		args = append(args, opts.RepoID)
	}
	if opts.Kind != "" {
		query += ` AND e.target_kind = ?`
		args = append(args, opts.Kind)
	}

	rows, err := s.store.DB().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	var out []embeddingCandidate
	for rows.Next() {
		var (
			kind       string
			id         int64
			model      string
			vectorBlob []byte
			repoID     string
			title      string
			path       string
			lineStart  sql.NullInt64
			lineEnd    sql.NullInt64
			snippet    string
		)
		if err := rows.Scan(&kind, &id, &model, &vectorBlob, &repoID, &title, &path, &lineStart, &lineEnd, &snippet); err != nil {
			return nil, fmt.Errorf("scan embedding candidate: %w", err)
		}
		result := Result{
			Kind:      kind,
			ID:        id,
			RepoID:    repoID,
			Title:     title,
			Path:      path,
			Snippet:   snippet,
			StableRef: stableRef(kind, repoID, title, path),
		}
		if lineStart.Valid {
			v := int(lineStart.Int64)
			result.LineStart = &v
		}
		if lineEnd.Valid {
			v := int(lineEnd.Int64)
			result.LineEnd = &v
		}
		out = append(out, embeddingCandidate{
			Model:  model,
			Vector: blobToFloat32(vectorBlob),
			Result: result,
		})
	}
	return out, rows.Err()
}

func (s *Service) searchFindingsFTS(query, repoID string, limit int) ([]Result, error) {
	sqlText := `SELECT f.id, COALESCE(f.repo_id, ''), f.title, f.description, -bm25(findings_fts) AS score
FROM findings_fts
JOIN findings f ON f.id = findings_fts.rowid
WHERE findings_fts MATCH ?`
	args := []any{query}
	if repoID != "" {
		sqlText += ` AND f.repo_id = ?`
		args = append(args, repoID)
	}
	sqlText += ` ORDER BY bm25(findings_fts) LIMIT ?`
	args = append(args, limit)

	rows, err := s.store.DB().Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("search findings fts: %w", err)
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var item Result
		if err := rows.Scan(&item.ID, &item.RepoID, &item.Title, &item.Snippet, &item.Score); err != nil {
			return nil, fmt.Errorf("scan finding search result: %w", err)
		}
		item.Kind = "finding"
		item.StableRef = stableRef(item.Kind, item.RepoID, item.Title, "")
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) searchSymbolsFTS(query, repoID string, limit int) ([]Result, error) {
	sqlText := `SELECT sym.id, sym.repo_id, sym.qualified_name, sym.file_path, sym.line_start, sym.line_end, sym.docstring, -bm25(symbols_fts) AS score
FROM symbols_fts
JOIN symbols sym ON sym.id = symbols_fts.rowid
WHERE symbols_fts MATCH ?`
	args := []any{query}
	if repoID != "" {
		sqlText += ` AND sym.repo_id = ?`
		args = append(args, repoID)
	}
	sqlText += ` ORDER BY bm25(symbols_fts) LIMIT ?`
	args = append(args, limit)

	rows, err := s.store.DB().Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("search symbols fts: %w", err)
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var item Result
		var lineStart, lineEnd sql.NullInt64
		if err := rows.Scan(&item.ID, &item.RepoID, &item.Title, &item.Path, &lineStart, &lineEnd, &item.Snippet, &item.Score); err != nil {
			return nil, fmt.Errorf("scan symbol search result: %w", err)
		}
		item.Kind = "symbol"
		if lineStart.Valid {
			v := int(lineStart.Int64)
			item.LineStart = &v
		}
		if lineEnd.Valid {
			v := int(lineEnd.Int64)
			item.LineEnd = &v
		}
		item.StableRef = stableRef(item.Kind, item.RepoID, item.Title, item.Path)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) searchCodeRefsFTS(query, repoID string, limit int) ([]Result, error) {
	sqlText := `SELECT cr.id, cr.repo_id, cr.file_path, cr.description, cr.line_start, cr.line_end, -bm25(code_references_fts) AS score
FROM code_references_fts
JOIN code_references cr ON cr.id = code_references_fts.rowid
WHERE code_references_fts MATCH ?`
	args := []any{query}
	if repoID != "" {
		sqlText += ` AND cr.repo_id = ?`
		args = append(args, repoID)
	}
	sqlText += ` ORDER BY bm25(code_references_fts) LIMIT ?`
	args = append(args, limit)

	rows, err := s.store.DB().Query(sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("search code refs fts: %w", err)
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var item Result
		var lineStart, lineEnd sql.NullInt64
		if err := rows.Scan(&item.ID, &item.RepoID, &item.Title, &item.Snippet, &lineStart, &lineEnd, &item.Score); err != nil {
			return nil, fmt.Errorf("scan code ref search result: %w", err)
		}
		item.Kind = "code-ref"
		item.Path = item.Title
		if lineStart.Valid {
			v := int(lineStart.Int64)
			item.LineStart = &v
		}
		if lineEnd.Valid {
			v := int(lineEnd.Int64)
			item.LineEnd = &v
		}
		item.StableRef = stableRef(item.Kind, item.RepoID, "", item.Path)
		out = append(out, item)
	}
	return out, rows.Err()
}

func fuseResults(lexical, semantic []Result, limit int) []Result {
	merged := make(map[string]Result)
	for _, item := range lexical {
		key := item.StableRef
		current := merged[key]
		current = preferResult(current, item)
		current.Score += 1.0 / float64(60+item.LexicalRank)
		merged[key] = current
	}
	for _, item := range semantic {
		key := item.StableRef
		current := merged[key]
		current = preferResult(current, item)
		current.Score += 1.0 / float64(60+item.SemanticRank)
		if len(current.Models) == 0 {
			current.Models = item.Models
		}
		merged[key] = current
	}

	out := make([]Result, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].StableRef < out[j].StableRef
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func preferResult(current, next Result) Result {
	if current.StableRef == "" {
		return next
	}
	if current.Snippet == "" {
		current.Snippet = next.Snippet
	}
	if current.Path == "" {
		current.Path = next.Path
	}
	if current.Title == "" {
		current.Title = next.Title
	}
	if current.LineStart == nil {
		current.LineStart = next.LineStart
	}
	if current.LineEnd == nil {
		current.LineEnd = next.LineEnd
	}
	if current.Kind == "" {
		current.Kind = next.Kind
	}
	if current.ID == 0 {
		current.ID = next.ID
	}
	if current.RepoID == "" {
		current.RepoID = next.RepoID
	}
	if current.LexicalRank == 0 {
		current.LexicalRank = next.LexicalRank
	}
	if current.SemanticRank == 0 {
		current.SemanticRank = next.SemanticRank
	}
	if current.SemanticScore == 0 {
		current.SemanticScore = next.SemanticScore
	}
	if len(current.Models) == 0 {
		current.Models = next.Models
	}
	return current
}

func stableRef(kind, repoID, title, path string) string {
	switch kind {
	case "symbol":
		return fmt.Sprintf("symbol:%s:%s", repoID, title)
	case "finding":
		return fmt.Sprintf("finding:%s:%s", repoID, title)
	case "code-ref":
		return fmt.Sprintf("code-ref:%s:%s", repoID, path)
	default:
		return fmt.Sprintf("%s:%s:%s", kind, repoID, title)
	}
}

func blobToFloat32(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
