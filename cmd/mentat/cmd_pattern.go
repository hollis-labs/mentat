package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var patternCmd = &cobra.Command{
	Use:   "pattern",
	Short: "Manage architecture patterns and anti-patterns",
}

var patternAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Add a new pattern",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := &domain.ArchitecturePattern{ID: args[0]}
		p.Name, _ = cmd.Flags().GetString("name")
		p.Type, _ = cmd.Flags().GetString("type")
		p.Category, _ = cmd.Flags().GetString("category")
		p.Description, _ = cmd.Flags().GetString("desc")

		if p.Name == "" {
			p.Name = p.ID
		}

		if err := store.CreatePattern(p); err != nil {
			return err
		}
		fmt.Printf("Added pattern: %s (%s)\n", p.ID, p.Type)
		return nil
	},
}

var patternListCmd = &cobra.Command{
	Use:   "list",
	Short: "List patterns",
	RunE: func(cmd *cobra.Command, args []string) error {
		patternType, _ := cmd.Flags().GetString("type")
		patterns, err := store.ListPatterns(patternType)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tTYPE\tCATEGORY\tNAME\n")
		for _, p := range patterns {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Type, p.Category, p.Name)
		}
		w.Flush()
		return nil
	},
}

var patternLinkCmd = &cobra.Command{
	Use:   "link <pattern-id> <repo-id>",
	Short: "Link a pattern to a repo",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		quality, _ := cmd.Flags().GetString("quality")
		notes, _ := cmd.Flags().GetString("notes")

		rp := &domain.RepoPattern{
			PatternID: args[0],
			RepoID:    args[1],
			Quality:   quality,
			Notes:     notes,
		}
		if err := store.LinkRepoPattern(rp); err != nil {
			return err
		}
		fmt.Printf("Linked %s -> %s (%s)\n", args[0], args[1], quality)
		return nil
	},
}

func init() {
	patternAddCmd.Flags().String("name", "", "display name")
	patternAddCmd.Flags().String("type", "pattern", "pattern or anti_pattern")
	patternAddCmd.Flags().String("category", "", "category")
	patternAddCmd.Flags().String("desc", "", "description")

	patternListCmd.Flags().String("type", "", "filter by type")

	patternLinkCmd.Flags().String("quality", "present", "exemplary|present|partial|absent")
	patternLinkCmd.Flags().String("notes", "", "notes about implementation")

	patternCmd.AddCommand(patternAddCmd)
	patternCmd.AddCommand(patternListCmd)
	patternCmd.AddCommand(patternLinkCmd)
}
