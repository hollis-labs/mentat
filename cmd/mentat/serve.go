package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/spf13/cobra"

	"github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/auth"
	"github.com/hollis-labs/substrate/modules/httphost"
	"github.com/hollis-labs/substrate/modules/identity"
	"github.com/hollis-labs/substrate/modules/queue"
	"github.com/hollis-labs/substrate/modules/scheduler"

	"github.com/hollis-labs/mentat/internal/api"
	"github.com/hollis-labs/mentat/internal/hostguard"
	semcp "github.com/hollis-labs/mentat/internal/mcp"
)

func serveCmd(ctx context.Context, app *module.App) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the REST API and the MCP endpoint",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return serve(ctx, app) },
	}
}

// serve runs the HTTP host, the queue worker, the scheduler and the scan worker until ctx ends. The
// framework stops the worker and the scheduler when the app stops, before the database closes.
func serve(ctx context.Context, app *module.App) error {
	if err := requireLoopback(app); err != nil {
		return err
	}
	id := identity.From(app)
	table := auth.ScopeTable(app)
	if id == nil || table == nil {
		return errors.New("the identity and auth modules must be started")
	}
	if err := store.MarkInProgressJobsFailed(); err != nil {
		return fmt.Errorf("startup recovery: %w", err)
	}
	githubToken, err := resolveSecret(ctx, app, appSettings.GitHubTokenRef)
	if err != nil {
		return fmt.Errorf("mentat.github_token_ref: %w", err)
	}
	srv := api.NewServer(store, api.Deps{Jobs: jobSvc, Embedder: embedder, GitHubToken: githubToken})
	srv.RegisterScopes(table)
	semcp.Register(app, semcp.NewService(store, jobSvc, embedder, appSettings.ActorModel), table)

	handler, err := httphost.Handler(app, httphost.Options{Routes: srv.Handler()})
	if err != nil {
		return err
	}
	// Outermost first: the Host check, then the session cookie becomes the bearer the guard
	// verifies, then the host (guard, routes).
	handler = hostguard.Handler(appSettings.AllowedHosts, id.Middleware(handler))

	// The queue worker runs jobs and the scheduler engine fires schedules; both are stopped with the app.
	if err := queue.Run(app); err != nil {
		return err
	}
	if err := scheduler.Run(app); err != nil {
		return err
	}
	background, stopBackground := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { srv.Run(background) })
	serveErr := httphost.Serve(ctx, app, handler, httphost.ServeOptions{
		Ready: func(addr net.Addr) { slog.Info("mentat listening", "addr", addr.String()) },
	})
	stopBackground()
	wg.Wait()
	return serveErr
}

// requireLoopback keeps the backend on the loopback interface: the Host check assumes it, and a
// remote client reaches it through an authenticated tunnel or a local TLS proxy.
func requireLoopback(app *module.App) error {
	cfg, _ := app.Config["httphost"].(map[string]any)
	addr, _ := cfg["addr"].(string)
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("mentat currently requires a loopback httphost.addr (for example 127.0.0.1:8081), got %q", addr)
}
