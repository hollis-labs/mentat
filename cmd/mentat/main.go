// Command mentat is the Mentat research platform, built on Substrate. The shell (config, logging,
// health, HTTP, auth, identity, secrets, database, MCP, CLI skeleton) comes from the manifest; this
// package and internal/ are the app: its commands, its data and its routes.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	libqueue "github.com/hollis-labs/libs/core/util/queue"
	"github.com/spf13/cobra"

	"github.com/hollis-labs/substrate/boot"
	"github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/cli"
	"github.com/hollis-labs/substrate/modules/database"
	"github.com/hollis-labs/substrate/modules/queue"
	"github.com/hollis-labs/substrate/modules/scheduler"
	"github.com/hollis-labs/substrate/modules/secrets"

	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/persistence"
	"github.com/hollis-labs/mentat/internal/settings"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// What the command groups share. run sets them once the framework has started; tests set store
// directly and build the command tree with newRoot.
var (
	store       *sqlite.Store
	embedder    *embed.Manager
	appSettings settings.Settings
	// jobSvc creates and runs jobs and holds the schedules. It is set once the framework has started;
	// only `serve` starts the queue worker and the scheduler engine that act on it.
	jobSvc *jobs.Service
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "mentat:", err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	fsys, root := persistence.Migrations()
	database.UseMigrationFS(fsys, root)
	// The queue module copies its handlers when it starts, before the service exists; the handler looks the
	// service up when a job arrives.
	queue.UseDriverFunc(newQueue)
	queue.Handle(jobs.JobType, func(ctx context.Context, qj *libqueue.QueuedJob) error {
		if jobSvc == nil {
			return errors.New("the job service is not ready")
		}
		return jobSvc.Handle(ctx, qj)
	})

	// The data directory is only known at run time, so the paths are resolved right after the
	// config (and the SUBSTRATE_CONFIG override) is loaded; a database in the old layout is
	// refused just before the database module opens, and so migrates, the file.
	started, err := boot.StartWith(ctx, substrateApp, substrateModules, substrateConfigJSON, boot.Options{
		AfterConfig: func(_ context.Context, app *module.App) error {
			return resolvePaths(app, getenv)
		},
		Before: map[string]boot.Hook{
			"database": func(_ context.Context, app *module.App) error {
				path, ok := dbPathOf(app)
				if !ok {
					return errors.New("no database path configured")
				}
				return persistence.RefuseLegacy(path)
			},
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = started.Stop(context.Background()) }()
	app := started.App

	if appSettings, err = settings.Parse(app.Config[settings.Key]); err != nil {
		return err
	}
	st := database.From(app)
	if st == nil {
		return errors.New("the database module must be started")
	}
	dbPath, _ := dbPathOf(app)
	store = sqlite.New(st, dbPath)
	if embedder, err = newEmbedder(ctx, app, appSettings); err != nil {
		return err
	}
	if jobSvc, err = newJobs(app); err != nil {
		return err
	}

	rootCmd := cli.Root(app)
	rootCmd.Short = "AI agent tooling research and comparison platform"
	addGroups(rootCmd)
	rootCmd.AddCommand(serveCmd(ctx, app), mcpCmd(app), migrateLegacyCmd(app))
	return rootCmd.ExecuteContext(ctx)
}

// newRoot is the command tree without the framework, for tests that set store themselves.
func newRoot() *cobra.Command {
	root := &cobra.Command{Use: "mentat", SilenceUsage: true}
	addGroups(root)
	return root
}

func addGroups(root *cobra.Command) {
	root.AddCommand(repoCmd, tagCmd, snapshotCmd, dimensionCmd, lensCmd, scoreCmd, scorecardCmd,
		patternCmd, findingCmd, auditCmd, symbolCmd, graphCmd, gapCmd, reportCmd, dbCmd, scheduleCmd,
		embeddingCmd, evalCmd, searchCmd)
}

// newEmbedder builds the embedding manager from the settings, resolving the key reference through
// the secrets module. A missing key is not an error here: embedding calls fail when they are made.
func newEmbedder(ctx context.Context, app *module.App, s settings.Settings) (*embed.Manager, error) {
	profiles := make(map[string]embed.ProfileConfig, len(s.Profiles))
	for name, p := range s.Profiles {
		profiles[name] = embed.ProfileConfig{Name: name, Provider: p.Provider, Model: p.Model}
	}
	key, err := resolveSecret(ctx, app, s.OpenAIKeyRef)
	if err != nil {
		return nil, fmt.Errorf("mentat.embed.openai_key_ref: %w", err)
	}
	return embed.NewManager(embed.Options{Profiles: profiles, OpenAIKey: key, OpenAIBaseURL: s.OpenAIBaseURL}), nil
}

// resolveSecret returns the value behind a secret reference, or "" for an empty reference.
func resolveSecret(ctx context.Context, app *module.App, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	r := secrets.Resolver(app)
	if r == nil {
		return "", errors.New("the secrets module must be started")
	}
	res, err := r.Resolve(ctx, ref)
	if err != nil {
		return "", err
	}
	return res.Secret.Reveal(), nil
}

// appQueue hands jobs to the queue module's queue.
type appQueue struct{ app *module.App }

func (q appQueue) Push(ctx context.Context, jobType string, payload []byte, opts ...libqueue.PushOption) error {
	return queue.Push(ctx, q.app, jobType, payload, opts...)
}

// newJobs builds the job service over the queue and scheduler modules.
func newJobs(app *module.App) (*jobs.Service, error) {
	st, ok := scheduler.Store(app).(jobs.ScheduleStore)
	if !ok {
		return nil, errors.New("the scheduler module's store is not the one mentat expects")
	}
	return jobs.NewService(jobs.Config{
		Store: store, Embedder: embedder, Queue: appQueue{app: app}, Schedules: st, ScheduleZone: appSettings.ScheduleZone,
	}), nil
}
