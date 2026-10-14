package main

import (
	"fmt"
	"strings"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var gapCmd = &cobra.Command{
	Use:   "gap",
	Short: "Gap analysis between repos",
}

var gapCompareSetCmd = &cobra.Command{
	Use:   "compare-set",
	Short: "Manage comparison sets",
}

var gapCompareSetCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Create a comparison set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("desc")
		subjects, _ := cmd.Flags().GetString("subjects")
		references, _ := cmd.Flags().GetString("references")

		cs := &domain.ComparisonSet{
			ID:          args[0],
			Name:        name,
			Description: desc,
		}
		if cs.Name == "" {
			cs.Name = cs.ID
		}

		if err := store.CreateComparisonSet(cs); err != nil {
			return err
		}

		for _, r := range splitCSV(subjects) {
			store.AddComparisonSetRepo(cs.ID, r, "subject")
		}
		for _, r := range splitCSV(references) {
			store.AddComparisonSetRepo(cs.ID, r, "reference")
		}

		fmt.Printf("Created comparison set: %s\n", cs.ID)
		return nil
	},
}

var gapCompareSetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List comparison sets",
	RunE: func(cmd *cobra.Command, args []string) error {
		sets, err := store.ListComparisonSets()
		if err != nil {
			return err
		}
		for _, cs := range sets {
			fmt.Printf("%s  %s  %s\n", cs.ID, cs.Name, cs.Description)
		}
		return nil
	},
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func init() {
	gapCompareSetCreateCmd.Flags().String("name", "", "display name")
	gapCompareSetCreateCmd.Flags().String("desc", "", "description")
	gapCompareSetCreateCmd.Flags().String("subjects", "", "comma-separated subject repo IDs")
	gapCompareSetCreateCmd.Flags().String("references", "", "comma-separated reference repo IDs")

	gapCompareSetCmd.AddCommand(gapCompareSetCreateCmd)
	gapCompareSetCmd.AddCommand(gapCompareSetListCmd)

	gapCmd.AddCommand(gapCompareSetCmd)
}
