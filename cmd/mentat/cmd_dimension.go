package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var dimensionCmd = &cobra.Command{
	Use:   "dimension",
	Short: "Manage review dimensions",
}

var dimensionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all review dimensions",
	RunE: func(cmd *cobra.Command, args []string) error {
		dims, err := store.ListDimensions()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tCATEGORY\tWEIGHT\tDESCRIPTION\n")
		for _, d := range dims {
			fmt.Fprintf(w, "%s\t%s\t%s\t%.1f\t%s\n", d.ID, d.Name, d.Category, d.Weight, d.Description)
		}
		w.Flush()
		return nil
	},
}

func init() {
	dimensionCmd.AddCommand(dimensionListCmd)
}
