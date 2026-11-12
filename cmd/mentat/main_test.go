package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/appdirs"
	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
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

// TestMain keeps every test off the real data, config and state directories: a test binary that
// reached the app's default location would be touching the live installation.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mentat-test-xdg-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for k, sub := range map[string]string{"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
		_ = os.Setenv(k, filepath.Join(root, sub))
	}
	_ = os.Unsetenv("MENTAT_DATA_DIR")
	_ = os.Unsetenv("SUBSTRATE_CONFIG")
	testRoot = root
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

var testRoot string

// A run with no override must resolve inside the test's temporary root. If the XDG variables stopped
// steering the data directory, this fails instead of a test writing to the real installation.
func TestDefaultDataDirIsNotTheRealOne(t *testing.T) {
	dirs, err := appdirs.Resolve(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dirs.Data, testRoot+string(filepath.Separator)) {
		t.Fatalf("default data directory %q is outside the test root %q", dirs.Data, testRoot)
	}
}
