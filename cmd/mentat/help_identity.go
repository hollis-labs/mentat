package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// identityHelp is a stand-in for the framework's `identity` commands, which only exist once the identity module has
// started, and starting it creates the signing key and the database. It exists so `mentat identity --help` can print
// something without starting the module; running a stand-in is an error. The real commands are used for every
// invocation that is not help (see plan).
func identityHelp() *cobra.Command {
	notHere := func(*cobra.Command, []string) error {
		return errors.New("this is the help text only; run the command without --help")
	}
	id := &cobra.Command{Use: "identity", Short: "Offline identity bootstrap (owner and API keys, no server)"}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Create the first owner in the database (no server; the password is read from stdin or a prompt)",
		Long: `Creates the first owner with every scope. The password is read from the terminal (prompted twice, not echoed)
or, when stdin is not a terminal, from its first line. It is never an argument or an environment variable.
Refuses when a user already exists. Creates the data directory, the database and the signing key if they are missing.`,
		RunE: notHere,
	}
	setup.Flags().String("username", "", "owner's username (required)")
	setup.Flags().String("display-name", "", "owner's display name (default: the username)")
	key := &cobra.Command{Use: "key", Short: "API keys"}
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an API key for a named principal",
		Long: `Creates an API key for a named principal. The key is shown once: it is written to the file named by --out (a new
file, mode 0600, never overwritten) or, with --stdout, to stdout as the only output. Needs an owner: run setup first.`,
		RunE: notHere,
	}
	create.Flags().String("name", "", "principal name (required)")
	create.Flags().String("out", "", "write the key to this new file (mode 0600)")
	create.Flags().Bool("stdout", false, "write the key to stdout, and nothing else")
	create.Flags().StringSlice("scope", nil, "scope to grant; repeat or comma-separate (required)")
	create.Flags().StringSlice("audience", nil, "audience the key is limited to; repeatable")
	create.Flags().Duration("ttl", 0, "key lifetime, for example 720h (default: no expiry)")
	key.AddCommand(create)
	id.AddCommand(setup, key)
	return id
}
