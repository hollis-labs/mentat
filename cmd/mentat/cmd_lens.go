package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/spf13/cobra"
)

var lensCmd = &cobra.Command{
	Use:   "lens",
	Short: "Manage scoring lenses",
}

var lensListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all lenses",
	RunE: func(cmd *cobra.Command, args []string) error {
		lenses, err := store.ListLenses()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tDIMENSIONS\tDESCRIPTION\n")
		for _, l := range lenses {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", l.ID, l.Name, l.DimCount, l.Description)
		}
		w.Flush()
		return nil
	},
}

var lensShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show lens details with dimensions and weights",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		l, err := store.GetLens(args[0])
		if err != nil {
			return fmt.Errorf("lens not found: %s", args[0])
		}

		fmt.Printf("ID:          %s\n", l.ID)
		fmt.Printf("Name:        %s\n", l.Name)
		fmt.Printf("Description: %s\n", l.Description)
		fmt.Printf("Dimensions:  %d\n\n", len(l.Dimensions))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "DIMENSION\tNAME\tWEIGHT\n")
		for _, d := range l.Dimensions {
			fmt.Fprintf(w, "%s\t%s\t%.1f\n", d.DimensionID, d.DimensionName, d.Weight)
		}
		w.Flush()
		return nil
	},
}

var lensCreateCmd = &cobra.Command{
	Use:   "create <id>",
	Short: "Create a custom lens",
	Long: `Create a custom lens with weighted dimensions.

Example:
  mentat lens create my-lens --name "My Lens" --dimensions hooks:1.5,memory:2.0,security:1.0`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("desc")
		dimsStr, _ := cmd.Flags().GetString("dimensions")

		if name == "" {
			name = args[0]
		}

		l := &domain.Lens{
			ID:          args[0],
			Name:        name,
			Description: desc,
		}
		if err := store.CreateLens(l); err != nil {
			return err
		}

		if dimsStr != "" {
			for i, pair := range strings.Split(dimsStr, ",") {
				parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
				dimID := parts[0]
				weight := 1.0
				if len(parts) == 2 {
					w, err := strconv.ParseFloat(parts[1], 64)
					if err == nil {
						weight = w
					}
				}
				if err := store.AddLensDimension(args[0], dimID, weight, i+1); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add dimension %s: %v\n", dimID, err)
				}
			}
		}

		fmt.Printf("Created lens: %s\n", args[0])
		return nil
	},
}

var lensAddDimCmd = &cobra.Command{
	Use:   "add-dim <lens-id> <dimension-id> [weight]",
	Short: "Add a dimension to a lens",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		weight := 1.0
		if len(args) == 3 {
			w, err := strconv.ParseFloat(args[2], 64)
			if err != nil {
				return fmt.Errorf("invalid weight: %w", err)
			}
			weight = w
		}
		sortOrder, _ := cmd.Flags().GetInt("sort")
		if err := store.AddLensDimension(args[0], args[1], weight, sortOrder); err != nil {
			return err
		}
		fmt.Printf("Added %s to %s (weight: %.1f)\n", args[1], args[0], weight)
		return nil
	},
}

func init() {
	lensCreateCmd.Flags().String("name", "", "display name")
	lensCreateCmd.Flags().String("desc", "", "description")
	lensCreateCmd.Flags().String("dimensions", "", "comma-separated dimension:weight pairs")

	lensAddDimCmd.Flags().Int("sort", 0, "sort order")

	lensCmd.AddCommand(lensListCmd)
	lensCmd.AddCommand(lensShowCmd)
	lensCmd.AddCommand(lensCreateCmd)
	lensCmd.AddCommand(lensAddDimCmd)
}
