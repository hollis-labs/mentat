package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/hollis-labs/mentat/internal/appdirs"
	"github.com/hollis-labs/mentat/internal/testenv"
)

// Every variable a Mentat process resolves a directory from points under the test root.
func TestEveryDefaultDirectoryIsInsideTheTestRoot(t *testing.T) {
	for _, k := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TMPDIR", "MENTAT_DATA_DIR"} {
		if v := os.Getenv(k); !testenv.Inside(testenv.Root, v) {
			t.Errorf("%s=%q is outside the test root %q", k, v, testenv.Root)
		}
	}
	if os.Getenv("SUBSTRATE_CONFIG") != "" {
		t.Error("SUBSTRATE_CONFIG is set")
	}
	// With the explicit override removed, the XDG variables alone must still steer the default data directory inside.
	dirs, err := appdirs.Resolve(func(k string) string {
		if k == appdirs.DataDirEnv {
			return ""
		}
		return os.Getenv(k)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !testenv.Inside(testenv.Root, dirs.Data) {
		t.Fatalf("default data directory %q is outside the test root %q", dirs.Data, testenv.Root)
	}
	// A child gets only isolated directories.
	for _, e := range testenv.Env(testenv.Root) {
		k, v, _ := strings.Cut(e, "=")
		switch k {
		case "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TMPDIR", "MENTAT_DATA_DIR":
			if !testenv.Inside(testenv.Root, v) {
				t.Errorf("child environment %s=%q is outside the test root", k, v)
			}
		}
	}
}

// No test file starts a process except installation_test.go, whose helpers set the clean environment, and
// internal/testenv. A test that runs `go run`, the built binary or `mentat` by hand would use the real directories.
func TestNoTestStartsAProcessOutsideTheHelpers(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		slash := filepath.ToSlash(path)
		if strings.HasSuffix(slash, "cmd/mentat/installation_test.go") || strings.Contains(slash, "internal/testenv/") ||
			strings.HasSuffix(slash, "cmd/mentat/isolation_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
					t.Errorf("%s: exec.%s outside the helpers in cmd/mentat/installation_test.go", fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// plan reads the first word that does not start with "-"; a global flag that takes a value would be read as the command.
func TestRootHasNoValueFlags(t *testing.T) {
	root := newRoot()
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.Type() != "bool" {
			t.Errorf("root flag --%s takes a value; plan() must learn it", f.Name)
		}
	})
}
