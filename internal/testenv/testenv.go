// Package testenv keeps tests and the binaries they start away from the real installation.
//
// A Mentat process that starts with its default directories creates a signing key and a database there, even for
// --help. Tests therefore run under a temporary root that HOME, every XDG variable and MENTAT_DATA_DIR point at, and a
// child process only ever gets an environment built here.
package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Root is the temporary root of this test binary; empty until Isolate has run.
var Root string

// keep holds the variables a child needs from the real environment (the toolchain's caches, never a Mentat directory).
var keep = map[string]string{}

// Isolate points HOME, XDG_*, MENTAT_DATA_DIR and TMPDIR at a fresh temporary root, removes SUBSTRATE_CONFIG, runs the
// tests and removes the root. Use it as `os.Exit(testenv.Isolate(m))` in TestMain.
func Isolate(m *testing.M) int {
	// Go's own caches live under the real home; fix them first so builds stay warm.
	if out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
			if i < len(lines) {
				keep[k] = lines[i]
				_ = os.Setenv(k, lines[i])
			}
		}
	}
	keep["PATH"] = os.Getenv("PATH")
	root, err := os.MkdirTemp("", "mentat-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	Root = root
	for k, v := range Vars(root) {
		if err := os.MkdirAll(v, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		_ = os.Setenv(k, v)
	}
	_ = os.Unsetenv("SUBSTRATE_CONFIG")
	code := m.Run()
	_ = os.RemoveAll(root)
	return code
}

// Vars are the directory variables for a root: nothing a Mentat process resolves points outside it.
func Vars(root string) map[string]string {
	return map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_DATA_HOME":   filepath.Join(root, "xdg-data"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME":  filepath.Join(root, "xdg-state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "xdg-cache"),
		"TMPDIR":          filepath.Join(root, "tmp"),
		"MENTAT_DATA_DIR": filepath.Join(root, "data"),
	}
}

// Env is the whole environment of a child process: the toolchain variables, the isolated directories under root and
// the extra KEY=VALUE entries (for example SUBSTRATE_CONFIG or a MENTAT_DATA_DIR of the test's own). It never inherits
// the parent's environment.
func Env(root string, extra ...string) []string {
	env := map[string]string{}
	for k, v := range keep {
		env[k] = v
	}
	for k, v := range Vars(root) {
		env[k] = v
		_ = os.MkdirAll(v, 0o700) // a child (go build, TMPDIR) needs the directories to exist
	}
	for _, e := range extra {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// Inside reports whether path is under root.
func Inside(root, path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, filepath.Clean(root)+string(filepath.Separator))
}
