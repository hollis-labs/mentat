package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Manage time-series snapshots",
}

var snapshotTakeCmd = &cobra.Command{
	Use:   "take <repo-id>",
	Short: "Capture a snapshot of current repo stats",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		loc, _ := cmd.Flags().GetInt("loc")
		files, _ := cmd.Flags().GetInt("files")
		contributors, _ := cmd.Flags().GetInt("contributors")
		commits30d, _ := cmd.Flags().GetInt("commits-30d")
		stars, _ := cmd.Flags().GetInt("stars")

		snap := &domain.Snapshot{
			RepoID:  args[0],
			RawJSON: "{}",
		}
		if cmd.Flags().Changed("loc") {
			snap.LoC = &loc
		}
		if cmd.Flags().Changed("files") {
			snap.Files = &files
		}
		if cmd.Flags().Changed("contributors") {
			snap.Contributors = &contributors
		}
		if cmd.Flags().Changed("commits-30d") {
			snap.Commits30d = &commits30d
		}
		if cmd.Flags().Changed("stars") {
			snap.Stars = &stars
		}

		if err := store.CreateSnapshot(snap); err != nil {
			return err
		}
		fmt.Printf("Snapshot #%d captured for %s\n", snap.ID, args[0])
		return nil
	},
}

var snapshotHistoryCmd = &cobra.Command{
	Use:   "history <repo-id>",
	Short: "Show snapshot history for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		snaps, err := store.ListSnapshots(args[0], limit)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tDATE\tLOC\tFILES\tCONTRIBS\tCOMMITS_30D\tSTARS\n")
		for _, s := range snaps {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID,
				s.CapturedAt.Format("2006-01-02"),
				intPtrStr(s.LoC),
				intPtrStr(s.Files),
				intPtrStr(s.Contributors),
				intPtrStr(s.Commits30d),
				intPtrStr(s.Stars),
			)
		}
		w.Flush()
		return nil
	},
}

func intPtrStr(p *int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *p)
}

func init() {
	snapshotTakeCmd.Flags().Int("loc", 0, "lines of code")
	snapshotTakeCmd.Flags().Int("files", 0, "file count")
	snapshotTakeCmd.Flags().Int("contributors", 0, "contributor count")
	snapshotTakeCmd.Flags().Int("commits-30d", 0, "commits in last 30 days")
	snapshotTakeCmd.Flags().Int("stars", 0, "GitHub stars")

	snapshotHistoryCmd.Flags().Int("limit", 20, "max snapshots to show")

	snapshotCmd.AddCommand(snapshotTakeCmd)
	snapshotCmd.AddCommand(snapshotHistoryCmd)
}
