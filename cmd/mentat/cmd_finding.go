package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/domain"
	semcp "github.com/hollis-labs/mentat/internal/mcp"
	"github.com/spf13/cobra"
)

var findingCmd = &cobra.Command{
	Use:   "finding",
	Short: "Manage research findings",
}

var findingAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a finding",
	RunE: func(cmd *cobra.Command, args []string) error {
		f := &domain.Finding{}
		f.Title, _ = cmd.Flags().GetString("title")
		f.Category, _ = cmd.Flags().GetString("category")
		f.Severity, _ = cmd.Flags().GetString("severity")
		f.Description, _ = cmd.Flags().GetString("desc")
		f.Status = "open"

		repoID, _ := cmd.Flags().GetString("repo")
		if repoID != "" {
			f.RepoID = &repoID
		}

		if f.Title == "" {
			return fmt.Errorf("--title is required")
		}

		if err := store.CreateFinding(f); err != nil {
			return err
		}
		fmt.Printf("Finding #%d added: %s\n", f.ID, f.Title)
		return nil
	},
}

var findingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List findings",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		category, _ := cmd.Flags().GetString("category")
		status, _ := cmd.Flags().GetString("status")

		findings, err := store.ListFindings(repoID, category, status)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tREPO\tCATEGORY\tSEVERITY\tSTATUS\tTITLE\n")
		for _, f := range findings {
			repo := "-"
			if f.RepoID != nil {
				repo = *f.RepoID
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", f.ID, repo, f.Category, f.Severity, f.Status, f.Title)
		}
		w.Flush()
		return nil
	},
}

var findingUpdateStatusCmd = &cobra.Command{
	Use:   "update-status <finding-id> <status>",
	Short: "Update the lifecycle status of a finding",
	Long:  "Update the lifecycle status of a finding.\n\nstatus must be one of: open, acknowledged, resolved, wontfix.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := mustInt64(args[0])
		if id <= 0 {
			return fmt.Errorf("invalid finding id %q", args[0])
		}
		status := strings.TrimSpace(args[1])
		if status == "" {
			return fmt.Errorf("status is required")
		}

		svc := semcp.NewService(store, nil)
		updated, err := svc.UpdateFindingStatus(
			semcp.FindingStatusUpdateInput{ID: id, Status: status},
			defaultProvenance("agent", "finding-update-status"),
		)
		if err != nil {
			return err
		}
		fmt.Printf("Finding #%d status updated: %s\n", updated.ID, updated.Status)
		return nil
	},
}

func init() {
	findingAddCmd.Flags().String("title", "", "finding title (required)")
	findingAddCmd.Flags().String("repo", "", "repo ID")
	findingAddCmd.Flags().String("category", "info", "gap|strength|opportunity|risk")
	findingAddCmd.Flags().String("severity", "info", "critical|high|medium|low|info")
	findingAddCmd.Flags().String("desc", "", "description")

	findingListCmd.Flags().String("repo", "", "filter by repo")
	findingListCmd.Flags().String("category", "", "filter by category")
	findingListCmd.Flags().String("status", "", "filter by status")

	findingCmd.AddCommand(findingAddCmd)
	findingCmd.AddCommand(findingListCmd)
	findingCmd.AddCommand(findingUpdateStatusCmd)
}
