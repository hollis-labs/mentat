package main

import (
	"github.com/spf13/cobra"

	"github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/auth"
	mcpmod "github.com/hollis-labs/substrate/modules/mcp"

	semcp "github.com/hollis-labs/mentat/internal/mcp"
)

// mcpCmd serves the tools over stdio for an agent that launches mentat as a subprocess. The HTTP
// transport is the main listener's /mcp (see `mentat serve`); this process never runs jobs.
func mcpCmd(app *module.App) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the MCP tools over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			semcp.Register(app, semcp.NewService(store, jobSvc, embedder, appSettings.ActorModel), auth.ScopeTable(app))
			return mcpmod.ServeStdio(cmd.Context(), app, mcpmod.StdioOptions{
				// With no token file configured the process runs as the local principal: whoever can run it can
				// read the data file anyway. The scopes are listed, never the default "*": read and write, not
				// admin (backup, audit import and export need a key that holds mentat.admin, given in the token file).
				LocalTrust:  true,
				LocalScopes: []string{semcp.ScopeRead, semcp.ScopeWrite},
			})
		},
	}
}
