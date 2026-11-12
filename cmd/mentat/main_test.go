package main

import (
	"os"
	"testing"

	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/testenv"
)

// The command tests run the real command tree against a throwaway database, without starting the
// framework: setCLI names the file and the arguments, execCLI opens the file as store, runs, and
// closes it again.
var (
	cliPath string
	cliArgs []string
)

func setCLI(path string, args []string) { cliPath, cliArgs = path, args }

func execCLI() error {
	s, err := sqlite.Open(cliPath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	store, embedder = s, embed.NewManager(embed.Options{})
	defer func() { store, embedder = nil, nil }()
	root := newRoot()
	root.SetArgs(cliArgs)
	return root.Execute()
}

// TestMain keeps every test off the real home, data, config and state directories: a test binary that reached the
// app's default location would be touching the live installation (see internal/testenv).
func TestMain(m *testing.M) { os.Exit(testenv.Isolate(m)) }
