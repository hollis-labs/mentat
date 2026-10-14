package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/graph"
	"github.com/hollis-labs/mentat/internal/symbols"
	"github.com/spf13/cobra"
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Query symbol relationships",
}

var graphNeighborsCmd = &cobra.Command{
	Use:   "neighbors <id-or-qualified-name>",
	Short: "List neighboring symbols for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sym, err := resolveGraphSymbol(args[0], cmd)
		if err != nil {
			return err
		}
		depth, _ := cmd.Flags().GetInt("depth")
		kind, _ := cmd.Flags().GetString("kind")
		source, _ := cmd.Flags().GetString("source")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")

		items, err := graph.NewService(store).Neighbors(context.Background(), sym.ID, graph.Filter{
			RepoID: sym.RepoID,
			Kind:   kind,
			Source: source,
			Depth:  depth,
		})
		if err != nil {
			return err
		}
		items = limitNeighbors(items, limit)
		return renderGraphNeighbors(sym, items, format)
	},
}

var graphCoChangeCmd = &cobra.Command{
	Use:   "co-change <id-or-qualified-name>",
	Short: "List co-change neighbors for a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sym, err := resolveGraphSymbol(args[0], cmd)
		if err != nil {
			return err
		}
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")

		items, err := graph.NewService(store).Neighbors(context.Background(), sym.ID, graph.Filter{
			RepoID: sym.RepoID,
			Kind:   "co-changed-with",
			Source: "git-history",
			Depth:  depth,
		})
		if err != nil {
			return err
		}
		items = limitNeighbors(items, limit)
		return renderGraphNeighbors(sym, items, format)
	},
}

func init() {
	graphNeighborsCmd.Flags().String("repo", "", "repo ID when looking up by qualified name")
	graphNeighborsCmd.Flags().String("kind", "", "filter by relationship kind")
	graphNeighborsCmd.Flags().String("source", "", "filter by relationship source")
	graphNeighborsCmd.Flags().Int("depth", 1, "maximum traversal depth")
	graphNeighborsCmd.Flags().Int("limit", 20, "maximum number of neighbors to print")
	graphNeighborsCmd.Flags().String("format", "", "output format: json")

	graphCoChangeCmd.Flags().String("repo", "", "repo ID when looking up by qualified name")
	graphCoChangeCmd.Flags().Int("depth", 1, "maximum traversal depth")
	graphCoChangeCmd.Flags().Int("limit", 20, "maximum number of neighbors to print")
	graphCoChangeCmd.Flags().String("format", "", "output format: json")

	graphCmd.AddCommand(graphNeighborsCmd)
	graphCmd.AddCommand(graphCoChangeCmd)
}

func resolveGraphSymbol(target string, cmd *cobra.Command) (*symbols.Symbol, error) {
	repoID, _ := cmd.Flags().GetString("repo")
	if id, err := parseInt64(target); err == nil {
		sym, err := store.GetSymbol(id)
		if err != nil {
			return nil, err
		}
		if sym == nil {
			return nil, fmt.Errorf("symbol not found: %s", target)
		}
		return sym, nil
	}
	if repoID == "" {
		return nil, fmt.Errorf("--repo is required when looking up by qualified name")
	}
	sym, err := store.FindSymbolByQualifiedName(repoID, target)
	if err != nil {
		return nil, err
	}
	if sym == nil {
		return nil, fmt.Errorf("symbol not found: %s", target)
	}
	return sym, nil
}

func limitNeighbors(items []graph.Neighbor, limit int) []graph.Neighbor {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func renderGraphNeighbors(sym *symbols.Symbol, items []graph.Neighbor, format string) error {
	if format == "json" {
		payload := map[string]any{
			"symbol":    sym,
			"neighbors": items,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Printf("Symbol: %s (%d)\n", sym.QualifiedName, sym.ID)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "DEPTH\tKIND\tSOURCE\tDIR\tWEIGHT\tSYMBOL\tLOCATION\n")
	for _, item := range items {
		location := item.FilePath
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f\t%s\t%s\n", item.Depth, item.Kind, item.Source, item.Direction, item.Weight, item.QualifiedName, location)
	}
	w.Flush()
	fmt.Printf("\n%d neighbors\n", len(items))
	return nil
}
