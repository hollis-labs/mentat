package main

import (
	"fmt"
	"strconv"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var scoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Manage repo scores",
}

var scoreSetCmd = &cobra.Command{
	Use:   "set <repo-id> <dimension-id> <score>",
	Short: "Set a dimension score for a repo (creates scorecard if needed)",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID := args[0]
		dimID := args[1]
		score, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			return fmt.Errorf("invalid score: %w", err)
		}
		if score < 0 || score > 10 {
			return fmt.Errorf("score must be between 0.0 and 10.0")
		}

		evidence, _ := cmd.Flags().GetString("evidence")
		notes, _ := cmd.Flags().GetString("notes")

		// Get or create latest scorecard
		sc, _ := store.GetLatestScorecard(repoID)
		if sc == nil {
			sc = &domain.Scorecard{RepoID: repoID}
			if err := store.CreateScorecard(sc); err != nil {
				return err
			}
		}

		ds := &domain.DimensionScore{
			ScorecardID: sc.ID,
			DimensionID: dimID,
			Score:       score,
			Evidence:    evidence,
			Notes:       notes,
		}
		if err := store.SetDimensionScore(ds); err != nil {
			return err
		}

		// Recalculate overall — use lens if specified
		lensID, _ := cmd.Flags().GetString("lens")
		if lensID != "" {
			if err := store.RecalculateOverallForLens(sc.ID, lensID); err != nil {
				return err
			}
		} else {
			if err := store.RecalculateOverall(sc.ID); err != nil {
				return err
			}
		}

		fmt.Printf("Set %s/%s = %.1f\n", repoID, dimID, score)
		return nil
	},
}

var scoreGetCmd = &cobra.Command{
	Use:   "get <repo-id>",
	Short: "Get current scores for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sc, err := store.GetLatestScorecard(args[0])
		if err != nil {
			return fmt.Errorf("no scorecard found for %s", args[0])
		}

		lensID, _ := cmd.Flags().GetString("lens")
		if lensID != "" {
			// Recalculate on-the-fly with lens weights
			if err := store.RecalculateOverallForLens(sc.ID, lensID); err != nil {
				return err
			}
			// Re-fetch
			sc, _ = store.GetLatestScorecard(args[0])
			fmt.Printf("Scorecard for %s [lens: %s] (overall: %.1f)\n\n", sc.RepoID, lensID, sc.Overall)
		} else {
			fmt.Printf("Scorecard for %s (overall: %.1f)\n\n", sc.RepoID, sc.Overall)
		}

		for _, ds := range sc.Scores {
			fmt.Printf("  %-22s %.1f", ds.DimensionID, ds.Score)
			if ds.Evidence != "" {
				fmt.Printf("  [%s]", ds.Evidence)
			}
			fmt.Println()
		}
		return nil
	},
}

var scoreRecalcCmd = &cobra.Command{
	Use:   "recalc <repo-id> --lens <lens-id>",
	Short: "Recalculate overall score using a specific lens",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		lensID, _ := cmd.Flags().GetString("lens")
		if lensID == "" {
			return fmt.Errorf("--lens is required")
		}

		sc, err := store.GetLatestScorecard(args[0])
		if err != nil {
			return fmt.Errorf("no scorecard for %s", args[0])
		}

		if err := store.RecalculateOverallForLens(sc.ID, lensID); err != nil {
			return err
		}

		// Re-fetch to show new overall
		sc, _ = store.GetLatestScorecard(args[0])
		fmt.Printf("%s [%s] = %.1f\n", args[0], lensID, sc.Overall)
		return nil
	},
}

func init() {
	scoreSetCmd.Flags().String("evidence", "", "supporting evidence")
	scoreSetCmd.Flags().String("notes", "", "additional notes")
	scoreSetCmd.Flags().String("lens", "", "recalculate overall using this lens")

	scoreGetCmd.Flags().String("lens", "", "view overall through this lens")

	scoreRecalcCmd.Flags().String("lens", "", "lens to use for recalculation (required)")

	scoreCmd.AddCommand(scoreSetCmd)
	scoreCmd.AddCommand(scoreGetCmd)
	scoreCmd.AddCommand(scoreRecalcCmd)
}
