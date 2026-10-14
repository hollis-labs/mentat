package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/symbols"
	"github.com/spf13/cobra"
)

var symbolCmd = &cobra.Command{
	Use:   "symbol",
	Short: "Manage indexed symbols",
}

var symbolIngestCmd = &cobra.Command{
	Use:   "ingest <repo-id>",
	Short: "Ingest symbols for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID := args[0]
		repo, err := store.GetRepo(repoID)
		if err != nil {
			return err
		}
		if repo == nil {
			return fmt.Errorf("repo not found: %s", repoID)
		}

		repoPath, err := resolveRepoPath(repoID, repo.LocalPath, cmd)
		if err != nil {
			return err
		}
		commitRef, _ := cmd.Flags().GetString("commit-ref")
		if commitRef == "" {
			commitRef = gitHead(repoPath)
		}

		var langs []string
		if raw, _ := cmd.Flags().GetString("lang"); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					langs = append(langs, part)
				}
			}
		}

		ingester := symbols.NewIngester(symbols.Config{
			WorkDir: repoPath,
			Store:   store,
		})
		result, err := ingester.Ingest(context.Background(), symbols.IngestRequest{
			RepoID:    repoID,
			RepoPath:  repoPath,
			Languages: langs,
			CommitRef: commitRef,
		})
		if err != nil {
			return err
		}

		fmt.Printf("Ingested symbols for %s\n", repoID)
		fmt.Printf("Path:     %s\n", repoPath)
		fmt.Printf("Inserted: %d\n", result.Inserted)
		fmt.Printf("Updated:  %d\n", result.Updated)
		fmt.Printf("Drifted:  %d\n", result.Drifted)
		return nil
	},
}

var symbolShowCmd = &cobra.Command{
	Use:   "show <id-or-qualified-name>",
	Short: "Show a symbol by numeric ID or qualified name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		target := args[0]

		var sym *symbols.Symbol
		if id, err := parseInt64(target); err == nil {
			sym, err = store.GetSymbol(id)
			if err != nil {
				return err
			}
		} else {
			if repoID == "" {
				return fmt.Errorf("--repo is required when looking up by qualified name")
			}
			sym, err = store.FindSymbolByQualifiedName(repoID, target)
			if err != nil {
				return err
			}
		}
		if sym == nil {
			return fmt.Errorf("symbol not found: %s", target)
		}

		fmt.Printf("ID:             %d\n", sym.ID)
		fmt.Printf("Repo:           %s\n", sym.RepoID)
		fmt.Printf("Kind:           %s\n", sym.Kind)
		fmt.Printf("Name:           %s\n", sym.Name)
		fmt.Printf("Qualified Name: %s\n", sym.QualifiedName)
		fmt.Printf("File:           %s\n", sym.FilePath)
		if sym.LineStart != nil && sym.LineEnd != nil {
			fmt.Printf("Lines:          %d-%d\n", *sym.LineStart, *sym.LineEnd)
		}
		fmt.Printf("Language:       %s\n", sym.Language)
		fmt.Printf("Visibility:     %s\n", sym.Visibility)
		fmt.Printf("Content Hash:   %s\n", sym.ContentHash)
		fmt.Printf("Signature Hash: %s\n", sym.SignatureHash)
		if sym.StaleSinceCommit != nil {
			fmt.Printf("Stale Since:    %s\n", *sym.StaleSinceCommit)
		}
		if sym.Docstring != "" {
			fmt.Printf("Docstring:      %s\n", sym.Docstring)
		}
		return nil
	},
}

var symbolSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search symbols by name, qualified name, or file path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoID, _ := cmd.Flags().GetString("repo")
		kind, _ := cmd.Flags().GetString("kind")
		language, _ := cmd.Flags().GetString("lang")
		limit, _ := cmd.Flags().GetInt("limit")
		format, _ := cmd.Flags().GetString("format")

		items, err := store.SearchSymbols(symbols.SearchFilter{
			RepoID:   repoID,
			Query:    args[0],
			Kind:     kind,
			Language: language,
			Limit:    limit,
		})
		if err != nil {
			return err
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tKIND\tLANG\tLOCATION\tQUALIFIED NAME\n")
		for _, item := range items {
			location := item.FilePath
			if item.LineStart != nil {
				location = fmt.Sprintf("%s:%d", item.FilePath, *item.LineStart)
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", item.ID, item.Kind, item.Language, location, item.QualifiedName)
		}
		w.Flush()
		fmt.Printf("\n%d symbols\n", len(items))
		return nil
	},
}

var symbolStatsCmd = &cobra.Command{
	Use:   "stats <repo-id>",
	Short: "Show symbol counts by kind and language for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stats, err := store.SymbolStats(symbols.StatsFilter{RepoID: args[0]})
		if err != nil {
			return err
		}

		fmt.Printf("Repo:   %s\n", stats.RepoID)
		fmt.Printf("Total:  %d\n", stats.Total)
		fmt.Printf("Stale:  %d\n", stats.Stale)

		if len(stats.ByLanguage) > 0 {
			fmt.Printf("\nBy Language\n")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "LANGUAGE\tCOUNT\n")
			for language, count := range stats.ByLanguage {
				fmt.Fprintf(w, "%s\t%d\n", language, count)
			}
			w.Flush()
		}

		if len(stats.ByKind) > 0 {
			fmt.Printf("\nBy Kind\n")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "KIND\tCOUNT\n")
			for kind, count := range stats.ByKind {
				fmt.Fprintf(w, "%s\t%d\n", kind, count)
			}
			w.Flush()
		}
		return nil
	},
}

func init() {
	symbolIngestCmd.Flags().String("lang", "", "comma-separated languages to ingest")
	symbolIngestCmd.Flags().String("path", "", "repo path override")
	symbolIngestCmd.Flags().String("commit-ref", "", "commit ref to mark drift against")

	symbolShowCmd.Flags().String("repo", "", "repo ID when looking up by qualified name")

	symbolSearchCmd.Flags().String("repo", "", "filter by repo ID")
	symbolSearchCmd.Flags().String("kind", "", "filter by symbol kind")
	symbolSearchCmd.Flags().String("lang", "", "filter by language")
	symbolSearchCmd.Flags().Int("limit", 20, "maximum number of results")
	symbolSearchCmd.Flags().String("format", "", "output format: json")

	symbolCmd.AddCommand(symbolIngestCmd)
	symbolCmd.AddCommand(symbolShowCmd)
	symbolCmd.AddCommand(symbolSearchCmd)
	symbolCmd.AddCommand(symbolStatsCmd)
}

func resolveRepoPath(repoID, localPath string, cmd *cobra.Command) (string, error) {
	if override, _ := cmd.Flags().GetString("path"); override != "" {
		return override, nil
	}
	if localPath != "" {
		return localPath, nil
	}
	cwd, err := os.Getwd()
	if err == nil && filepath.Base(cwd) == repoID {
		return cwd, nil
	}
	return "", fmt.Errorf("repo path unknown for %s; set repo.local_path or pass --path", repoID)
}

func gitHead(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseInt64(v string) (int64, error) {
	var id int64
	_, err := fmt.Sscan(v, &id)
	return id, err
}
