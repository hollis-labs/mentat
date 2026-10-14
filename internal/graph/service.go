package graph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

type Neighbor struct {
	SymbolID      int64   `json:"symbol_id"`
	RelatedFromID int64   `json:"related_from_id"`
	RepoID        string  `json:"repo_id"`
	Kind          string  `json:"kind"`
	Source        string  `json:"source"`
	Direction     string  `json:"direction"`
	Depth         int     `json:"depth"`
	Weight        float64 `json:"weight"`
	Name          string  `json:"name"`
	QualifiedName string  `json:"qualified_name"`
	FilePath      string  `json:"file_path"`
	Language      string  `json:"language"`
}

type Filter struct {
	RepoID string
	Kind   string
	Source string
	Depth  int
}

type Service struct {
	store *sqlite.Store
}

func NewService(store *sqlite.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Neighbors(ctx context.Context, symbolID int64, filter Filter) ([]Neighbor, error) {
	depth := filter.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth <= 3 {
		return s.neighborsCTE(ctx, symbolID, filter, depth)
	}
	return s.neighborsBFS(ctx, symbolID, filter, depth)
}

func (s *Service) neighborsCTE(ctx context.Context, symbolID int64, filter Filter, depth int) ([]Neighbor, error) {
	query := `
WITH RECURSIVE walk(symbol_id, related_from_id, repo_id, kind, source, direction, depth, weight, path) AS (
    SELECT
        CASE WHEN r.src_symbol_id = ? THEN r.dst_symbol_id ELSE r.src_symbol_id END AS symbol_id,
        ? AS related_from_id,
        r.repo_id,
        r.kind,
        r.source,
        CASE WHEN r.src_symbol_id = ? THEN 'out' ELSE 'in' END AS direction,
        1 AS depth,
        r.weight,
        ',' || CAST(? AS TEXT) || ',' || CAST(CASE WHEN r.src_symbol_id = ? THEN r.dst_symbol_id ELSE r.src_symbol_id END AS TEXT) || ',' AS path
    FROM relationships r
    WHERE (r.src_symbol_id = ? OR r.dst_symbol_id = ?)`
	args := []any{symbolID, symbolID, symbolID, symbolID, symbolID, symbolID, symbolID}
	if filter.RepoID != "" {
		query += ` AND r.repo_id = ?`
		args = append(args, filter.RepoID)
	}
	if filter.Kind != "" {
		query += ` AND r.kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.Source != "" {
		query += ` AND r.source = ?`
		args = append(args, filter.Source)
	}
	query += `
    UNION ALL
    SELECT
        CASE WHEN r.src_symbol_id = w.symbol_id THEN r.dst_symbol_id ELSE r.src_symbol_id END,
        w.symbol_id,
        r.repo_id,
        r.kind,
        r.source,
        CASE WHEN r.src_symbol_id = w.symbol_id THEN 'out' ELSE 'in' END,
        w.depth + 1,
        r.weight,
        w.path || CAST(CASE WHEN r.src_symbol_id = w.symbol_id THEN r.dst_symbol_id ELSE r.src_symbol_id END AS TEXT) || ','
    FROM walk w
    JOIN relationships r ON r.src_symbol_id = w.symbol_id OR r.dst_symbol_id = w.symbol_id
    WHERE w.depth < ?`
	args = append(args, depth)
	if filter.RepoID != "" {
		query += ` AND r.repo_id = ?`
		args = append(args, filter.RepoID)
	}
	if filter.Kind != "" {
		query += ` AND r.kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.Source != "" {
		query += ` AND r.source = ?`
		args = append(args, filter.Source)
	}
	query += `
      AND instr(w.path, ',' || CAST(CASE WHEN r.src_symbol_id = w.symbol_id THEN r.dst_symbol_id ELSE r.src_symbol_id END AS TEXT) || ',') = 0
)
SELECT
    w.symbol_id,
    w.related_from_id,
    w.repo_id,
    w.kind,
    w.source,
    w.direction,
    w.depth,
    w.weight,
    s.name,
    s.qualified_name,
    s.file_path,
    s.language
FROM walk w
JOIN symbols s ON s.id = w.symbol_id
ORDER BY w.depth, w.kind, s.qualified_name`

	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query graph neighbors: %w", err)
	}
	defer rows.Close()
	return scanNeighbors(rows)
}

func (s *Service) neighborsBFS(ctx context.Context, symbolID int64, filter Filter, depth int) ([]Neighbor, error) {
	query := `
SELECT
    CASE WHEN src_symbol_id = ? THEN dst_symbol_id ELSE src_symbol_id END AS symbol_id,
    CASE WHEN src_symbol_id = ? THEN src_symbol_id ELSE dst_symbol_id END AS related_from_id,
    r.repo_id,
    r.kind,
    r.source,
    CASE WHEN r.src_symbol_id = ? THEN 'out' ELSE 'in' END AS direction,
    r.weight,
    s.name,
    s.qualified_name,
    s.file_path,
    s.language
FROM relationships r
JOIN symbols s ON s.id = CASE WHEN r.src_symbol_id = ? THEN r.dst_symbol_id ELSE r.src_symbol_id END
WHERE (r.src_symbol_id = ? OR r.dst_symbol_id = ?)`

	baseArgs := []any{symbolID, symbolID, symbolID, symbolID, symbolID, symbolID}
	if filter.RepoID != "" {
		query += ` AND r.repo_id = ?`
		baseArgs = append(baseArgs, filter.RepoID)
	}
	if filter.Kind != "" {
		query += ` AND r.kind = ?`
		baseArgs = append(baseArgs, filter.Kind)
	}
	if filter.Source != "" {
		query += ` AND r.source = ?`
		baseArgs = append(baseArgs, filter.Source)
	}

	visited := map[int64]struct{}{symbolID: {}}
	type frontierNode struct {
		symbolID int64
		depth    int
	}
	queue := []frontierNode{{symbolID: symbolID, depth: 0}}
	out := make([]Neighbor, 0)

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node.depth >= depth {
			continue
		}

		args := append([]any(nil), baseArgs...)
		args[0] = node.symbolID
		args[1] = node.symbolID
		args[2] = node.symbolID
		args[3] = node.symbolID
		args[4] = node.symbolID
		args[5] = node.symbolID
		rows, err := s.store.DB().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query bfs neighbors: %w", err)
		}

		for rows.Next() {
			var item Neighbor
			if err := rows.Scan(&item.SymbolID, &item.RelatedFromID, &item.RepoID, &item.Kind, &item.Source, &item.Direction, &item.Weight, &item.Name, &item.QualifiedName, &item.FilePath, &item.Language); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan bfs neighbor: %w", err)
			}
			item.Depth = node.depth + 1
			if _, ok := visited[item.SymbolID]; ok {
				continue
			}
			visited[item.SymbolID] = struct{}{}
			out = append(out, item)
			queue = append(queue, frontierNode{symbolID: item.SymbolID, depth: item.Depth})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate bfs neighbors: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func scanNeighbors(rows *sql.Rows) ([]Neighbor, error) {
	out := make([]Neighbor, 0)
	for rows.Next() {
		var item Neighbor
		if err := rows.Scan(
			&item.SymbolID,
			&item.RelatedFromID,
			&item.RepoID,
			&item.Kind,
			&item.Source,
			&item.Direction,
			&item.Depth,
			&item.Weight,
			&item.Name,
			&item.QualifiedName,
			&item.FilePath,
			&item.Language,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func NormalizeKind(kind string) string {
	return strings.TrimSpace(strings.ToLower(kind))
}
