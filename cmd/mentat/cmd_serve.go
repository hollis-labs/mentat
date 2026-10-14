package main

import (
	"os"

	"github.com/hollis-labs/mentat/internal/api"
	"github.com/spf13/cobra"
)

var (
	servePort        int
	serveHost        string
	serveToken       string
	serveCORSOrigins []string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the REST API server",
	Long: `Start an HTTP server exposing the Stack Explorer REST API for the Sigil frontend.

The server binds to 127.0.0.1 by default. To listen on another interface,
set a bearer token with --token or STACK_EXPLORER_API_TOKEN; clients then send
"Authorization: Bearer <token>". Binding beyond loopback without a token is refused.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := serveToken
		if token == "" {
			token = os.Getenv("STACK_EXPLORER_API_TOKEN")
		}
		srv := api.NewServer(store)
		return srv.ListenAndServe(api.ServeOptions{
			Host:        serveHost,
			Port:        servePort,
			Token:       token,
			CORSOrigins: serveCORSOrigins,
		})
	},
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "port to listen on")
	serveCmd.Flags().StringVar(&serveHost, "host", "127.0.0.1", "interface to listen on (non-loopback requires a token)")
	serveCmd.Flags().StringVar(&serveToken, "token", "", "bearer token required on /api requests (default $STACK_EXPLORER_API_TOKEN)")
	serveCmd.Flags().StringSliceVar(&serveCORSOrigins, "cors-origin", nil, "browser origins allowed to call the API (default http://localhost:3334)")
	rootCmd.AddCommand(serveCmd)
}
