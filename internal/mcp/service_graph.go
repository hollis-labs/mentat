package semcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/hollis-labs/mentat/internal/graph"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

type NeighborSummary struct {
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

type SymbolLookupOptions struct {
	IncludeNeighbors bool
	NeighborKind     string
	NeighborSource   string
	NeighborDepth    int
	NeighborLimit    int
}

type SymbolLookupResult struct {
	SymbolSummary
	Neighbors []NeighborSummary `json:"neighbors,omitempty"`
}

func (s *Service) GraphNeighbors(ctx context.Context, repoID, qualifiedName string, symbolID int64, kind, source string, depth, limit int) ([]NeighborSummary, error) {
	sym, err := s.resolveSymbol(repoID, qualifiedName, symbolID)
	if err != nil {
		return nil, err
	}
	if depth <= 0 {
		depth = 1
	}
	items, err := graph.NewService(s.store).Neighbors(ctx, sym.ID, graph.Filter{
		RepoID: sym.RepoID,
		Kind:   strings.TrimSpace(kind),
		Source: strings.TrimSpace(source),
		Depth:  depth,
	})
	if err != nil {
		return nil, err
	}
	out := summarizeNeighbors(items)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) SymbolLookupWithOptions(ctx context.Context, repoID, query, kind, language string, limit int, opts SymbolLookupOptions) ([]SymbolLookupResult, error) {
	items, err := s.SymbolLookup(repoID, query, kind, language, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SymbolLookupResult, 0, len(items))
	for _, item := range items {
		result := SymbolLookupResult{SymbolSummary: summarizeSymbol(item)}
		if opts.IncludeNeighbors {
			neighbors, err := s.GraphNeighbors(ctx, "", "", item.ID, opts.NeighborKind, opts.NeighborSource, opts.NeighborDepth, opts.NeighborLimit)
			if err != nil {
				return nil, err
			}
			result.Neighbors = neighbors
		}
		out = append(out, result)
	}
	return out, nil
}

func (s *Service) resolveSymbol(repoID, qualifiedName string, symbolID int64) (*model.Symbol, error) {
	switch {
	case symbolID > 0:
		sym, err := s.store.GetSymbol(symbolID)
		if err != nil {
			return nil, err
		}
		if sym == nil {
			return nil, fmt.Errorf("symbol not found")
		}
		return sym, nil
	case strings.TrimSpace(repoID) != "" && strings.TrimSpace(qualifiedName) != "":
		sym, err := s.store.FindSymbolByQualifiedName(strings.TrimSpace(repoID), strings.TrimSpace(qualifiedName))
		if err != nil {
			return nil, err
		}
		if sym == nil {
			return nil, fmt.Errorf("symbol not found")
		}
		return sym, nil
	default:
		return nil, fmt.Errorf("symbol_id or repo_id + qualified_name is required")
	}
}

func summarizeNeighbors(items []graph.Neighbor) []NeighborSummary {
	out := make([]NeighborSummary, 0, len(items))
	for _, item := range items {
		out = append(out, NeighborSummary{
			SymbolID:      item.SymbolID,
			RelatedFromID: item.RelatedFromID,
			RepoID:        item.RepoID,
			Kind:          item.Kind,
			Source:        item.Source,
			Direction:     item.Direction,
			Depth:         item.Depth,
			Weight:        item.Weight,
			Name:          item.Name,
			QualifiedName: item.QualifiedName,
			FilePath:      item.FilePath,
			Language:      item.Language,
		})
	}
	return out
}
