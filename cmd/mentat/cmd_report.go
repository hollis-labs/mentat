package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Manage report configs and generate reports",
}

var reportCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Create a report config",
	Long: `Create a report config with a lens and filters.

Filters determine which repos appear in the report (union of all filters).
Filter types: category, tag, stack, repo, is_own

Example:
  mentat report create chat-apps \
    --name "Chat Applications" \
    --lens chat-app \
    --filter category=gui-clients \
    --filter category=cli-clients \
    --filter repo=conduit \
    --filter repo=flowise`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("desc")
		lensID, _ := cmd.Flags().GetString("lens")
		audience, _ := cmd.Flags().GetString("audience")
		filters, _ := cmd.Flags().GetStringArray("filter")

		if name == "" {
			name = args[0]
		}

		rc := &domain.ReportConfig{
			ID:          args[0],
			Name:        name,
			Description: desc,
			LensID:      lensID,
			Audience:    audience,
		}
		if err := store.CreateReportConfig(rc); err != nil {
			return err
		}

		for _, f := range filters {
			parts := strings.SplitN(f, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "warning: invalid filter %q (expected type=value)\n", f)
				continue
			}
			if err := store.AddReportConfigFilter(args[0], parts[0], parts[1]); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add filter %s: %v\n", f, err)
			}
		}

		fmt.Printf("Created report: %s\n", args[0])
		return nil
	},
}

var reportAddFilterCmd = &cobra.Command{
	Use:   "add-filter <report-id> <type=value>",
	Short: "Add a filter to a report config",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.SplitN(args[1], "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid filter %q (expected type=value)", args[1])
		}
		if err := store.AddReportConfigFilter(args[0], parts[0], parts[1]); err != nil {
			return err
		}
		fmt.Printf("Added filter: %s=%s to %s\n", parts[0], parts[1], args[0])
		return nil
	},
}

var reportListCmd = &cobra.Command{
	Use:   "list",
	Short: "List report configs",
	RunE: func(cmd *cobra.Command, args []string) error {
		configs, err := store.ListReportConfigs()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tLENS\tAUDIENCE\tDESCRIPTION\n")
		for _, rc := range configs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", rc.ID, rc.Name, rc.LensID, rc.Audience, rc.Description)
		}
		w.Flush()
		return nil
	},
}

var reportShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show report config with filters and resolved repos",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rc, err := store.GetReportConfig(args[0])
		if err != nil {
			return fmt.Errorf("report not found: %s", args[0])
		}

		fmt.Printf("ID:          %s\n", rc.ID)
		fmt.Printf("Name:        %s\n", rc.Name)
		fmt.Printf("Lens:        %s\n", rc.LensID)
		fmt.Printf("Audience:    %s\n", rc.Audience)
		fmt.Printf("Description: %s\n", rc.Description)
		fmt.Printf("Filters:\n")
		for _, f := range rc.Filters {
			fmt.Printf("  %s = %s\n", f.FilterType, f.FilterValue)
		}

		repos, err := store.ResolveReportRepos(args[0])
		if err != nil {
			fmt.Printf("Resolved repos: (error: %v)\n", err)
		} else {
			fmt.Printf("Resolved repos: %d\n", len(repos))
			for _, id := range repos {
				fmt.Printf("  %s\n", id)
			}
		}
		return nil
	},
}

var reportGenerateCmd = &cobra.Command{
	Use:   "generate <id>",
	Short: "Generate a report from a config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		if output == "" {
			output = "reports"
		}

		rc, err := store.GetReportConfig(args[0])
		if err != nil {
			return fmt.Errorf("report not found: %s", args[0])
		}

		repoIDs, err := store.ResolveReportRepos(args[0])
		if err != nil {
			return err
		}

		// Get lens info
		lens, err := store.GetLens(rc.LensID)
		if err != nil {
			return fmt.Errorf("lens not found: %s", rc.LensID)
		}

		// Build leaderboard for filtered repos through this lens
		type entry struct {
			ID       string
			Name     string
			Category string
			Stack    string
			IsOwn    bool
			URL      string
			Overall  float64
			LoC      int
		}

		var entries []entry
		for _, repoID := range repoIDs {
			repo, err := store.GetRepo(repoID)
			if err != nil || repo == nil {
				continue
			}

			sc, _ := store.GetLatestScorecard(repoID)
			if sc == nil {
				continue
			}

			// Recalculate through lens
			store.RecalculateOverallForLens(sc.ID, rc.LensID)
			sc, _ = store.GetLatestScorecard(repoID)

			snaps, _ := store.ListSnapshots(repoID, 1)
			loc := 0
			if len(snaps) > 0 && snaps[0].LoC != nil {
				loc = *snaps[0].LoC
			}

			entries = append(entries, entry{
				ID: repo.ID, Name: repo.Name, Category: repo.Category,
				Stack: repo.Stack, IsOwn: repo.IsOwn, URL: repo.URL,
				Overall: sc.Overall, LoC: loc,
			})
		}

		// Sort by overall desc
		for i := 0; i < len(entries); i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].Overall > entries[i].Overall {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}

		// Generate markdown
		md := fmt.Sprintf("# Report: %s\n\n", rc.Name)
		if rc.Description != "" {
			md += fmt.Sprintf("*%s*\n\n", rc.Description)
		}
		md += fmt.Sprintf("*Lens: `%s` (%s) | %d dimensions | %d repos | Generated %s*\n\n",
			lens.ID, lens.Name, len(lens.Dimensions), len(entries), time.Now().Format("2006-01-02"))

		if rc.Audience != "" {
			md += fmt.Sprintf("**Audience:** %s\n\n", rc.Audience)
		}

		// Dimensions table
		md += "## Scoring Dimensions\n\n"
		md += "| Dimension | Weight |\n|-----------|--------|\n"
		for _, d := range lens.Dimensions {
			md += fmt.Sprintf("| %s | %.1f |\n", d.DimensionName, d.Weight)
		}

		// Rankings
		md += "\n## Rankings\n\n"
		md += "| Rank | Repo | Category | Stack | Own | Score | LoC |\n"
		md += "|------|------|----------|-------|-----|-------|-----|\n"
		for i, e := range entries {
			own := ""
			if e.IsOwn {
				own = "\\*"
				e.Name = "**" + e.Name + "**"
			}
			locStr := ""
			if e.LoC >= 1000000 {
				locStr = fmt.Sprintf("%.1fM", float64(e.LoC)/1000000)
			} else if e.LoC >= 1000 {
				locStr = fmt.Sprintf("%dK", e.LoC/1000)
			} else if e.LoC > 0 {
				locStr = fmt.Sprintf("%d", e.LoC)
			}
			if e.URL != "" {
				md += fmt.Sprintf("| %d | [%s](%s) | %s | %s | %s | %.1f | %s |\n",
					i+1, e.Name, e.URL, e.Category, e.Stack, own, e.Overall, locStr)
			} else {
				md += fmt.Sprintf("| %d | %s | %s | %s | %s | %.1f | %s |\n",
					i+1, e.Name, e.Category, e.Stack, own, e.Overall, locStr)
			}
		}

		// Per-dimension breakdown for top entries
		if len(entries) > 0 {
			md += "\n## Dimension Breakdown (Top Repos)\n\n"
			md += "| Repo | "
			for _, d := range lens.Dimensions {
				md += d.DimensionName + " | "
			}
			md += "\n|------|"
			for range lens.Dimensions {
				md += "------|"
			}
			md += "\n"

			limit := len(entries)
			if limit > 15 {
				limit = 15
			}
			for _, e := range entries[:limit] {
				sc, _ := store.GetLatestScorecard(e.ID)
				if sc == nil {
					continue
				}
				scoreMap := map[string]float64{}
				for _, ds := range sc.Scores {
					scoreMap[ds.DimensionID] = ds.Score
				}
				md += fmt.Sprintf("| %s | ", e.Name)
				for _, d := range lens.Dimensions {
					if s, ok := scoreMap[d.DimensionID]; ok {
						md += fmt.Sprintf("%.0f | ", s)
					} else {
						md += "- | "
					}
				}
				md += "\n"
			}
		}

		md += fmt.Sprintf("\n*Generated by Stack Explorer | (c) Hollis Labs*\n")

		// Restore agent-platform overalls
		for _, repoID := range repoIDs {
			sc, _ := store.GetLatestScorecard(repoID)
			if sc != nil {
				store.RecalculateOverall(sc.ID)
			}
		}

		path := filepath.Join(output, fmt.Sprintf("report-%s.md", args[0]))
		if err := os.WriteFile(path, []byte(md), 0644); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		fmt.Printf("Report written to %s\n", path)
		return nil
	},
}

var reportRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a report config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := store.DeleteReportConfig(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed report: %s\n", args[0])
		return nil
	},
}

func init() {
	reportCreateCmd.Flags().String("name", "", "report name")
	reportCreateCmd.Flags().String("desc", "", "description")
	reportCreateCmd.Flags().String("lens", "general", "lens ID to use")
	reportCreateCmd.Flags().String("audience", "", "target audience (engineer/product/leadership)")
	reportCreateCmd.Flags().StringArray("filter", nil, "filter as type=value (repeatable)")

	reportGenerateCmd.Flags().String("output", "reports", "output directory")

	reportCmd.AddCommand(reportCreateCmd)
	reportCmd.AddCommand(reportAddFilterCmd)
	reportCmd.AddCommand(reportListCmd)
	reportCmd.AddCommand(reportShowCmd)
	reportCmd.AddCommand(reportGenerateCmd)
	reportCmd.AddCommand(reportRemoveCmd)
}
