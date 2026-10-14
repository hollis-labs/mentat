package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Run hybrid search across findings, symbols, and code references",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		kind, _ := cmd.Flags().GetString("kind")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")

		service := retrieval.NewService(store)
		results, err := service.Search(context.Background(), retrieval.SearchOptions{
			Query:  args[0],
			RepoID: repoID,
			Kind:   kind,
			Limit:  limit,
		})
		if err != nil {
			return err
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tREPO\tTITLE\tLOCATION\tSCORE")
		for _, result := range results {
			location := result.Path
			if result.LineStart != nil {
				location = fmt.Sprintf("%s:%d", result.Path, *result.LineStart)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.4f\n", result.Kind, result.RepoID, result.Title, location, result.Score)
		}
		w.Flush()
		fmt.Printf("\n%d results\n", len(results))
		return nil
	},
}

func init() {
	searchCmd.Flags().String("repo", "", "filter by repo ID")
	searchCmd.Flags().String("kind", "", "filter by kind: finding, symbol, code-ref")
	searchCmd.Flags().Int("limit", 10, "maximum number of results")
	searchCmd.Flags().String("format", "", "output format: json")
	rootCmd.AddCommand(searchCmd)
}
