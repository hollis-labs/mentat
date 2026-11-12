package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/hollis-labs/substrate/module"

	"github.com/hollis-labs/mentat/internal/legacy"
)

// migrateLegacyCmd copies a pre-Substrate database into the file the database module opened. The
// old file is attached read-only and stays untouched, so it remains the rollback. Stop `mentat
// serve` on the target first; the copy takes the write lock.
func migrateLegacyCmd(app *module.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-legacy --from <old database>",
		Short: "Copy a pre-Substrate Mentat database into this installation's new, empty database",
		Long: `Copies every table of a database from before Mentat moved onto Substrate into the new
database (database.sqlite.path), keeping primary keys, rebuilding the full-text indexes and proving
the copy: row counts and a content digest per table, foreign keys, integrity, the counters and the
search hits. It commits only when every check passes. It refuses a target that already holds data
and a source that is not a pre-Substrate database. The source is opened read-only and is never
written.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, _ := cmd.Flags().GetString("from")
			if from == "" {
				return errors.New("--from is required")
			}
			target, ok := dbPathOf(app)
			if !ok {
				return errors.New("no database path configured")
			}
			_, err := legacy.Run(cmd.Context(), legacy.Options{Source: from, Target: target, ScheduleZone: appSettings.ScheduleZone, Out: cmd.OutOrStdout()})
			return err
		},
	}
	cmd.Flags().String("from", "", "the pre-Substrate database file to copy (read-only)")
	return cmd
}
