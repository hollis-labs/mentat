package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/hollis-labs/mentat/internal/jobs"
	semcp "github.com/hollis-labs/mentat/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpTransport string
	mcpHost      string
	mcpPort      int
	mcpPath      string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the Stack Explorer MCP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		jobSvc := jobs.NewService(jobs.Config{Store: store, Workers: 2})
		if err := jobSvc.Start(ctx); err != nil {
			return err
		}
		defer jobSvc.Close()

		switch mcpTransport {
		case "stdio":
			return semcp.ServeStdio(ctx, store, jobSvc)
		case "http":
			return semcp.ListenAndServeHTTP(ctx, store, jobSvc, mcpHost, mcpPort, mcpPath)
		default:
			return fmt.Errorf("unsupported transport %q", mcpTransport)
		}
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpTransport, "transport", "stdio", "transport to use: stdio or http")
	mcpCmd.Flags().StringVar(&mcpHost, "host", "127.0.0.1", "host to listen on for HTTP transport")
	mcpCmd.Flags().IntVar(&mcpPort, "port", 8090, "port to listen on for HTTP transport")
	mcpCmd.Flags().StringVar(&mcpPath, "path", "/mcp", "HTTP path for the MCP endpoint")
}
