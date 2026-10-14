package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var scorecardCmd = &cobra.Command{
	Use:   "scorecard",
	Short: "Generate scorecard reports",
}

var scorecardGenerateCmd = &cobra.Command{
	Use:   "generate <repo-id>",
	Short: "Generate a markdown scorecard for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID := args[0]
		output, _ := cmd.Flags().GetString("output")

		repo, err := store.GetRepo(repoID)
		if err != nil || repo == nil {
			return fmt.Errorf("repo not found: %s", repoID)
		}

		sc, err := store.GetLatestScorecard(repoID)
		if err != nil {
			return fmt.Errorf("no scorecard for %s", repoID)
		}

		dims, _ := store.ListDimensions()
		dimMap := map[string]string{}
		for _, d := range dims {
			dimMap[d.ID] = d.Name
		}

		md := fmt.Sprintf("# Scorecard: %s\n\n", repo.Name)
		md += fmt.Sprintf("**Category:** %s | **Stack:** %s | **Overall: %.1f/10**\n\n", repo.Category, repo.Stack, sc.Overall)
		md += fmt.Sprintf("> %s\n\n", repo.Description)
		md += "| Dimension | Score | Evidence |\n"
		md += "|-----------|-------|----------|\n"
		for _, ds := range sc.Scores {
			name := dimMap[ds.DimensionID]
			if name == "" {
				name = ds.DimensionID
			}
			md += fmt.Sprintf("| %s | %.1f | %s |\n", name, ds.Score, ds.Evidence)
		}
		md += fmt.Sprintf("\n*Generated %s*\n", time.Now().Format("2006-01-02"))

		if output != "" {
			path := filepath.Join(output, fmt.Sprintf("scorecard-%s.md", repoID))
			if err := os.WriteFile(path, []byte(md), 0644); err != nil {
				return fmt.Errorf("write scorecard: %w", err)
			}
			fmt.Printf("Scorecard written to %s\n", path)
		} else {
			fmt.Print(md)
		}
		return nil
	},
}

func init() {
	scorecardGenerateCmd.Flags().String("output", "", "output directory (prints to stdout if empty)")

	scorecardCmd.AddCommand(scorecardGenerateCmd)
}
