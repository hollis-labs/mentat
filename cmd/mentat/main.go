package main

import (
	"fmt"
	"os"

	"github.com/hollis-labs/mentat/internal/config"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/spf13/cobra"
)

var (
	dbPath string
	store  *sqlite.Store

	version   = "0.1.0"
	buildDate = "unknown"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mentat",
	Short: "AI agent tooling research and comparison platform",
	Long: `Stack Explorer — analyze, score, and compare AI agent tooling repos.

Track repos, capture snapshots, score across dimensions, discover patterns,
and generate gap analysis reports.

(c) HOLLIS LABS`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip DB init for help/version/completion
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}
		var err error
		store, err = sqlite.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if store != nil {
			store.Close()
		}
	},
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("mentat %s (built %s)\n(c) HOLLIS LABS\n", version, buildDate))
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", config.DefaultDBPath(), "path to SQLite database")

	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(tagCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(dimensionCmd)
	rootCmd.AddCommand(lensCmd)
	rootCmd.AddCommand(scoreCmd)
	rootCmd.AddCommand(scorecardCmd)
	rootCmd.AddCommand(patternCmd)
	rootCmd.AddCommand(findingCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(symbolCmd)
	rootCmd.AddCommand(graphCmd)
	rootCmd.AddCommand(gapCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(scheduleCmd)
}
