package appdirs

import (
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// xdg points every XDG root at a fresh temp tree and returns the base.
func xdg(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	return base
}

func TestDefaultsFollowXDGAndCreateNothing(t *testing.T) {
	base := xdg(t)
	d, err := Resolve(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "data", "mentat"); d.Data != want {
		t.Fatalf("data = %q, want %q", d.Data, want)
	}
	for _, dir := range []string{"data", "config", "state", "cache"} {
		if _, err := os.Stat(filepath.Join(base, dir)); err == nil {
			t.Fatalf("Resolve created %s", dir)
		}
	}
}

func TestExplicitDirectoryWins(t *testing.T) {
	xdg(t)
	data := t.TempDir()
	d, err := Resolve(env(map[string]string{DataDirEnv: data}))
	if err != nil || d.Data != data {
		t.Fatalf("got %+v, %v", d, err)
	}
	if d.Database() != filepath.Join(data, "mentat.db") || d.SigningKey() != filepath.Join(data, "identity-signing.key") || d.Secrets() != filepath.Join(data, "secrets") {
		t.Fatalf("file names changed: %q %q %q", d.Database(), d.SigningKey(), d.Secrets())
	}
}

func TestPrepareMakesPrivateAndTightensLooseModes(t *testing.T) {
	d := Dirs{Data: filepath.Join(t.TempDir(), "data")}
	if err := d.Prepare(); err != nil {
		t.Fatal(err)
	}
	// A directory and file made too open by an earlier run are tightened, not trusted.
	if err := os.Chmod(d.Data, 0o755); err != nil { //nolint:gosec // deliberately loose, to prove Prepare tightens it
		t.Fatal(err)
	}
	if err := os.Chmod(d.Database(), 0o644); err != nil { //nolint:gosec // deliberately loose, to prove Prepare tightens it
		t.Fatal(err)
	}
	if err := d.Prepare(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{d.Data, d.Database()} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is accessible to other users: %v", p, info.Mode())
		}
	}
}
