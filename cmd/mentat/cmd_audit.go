package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/audits/import/deepreview"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Manage audits",
}

var auditStartCmd = &cobra.Command{
	Use:   "start <repo>",
	Short: "Start a new audit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scope, _ := cmd.Flags().GetString("scope")
		auditType, _ := cmd.Flags().GetString("type")
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("--scope is required")
		}
		s := audits.NewStore(store.DB())
		audit := &audits.Audit{
			RepoID:     args[0],
			Scope:      scope,
			AuditType:  auditType,
			Auditor:    actorID(),
			Status:     "in_progress",
			Provenance: defaultProvenance("human", "audit-start"),
			StartedAt:  time.Now().UTC(),
		}
		if err := s.CreateAudit(audit); err != nil {
			return err
		}
		fmt.Printf("Audit #%d started for %s (%s)\n", audit.ID, audit.RepoID, audit.Scope)
		return nil
	},
}

var auditFinishCmd = &cobra.Command{
	Use:   "finish <id>",
	Short: "Finish an audit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := mustInt64(args[0])
		verdict, _ := cmd.Flags().GetString("verdict")
		summary, _ := cmd.Flags().GetString("summary")
		s := audits.NewStore(store.DB())
		audit, err := s.GetAudit(id)
		if err != nil {
			return err
		}
		if audit == nil {
			return fmt.Errorf("audit not found: %d", id)
		}
		now := time.Now().UTC()
		audit.Status = "completed"
		audit.FinishedAt = &now
		if verdict != "" {
			audit.Verdict = &verdict
		}
		if summary != "" {
			audit.SummaryMarkdown = summary
		}
		if err := s.UpdateAudit(audit); err != nil {
			return err
		}
		fmt.Printf("Audit #%d finished\n", audit.ID)
		return nil
	},
}

var auditListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audits",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		status, _ := cmd.Flags().GetString("status")
		format, _ := cmd.Flags().GetString("format")
		s := audits.NewStore(store.DB())
		items, err := s.ListAudits(audits.ListFilter{RepoID: repoID, Status: status})
		if err != nil {
			return err
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tREPO\tSTATUS\tTYPE\tSCOPE\tFINDINGS")
		for _, item := range items {
			bundle, err := s.GetAuditBundle(item.ID)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\n", item.ID, item.RepoID, item.Status, item.AuditType, item.Scope, len(bundle.Findings))
		}
		return w.Flush()
	},
}

var auditShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show an audit with findings and themes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := mustInt64(args[0])
		format, _ := cmd.Flags().GetString("format")
		s := audits.NewStore(store.DB())
		bundle, err := s.GetAuditBundle(id)
		if err != nil {
			return err
		}
		if bundle == nil {
			return fmt.Errorf("audit not found: %d", id)
		}
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(bundle)
		}
		fmt.Printf("Audit #%d\n", bundle.Audit.ID)
		fmt.Printf("Repo:      %s\n", bundle.Audit.RepoID)
		fmt.Printf("Scope:     %s\n", bundle.Audit.Scope)
		fmt.Printf("Status:    %s\n", bundle.Audit.Status)
		fmt.Printf("Type:      %s\n", bundle.Audit.AuditType)
		fmt.Printf("Auditor:   %s\n", bundle.Audit.Auditor)
		if bundle.Audit.Verdict != nil {
			fmt.Printf("Verdict:   %s\n", *bundle.Audit.Verdict)
		}
		if len(bundle.Themes) > 0 {
			names := make([]string, 0, len(bundle.Themes))
			for _, theme := range bundle.Themes {
				names = append(names, theme.Name)
			}
			fmt.Printf("Themes:    %s\n", strings.Join(names, ", "))
		}
		fmt.Println()
		for _, finding := range bundle.Findings {
			fmt.Printf("[%s] %s\n", strings.ToUpper(finding.Severity), finding.Title)
			fmt.Printf("  category: %s\n", finding.Category)
			fmt.Printf("  status:   %s\n", finding.Status)
			if len(finding.Themes) > 0 {
				names := make([]string, 0, len(finding.Themes))
				for _, theme := range finding.Themes {
					names = append(names, theme.Name)
				}
				fmt.Printf("  themes:   %s\n", strings.Join(names, ", "))
			}
			if len(finding.CodeRefs) > 0 {
				fmt.Printf("  refs:     %s\n", finding.CodeRefs[0].FilePath)
			}
			fmt.Printf("  %s\n\n", finding.Description)
		}
		return nil
	},
}

var auditImportCmd = &cobra.Command{
	Use:   "import <folder>",
	Short: "Import deep-review audits from a folder",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		modelName, _ := cmd.Flags().GetString("model-name")
		if repoID == "" {
			return fmt.Errorf("--repo is required")
		}
		s := audits.NewStore(store.DB())
		dirs, err := auditDirectories(args[0])
		if err != nil {
			return err
		}
		provenance := defaultProvenance("importer", "audit-import")
		if strings.TrimSpace(modelName) != "" {
			provenance.ModelName = strings.TrimSpace(modelName)
		}
		for _, dir := range dirs {
			bundle, err := deepreview.ParseDir(dir, repoID, provenance)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Printf("Would import %s: %d findings\n", bundle.Audit.Scope, len(bundle.Findings))
				continue
			}
			saved, err := s.ReplaceImportedAudit(*bundle)
			if err != nil {
				return err
			}
			fmt.Printf("Imported audit #%d %s (%d findings)\n", saved.Audit.ID, saved.Audit.Scope, len(saved.Findings))
		}
		return nil
	},
}

var auditExportCmd = &cobra.Command{
	Use:   "export <id>",
	Short: "Export an audit to deep-review markdown",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := mustInt64(args[0])
		outDir, _ := cmd.Flags().GetString("out")
		s := audits.NewStore(store.DB())
		bundle, err := s.GetAuditBundle(id)
		if err != nil {
			return err
		}
		if bundle == nil {
			return fmt.Errorf("audit not found: %d", id)
		}
		if outDir == "" {
			outDir = filepath.Join(".", fmt.Sprintf("audit-%d-export", id))
		}
		if err := audits.ExportBundle(bundle, outDir); err != nil {
			return err
		}
		fmt.Printf("Exported audit #%d to %s\n", id, outDir)
		return nil
	},
}

var auditDiffCmd = &cobra.Command{
	Use:   "diff <id-a> <id-b>",
	Short: "Diff two audits from the same repo",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s := audits.NewStore(store.DB())
		left, err := s.GetAuditBundle(mustInt64(args[0]))
		if err != nil {
			return err
		}
		right, err := s.GetAuditBundle(mustInt64(args[1]))
		if err != nil {
			return err
		}
		diff, err := audits.DiffBundles(left, right)
		if err != nil {
			return err
		}
		printDiffSection("Resolved", diff.Resolved)
		printDiffSection("Still open", diff.StillOpen)
		printDiffSection("New", diff.New)
		printDiffSection("Regressed", diff.Regressed)
		if len(diff.PossiblySame) > 0 {
			fmt.Println("Possibly same")
			for _, line := range diff.PossiblySame {
				fmt.Printf("- %s\n", line)
			}
		}
		return nil
	},
}

func init() {
	auditStartCmd.Flags().String("scope", "", "audit scope label")
	auditStartCmd.Flags().String("type", "deep-review", "audit type")

	auditFinishCmd.Flags().String("verdict", "", "verdict value")
	auditFinishCmd.Flags().String("summary", "", "summary markdown")

	auditListCmd.Flags().String("repo", "", "filter by repo")
	auditListCmd.Flags().String("status", "", "filter by status")
	auditListCmd.Flags().String("format", "", "output format: json")

	auditShowCmd.Flags().String("format", "", "output format: json")

	auditImportCmd.Flags().String("repo", "", "repo ID")
	auditImportCmd.Flags().Bool("dry-run", false, "report imports without writing")
	auditImportCmd.Flags().String("model-name", "", "override provenance model_name for imported audits and findings")

	auditExportCmd.Flags().String("out", "", "output directory")

	auditCmd.AddCommand(auditStartCmd)
	auditCmd.AddCommand(auditFinishCmd)
	auditCmd.AddCommand(auditListCmd)
	auditCmd.AddCommand(auditShowCmd)
	auditCmd.AddCommand(auditImportCmd)
	auditCmd.AddCommand(auditExportCmd)
	auditCmd.AddCommand(auditDiffCmd)
}

func auditDirectories(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	if hasExactEntry(root, "index.md") {
		return []string{root}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if hasExactEntry(dir, "index.md") {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no audit folders found under %s", root)
	}
	return dirs, nil
}

func hasExactEntry(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return true
		}
	}
	return false
}

func actorID() string {
	if value := strings.TrimSpace(os.Getenv("STACK_EXPLORER_ACTOR_ID")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	return "stack-explorer-cli"
}

func defaultProvenance(actorKind, toolName string) audits.Provenance {
	sessionID := strings.TrimSpace(os.Getenv("STACK_EXPLORER_SESSION_ID"))
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	return audits.Provenance{
		ActorKind: actorKind,
		ActorID:   actorID(),
		SessionID: sessionID,
		ToolName:  toolName,
		ModelName: strings.TrimSpace(os.Getenv("STACK_EXPLORER_MODEL")),
	}
}

func mustInt64(value string) int64 {
	var id int64
	fmt.Sscanf(value, "%d", &id)
	return id
}

func printDiffSection(label string, items []audits.Finding) {
	fmt.Println(label)
	if len(items) == 0 {
		fmt.Println("- none")
		fmt.Println()
		return
	}
	for _, item := range items {
		fmt.Printf("- [%s] %s\n", strings.ToUpper(item.Severity), item.Title)
	}
	fmt.Println()
}
