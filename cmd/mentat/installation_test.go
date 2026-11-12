package main

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/mentat/internal/testenv"
)

// This file is the only place a test starts a process (TestNoTestStartsAProcessOutsideTheHelpers enforces it), so the
// built binary can only run with the environment testenv builds: never the real home or data directories.

// buildBinary compiles the command into bin. It runs the Go toolchain, not Mentat, and gets the isolated environment.
func buildBinary(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = testenv.Env(filepath.Dir(bin))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
}

// command prepares the installation's binary with the clean environment of env(tokenFile).
func (in *installation) command(tokenFile string, args ...string) *exec.Cmd {
	in.t.Helper()
	cmd := exec.Command(in.bin, args...)
	cmd.Env = in.env(tokenFile)
	return cmd
}
