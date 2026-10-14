package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage maintenance schedules and manual triggers",
}

var scheduleAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a recurring schedule",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		repoID, _ := cmd.Flags().GetString("repo")
		cronExpr, _ := cmd.Flags().GetString("cron")
		kind, _ := cmd.Flags().GetString("kind")
		payload, _ := cmd.Flags().GetString("payload")
		maxAttempts, _ := cmd.Flags().GetInt("max-attempts")
		retryBackoff, _ := cmd.Flags().GetString("retry-backoff")
		retryDelay, _ := cmd.Flags().GetInt("retry-delay")
		if name == "" || cronExpr == "" || kind == "" {
			return fmt.Errorf("--name, --cron, and --kind are required")
		}
		svc := jobs.NewService(jobs.Config{Store: store, Workers: 1})
		schedule := &domain.Schedule{
			Name:           name,
			RepoID:         repoID,
			CronExpr:       cronExpr,
			JobKind:        kind,
			PayloadJSON:    payload,
			Enabled:        true,
			MaxAttempts:    maxAttempts,
			RetryBackoff:   retryBackoff,
			RetryDelaySecs: retryDelay,
		}
		if err := svc.AddSchedule(schedule); err != nil {
			return err
		}
		fmt.Printf("Added schedule %s (%s)\n", schedule.ID, schedule.Name)
		return nil
	},
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schedules",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		format, _ := cmd.Flags().GetString("format")
		svc := jobs.NewService(jobs.Config{Store: store, Workers: 1})
		items, err := svc.ListSchedules(repoID)
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tREPO\tKIND\tCRON\tENABLED\tNEXT RUN")
		for _, item := range items {
			nextRun := ""
			if item.NextRunAt != nil {
				nextRun = item.NextRunAt.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%t\t%s\n", item.ID, item.Name, item.RepoID, item.JobKind, item.CronExpr, item.Enabled, nextRun)
		}
		return w.Flush()
	},
}

var scheduleRemoveCmd = &cobra.Command{
	Use:   "remove <schedule-id>",
	Short: "Remove a schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := jobs.NewService(jobs.Config{Store: store, Workers: 1})
		if err := svc.RemoveSchedule(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed schedule %s\n", args[0])
		return nil
	},
}

var scheduleTriggerCmd = &cobra.Command{
	Use:   "trigger <schedule-id>",
	Short: "Trigger a schedule immediately and wait for completion",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		timeoutSecs, _ := cmd.Flags().GetInt("timeout")
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
		defer cancel()
		svc := jobs.NewService(jobs.Config{Store: store, Workers: 1})
		if err := svc.Start(ctx); err != nil {
			return err
		}
		defer svc.Close()
		job, err := svc.TriggerSchedule(ctx, args[0])
		if err != nil {
			return err
		}
		job, err = svc.WaitForTerminal(ctx, job.ID)
		if err != nil {
			return err
		}
		fmt.Printf("Job:     %s\n", job.ID)
		fmt.Printf("Status:  %s\n", job.Status)
		if job.Error != "" {
			fmt.Printf("Error:   %s\n", job.Error)
		}
		fmt.Printf("Output:  %s\n", job.OutputJSON)
		return nil
	},
}

func init() {
	scheduleAddCmd.Flags().String("name", "", "schedule name")
	scheduleAddCmd.Flags().String("repo", "", "repo ID")
	scheduleAddCmd.Flags().String("cron", "", "cron expression with seconds")
	scheduleAddCmd.Flags().String("kind", "", "job kind")
	scheduleAddCmd.Flags().String("payload", "{}", "job payload as JSON object")
	scheduleAddCmd.Flags().Int("max-attempts", 1, "maximum attempts per job")
	scheduleAddCmd.Flags().String("retry-backoff", "fixed", "retry backoff: fixed, linear, exponential")
	scheduleAddCmd.Flags().Int("retry-delay", 1, "base retry delay in seconds")

	scheduleListCmd.Flags().String("repo", "", "filter by repo")
	scheduleListCmd.Flags().String("format", "", "output format: json")

	scheduleTriggerCmd.Flags().Int("timeout", 300, "wait timeout in seconds")

	scheduleCmd.AddCommand(scheduleAddCmd)
	scheduleCmd.AddCommand(scheduleListCmd)
	scheduleCmd.AddCommand(scheduleRemoveCmd)
	scheduleCmd.AddCommand(scheduleTriggerCmd)
}
