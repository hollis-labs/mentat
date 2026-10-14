package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage repo tags",
}

var tagAddCmd = &cobra.Command{
	Use:   "add <repo-id> <tag> [<tag>...]",
	Short: "Add tags to a repo",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID := args[0]
		for _, tag := range args[1:] {
			tagID := strings.ToLower(strings.ReplaceAll(tag, " ", "-"))
			if err := store.EnsureTag(tagID, tag); err != nil {
				return err
			}
			if err := store.AddRepoTag(repoID, tagID); err != nil {
				return err
			}
		}
		fmt.Printf("Tagged %s: %s\n", repoID, strings.Join(args[1:], ", "))
		return nil
	},
}

var tagRemoveCmd = &cobra.Command{
	Use:   "remove <repo-id> <tag>",
	Short: "Remove a tag from a repo",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tagID := strings.ToLower(strings.ReplaceAll(args[1], " ", "-"))
		return store.RemoveRepoTag(args[0], tagID)
	},
}

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		tags, err := store.ListAllTags()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\n")
		for _, t := range tags {
			fmt.Fprintf(w, "%s\t%s\n", t.ID, t.Name)
		}
		w.Flush()
		return nil
	},
}

func init() {
	tagCmd.AddCommand(tagAddCmd)
	tagCmd.AddCommand(tagRemoveCmd)
	tagCmd.AddCommand(tagListCmd)
}
