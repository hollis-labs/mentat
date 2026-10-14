package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations",
}

var dbStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		tables := []string{"repos", "tags", "repo_tags", "snapshots", "review_dimensions",
			"scorecards", "dimension_scores", "architecture_patterns", "repo_patterns",
			"findings", "audits", "audit_themes", "finding_themes", "symbols", "embeddings", "code_references", "comparison_sets", "comparison_set_repos"}

		db := store.DB()
		for _, table := range tables {
			var count int
			err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
			if err != nil {
				fmt.Printf("  %-25s error: %v\n", table, err)
				continue
			}
			fmt.Printf("  %-25s %d\n", table, count)
		}
		return nil
	},
}

func init() {
	dbCmd.AddCommand(dbStatsCmd)
}
