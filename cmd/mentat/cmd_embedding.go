package main

import (
	"context"
	"fmt"

	"github.com/hollis-labs/mentat/internal/retrieval"
	"github.com/spf13/cobra"
)

var embeddingCmd = &cobra.Command{
	Use:   "embedding",
	Short: "Manage retrieval embeddings",
}

var embeddingRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh embeddings for an opted-in repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		force, _ := cmd.Flags().GetBool("force")
		if repoID == "" {
			return fmt.Errorf("--repo is required")
		}

		report, err := retrieval.RefreshRepoEmbeddings(context.Background(), store, repoID, force)
		if err != nil {
			return err
		}
		fmt.Printf("Repo:              %s\n", report.RepoID)
		fmt.Printf("Profile:           %s\n", report.Profile)
		fmt.Printf("Model:             %s\n", report.Model)
		fmt.Printf("Symbols Embedded:  %d\n", report.SymbolsEmbedded)
		fmt.Printf("Findings Embedded: %d\n", report.FindingsEmbedded)
		fmt.Printf("Skipped:           %d\n", report.Skipped)
		return nil
	},
}

func init() {
	embeddingRefreshCmd.Flags().String("repo", "", "repo ID")
	embeddingRefreshCmd.Flags().Bool("force", false, "recompute all embeddings regardless of content hash")
	embeddingCmd.AddCommand(embeddingRefreshCmd)
	rootCmd.AddCommand(embeddingCmd)
}
