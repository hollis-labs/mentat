package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage the repo catalog",
}

var repoEmbedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Manage per-repo embedding settings",
}

var repoEmbedEnableCmd = &cobra.Command{
	Use:   "enable <repo-id>",
	Short: "Enable embeddings for a repo",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := store.GetRepo(args[0])
		if err != nil {
			return err
		}
		if repo == nil {
			return fmt.Errorf("repo not found: %s", args[0])
		}
		profile, _ := cmd.Flags().GetString("profile")
		if profile != "small" && profile != "medium" && profile != "full" {
			return fmt.Errorf("invalid profile %q", profile)
		}
		if err := store.SetRepoEmbeddingProfile(repo.ID, profile); err != nil {
			return err
		}
		fmt.Printf("Enabled embeddings for %s with profile %s\n", repo.ID, profile)
		return nil
	},
}

var repoEmbedDisableCmd = &cobra.Command{
	Use:   "disable <repo-id>",
	Short: "Disable embeddings for a repo and remove stored vectors",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := store.GetRepo(args[0])
		if err != nil {
			return err
		}
		if repo == nil {
			return fmt.Errorf("repo not found: %s", args[0])
		}
		if err := store.SetRepoEmbeddingProfile(repo.ID, "none"); err != nil {
			return err
		}
		if err := store.DeleteEmbeddingsForRepo(repo.ID); err != nil {
			return err
		}
		fmt.Printf("Disabled embeddings for %s\n", repo.ID)
		return nil
	},
}

var repoAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Add a repo to the catalog",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &domain.Repo{ID: args[0]}
		r.Name, _ = cmd.Flags().GetString("name")
		r.URL, _ = cmd.Flags().GetString("url")
		r.Description, _ = cmd.Flags().GetString("desc")
		r.Stack, _ = cmd.Flags().GetString("stack")
		r.Category, _ = cmd.Flags().GetString("category")
		r.IsOwn, _ = cmd.Flags().GetBool("own")
		r.LocalPath, _ = cmd.Flags().GetString("path")
		r.Homepage, _ = cmd.Flags().GetString("homepage")
		r.License, _ = cmd.Flags().GetString("license")
		r.EmbeddingProfile, _ = cmd.Flags().GetString("embedding-profile")

		if r.Name == "" {
			r.Name = r.ID
		}

		if err := store.CreateRepo(r); err != nil {
			return err
		}
		fmt.Printf("Added repo: %s\n", r.ID)
		return nil
	},
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repos in the catalog",
	RunE: func(cmd *cobra.Command, args []string) error {
		category, _ := cmd.Flags().GetString("category")
		own, _ := cmd.Flags().GetBool("own")
		tag, _ := cmd.Flags().GetString("tag")
		stack, _ := cmd.Flags().GetString("stack")
		format, _ := cmd.Flags().GetString("format")

		filter := sqlite.RepoFilter{
			Category: category,
			Tag:      tag,
			Stack:    stack,
		}
		if cmd.Flags().Changed("own") {
			filter.IsOwn = &own
		}

		repos, err := store.ListRepos(filter)
		if err != nil {
			return err
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(repos)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tCATEGORY\tSTACK\tOWN\tDESCRIPTION\n")
		for _, r := range repos {
			own := ""
			if r.IsOwn {
				own = "*"
			}
			desc := r.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Category, r.Stack, own, desc)
		}
		w.Flush()
		fmt.Printf("\n%d repos\n", len(repos))
		return nil
	},
}

var repoShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show repo details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := store.GetRepo(args[0])
		if err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("repo not found: %s", args[0])
		}

		tags, _ := store.ListRepoTags(r.ID)

		fmt.Printf("ID:          %s\n", r.ID)
		fmt.Printf("Name:        %s\n", r.Name)
		fmt.Printf("URL:         %s\n", r.URL)
		fmt.Printf("Category:    %s\n", r.Category)
		fmt.Printf("Stack:       %s\n", r.Stack)
		fmt.Printf("Own:         %v\n", r.IsOwn)
		fmt.Printf("Local Path:  %s\n", r.LocalPath)
		fmt.Printf("Description: %s\n", r.Description)
		fmt.Printf("Embeddings:  %s\n", r.EmbeddingProfile)
		if len(tags) > 0 {
			names := make([]string, len(tags))
			for i, t := range tags {
				names[i] = t.Name
			}
			fmt.Printf("Tags:        %s\n", strings.Join(names, ", "))
		}
		return nil
	},
}

var repoUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update repo fields",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := store.GetRepo(args[0])
		if err != nil || r == nil {
			return fmt.Errorf("repo not found: %s", args[0])
		}

		if v, _ := cmd.Flags().GetString("name"); cmd.Flags().Changed("name") {
			r.Name = v
		}
		if v, _ := cmd.Flags().GetString("url"); cmd.Flags().Changed("url") {
			r.URL = v
		}
		if v, _ := cmd.Flags().GetString("desc"); cmd.Flags().Changed("desc") {
			r.Description = v
		}
		if v, _ := cmd.Flags().GetString("stack"); cmd.Flags().Changed("stack") {
			r.Stack = v
		}
		if v, _ := cmd.Flags().GetString("category"); cmd.Flags().Changed("category") {
			r.Category = v
		}
		if v, _ := cmd.Flags().GetBool("own"); cmd.Flags().Changed("own") {
			r.IsOwn = v
		}
		if v, _ := cmd.Flags().GetString("path"); cmd.Flags().Changed("path") {
			r.LocalPath = v
		}
		if v, _ := cmd.Flags().GetString("homepage"); cmd.Flags().Changed("homepage") {
			r.Homepage = v
		}
		if v, _ := cmd.Flags().GetString("license"); cmd.Flags().Changed("license") {
			r.License = v
		}
		if v, _ := cmd.Flags().GetString("embedding-profile"); cmd.Flags().Changed("embedding-profile") {
			r.EmbeddingProfile = v
		}

		if err := store.UpdateRepo(r); err != nil {
			return err
		}
		fmt.Printf("Updated repo: %s\n", r.ID)
		return nil
	},
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a repo from the catalog",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := store.DeleteRepo(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed repo: %s\n", args[0])
		return nil
	},
}

type repoManifest struct {
	Repos []repoEntry `yaml:"repos"`
}

type repoEntry struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	URL              string   `yaml:"url"`
	Description      string   `yaml:"description"`
	Stack            string   `yaml:"stack"`
	Category         string   `yaml:"category"`
	IsOwn            bool     `yaml:"is_own"`
	LocalPath        string   `yaml:"local_path"`
	Homepage         string   `yaml:"homepage"`
	License          string   `yaml:"license"`
	EmbeddingProfile string   `yaml:"embedding_profile"`
	Tags             []string `yaml:"tags"`
}

var repoImportCmd = &cobra.Command{
	Use:   "import <file.yaml>",
	Short: "Bulk import repos from a YAML manifest",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}

		var manifest repoManifest
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}

		added, skipped := 0, 0
		for _, entry := range manifest.Repos {
			existing, _ := store.GetRepo(entry.ID)
			if existing != nil {
				skipped++
				continue
			}

			r := &domain.Repo{
				ID:               entry.ID,
				Name:             entry.Name,
				URL:              entry.URL,
				Description:      entry.Description,
				Stack:            entry.Stack,
				Category:         entry.Category,
				IsOwn:            entry.IsOwn,
				LocalPath:        entry.LocalPath,
				Homepage:         entry.Homepage,
				License:          entry.License,
				EmbeddingProfile: entry.EmbeddingProfile,
			}
			if r.Name == "" {
				r.Name = r.ID
			}

			if err := store.CreateRepo(r); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add %s: %v\n", entry.ID, err)
				continue
			}

			for _, tag := range entry.Tags {
				tagID := strings.ToLower(strings.ReplaceAll(tag, " ", "-"))
				store.EnsureTag(tagID, tag)
				store.AddRepoTag(entry.ID, tagID)
			}
			added++
		}

		fmt.Printf("Import complete: %d added, %d skipped (already exist)\n", added, skipped)
		return nil
	},
}

var repoExportCmd = &cobra.Command{
	Use:   "export [file.yaml]",
	Short: "Export repos to YAML manifest (stdout if no file given)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repos, err := store.ListRepos(sqlite.RepoFilter{})
		if err != nil {
			return err
		}

		var entries []repoEntry
		for _, r := range repos {
			tags, _ := store.ListRepoTags(r.ID)
			var tagNames []string
			for _, t := range tags {
				tagNames = append(tagNames, t.Name)
			}
			entries = append(entries, repoEntry{
				ID:               r.ID,
				Name:             r.Name,
				URL:              r.URL,
				Description:      r.Description,
				Stack:            r.Stack,
				Category:         r.Category,
				IsOwn:            r.IsOwn,
				LocalPath:        r.LocalPath,
				Homepage:         r.Homepage,
				License:          r.License,
				EmbeddingProfile: r.EmbeddingProfile,
				Tags:             tagNames,
			})
		}

		data, err := yaml.Marshal(repoManifest{Repos: entries})
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}

		if len(args) > 0 {
			if err := os.WriteFile(args[0], data, 0644); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
			fmt.Printf("Exported %d repos to %s\n", len(entries), args[0])
		} else {
			fmt.Print(string(data))
		}
		return nil
	},
}

func init() {
	repoAddCmd.Flags().String("name", "", "display name")
	repoAddCmd.Flags().String("url", "", "git clone URL")
	repoAddCmd.Flags().String("desc", "", "description")
	repoAddCmd.Flags().String("stack", "", "primary language/stack")
	repoAddCmd.Flags().String("category", "", "category")
	repoAddCmd.Flags().Bool("own", false, "mark as own project")
	repoAddCmd.Flags().String("path", "", "local filesystem path")
	repoAddCmd.Flags().String("homepage", "", "project homepage URL")
	repoAddCmd.Flags().String("license", "", "license type")
	repoAddCmd.Flags().String("embedding-profile", "none", "embedding profile: none, small, medium, full")

	repoUpdateCmd.Flags().String("name", "", "display name")
	repoUpdateCmd.Flags().String("url", "", "git clone URL")
	repoUpdateCmd.Flags().String("desc", "", "description")
	repoUpdateCmd.Flags().String("stack", "", "primary language/stack")
	repoUpdateCmd.Flags().String("category", "", "category")
	repoUpdateCmd.Flags().Bool("own", false, "mark as own project")
	repoUpdateCmd.Flags().String("path", "", "local filesystem path override")
	repoUpdateCmd.Flags().String("homepage", "", "project homepage URL")
	repoUpdateCmd.Flags().String("license", "", "license type")
	repoUpdateCmd.Flags().String("embedding-profile", "", "embedding profile: none, small, medium, full")

	repoListCmd.Flags().String("category", "", "filter by category")
	repoListCmd.Flags().Bool("own", false, "filter to own projects")
	repoListCmd.Flags().String("tag", "", "filter by tag")
	repoListCmd.Flags().String("stack", "", "filter by stack")
	repoListCmd.Flags().String("format", "table", "output format: table|json")

	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoShowCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoImportCmd)
	repoCmd.AddCommand(repoExportCmd)
	repoEmbedEnableCmd.Flags().String("profile", "small", "embedding profile: small, medium, full")
	repoEmbedCmd.AddCommand(repoEmbedEnableCmd)
	repoEmbedCmd.AddCommand(repoEmbedDisableCmd)
	repoCmd.AddCommand(repoEmbedCmd)
}
