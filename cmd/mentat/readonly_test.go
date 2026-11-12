package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// tree describes every file and directory under root (path, size, content hash, mode) so two states can be compared.
func tree(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		line := fmt.Sprintf("%s %v %d", rel, info.Mode(), info.Size())
		if !d.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			line += fmt.Sprintf(" %x", sha256.Sum256(raw))
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// run runs the installation's binary and returns its combined output and whether it failed.
func (in *installation) run(args ...string) (string, bool) {
	in.t.Helper()
	cmd := in.command("", args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		in.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err != nil
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return buf.String() + "\n[killed after 20s: the command did not exit]", true
	}
}

// Help, version and every read-only command create nothing, with an empty data directory, with a directory that
// holds only the old database, and refuse where they cannot read (with the migrate-legacy message).
func TestReadOnlyCommandsCreateNothing(t *testing.T) {
	in := newInstallation(t)
	_ = os.RemoveAll(in.dir) // an empty HOME and no data directory at all
	in.env("")               // writes the config file and creates the isolated HOME/XDG directories
	root := filepath.Dir(in.dir)

	for _, args := range [][]string{
		{"--help"}, {"help"}, {"identity", "--help"}, {"identity", "key", "create", "--help"}, {"serve", "--help"},
		{"repo", "--help"}, {"version"}, {"config"}, {"health"}, {"no-such-command"},
	} {
		before := tree(t, root)
		out, failed := in.run(args...)
		if failed != (args[0] == "no-such-command") {
			t.Errorf("mentat %s: failed=%v, output %q", strings.Join(args, " "), failed, out)
		}
		if after := tree(t, root); after != before {
			t.Errorf("mentat %s changed the tree:\n--- before\n%s\n--- after\n%s", strings.Join(args, " "), before, after)
		}
	}
	for _, args := range [][]string{{"repo", "list"}, {"schedule", "list"}, {"mcp"}} {
		before := tree(t, root)
		out, failed := in.run(args...)
		if !failed || !strings.Contains(out, "Nothing was created") {
			t.Errorf("mentat %s with no database: failed=%v output %q", strings.Join(args, " "), failed, out)
		}
		if after := tree(t, root); after != before {
			t.Errorf("mentat %s changed the tree:\n--- before\n%s\n--- after\n%s", strings.Join(args, " "), before, after)
		}
	}

	// A data directory holding only the old database: a read-only command refuses with the migrate-legacy message.
	if err := os.MkdirAll(in.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in.dir, "stack-explorer.db"), []byte("not opened"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"repo", "list"}, {"schedule", "list"}, {"mcp"}, {"serve"}, {"identity", "setup", "--username", "x"}} {
		before := tree(t, root)
		out, failed := in.run(args...)
		if !failed || !strings.Contains(out, "migrate-legacy") {
			t.Errorf("mentat %s beside the old database: failed=%v output %q", strings.Join(args, " "), failed, out)
		}
		if after := tree(t, root); after != before {
			t.Errorf("mentat %s changed the tree:\n--- before\n%s\n--- after\n%s", strings.Join(args, " "), before, after)
		}
	}
	if _, err := os.Stat(filepath.Join(in.dir, "mentat.db")); err == nil {
		t.Fatal("a mentat.db was created beside the old database")
	}
}

// With a new-layout database in place the data commands read it.
func TestDataCommandsReadAnExistingDatabase(t *testing.T) {
	in := newInstallation(t)
	s, err := sqlite.Open(filepath.Join(in.dir, "mentat.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	for _, args := range [][]string{{"repo", "list"}, {"schedule", "list"}} {
		if out, failed := in.run(args...); failed {
			t.Errorf("mentat %s: %s", strings.Join(args, " "), out)
		}
	}
	if _, err := os.Stat(filepath.Join(in.dir, "identity-signing.key")); err == nil {
		t.Error("a data command created a signing key")
	}
}
