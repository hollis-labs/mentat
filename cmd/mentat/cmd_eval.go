package main

import (
	"context"
	"fmt"

	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Run retrieval recall evaluation",
	RunE: func(cmd *cobra.Command, args []string) error {
		queryFile, _ := cmd.Flags().GetString("queries")
		baselineFile, _ := cmd.Flags().GetString("baseline")
		report, err := retrieval.RunEval(context.Background(), store, queryFile, baselineFile)
		if err != nil {
			return err
		}

		fmt.Printf("Queries:     %d\n", report.Total)
		fmt.Printf("Hits@10:     %d\n", report.Hits)
		fmt.Printf("Recall@10:   %.3f\n", report.RecallAt10)
		if baselineFile != "" {
			fmt.Printf("Baseline:    %s\n", baselineFile)
		}
		return nil
	},
}

func init() {
	evalCmd.Flags().String("queries", "eval/queries.yaml", "path to eval query YAML")
	evalCmd.Flags().String("baseline", "eval/baseline.json", "path to write the current baseline report")
	rootCmd.AddCommand(evalCmd)
}
