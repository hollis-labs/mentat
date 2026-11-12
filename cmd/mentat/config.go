package main

import (
	"fmt"
	"path/filepath"

	"github.com/hollis-labs/substrate/module"

	"github.com/hollis-labs/mentat/internal/appdirs"
)

// resolvePaths makes the data directory private and points the files that live in it at it. The
// manifest holds working-directory defaults (mentat.db, identity-signing.key, secrets); the
// operator's data directory (XDG data home, or MENTAT_DATA_DIR) is only known at run time. A path
// the operator set to something absolute in the SUBSTRATE_CONFIG file is left alone.
func resolvePaths(app *module.App, getenv func(string) string) error {
	dirs, err := appdirs.Resolve(getenv)
	if err != nil {
		return err
	}
	if err = dirs.Prepare(); err != nil {
		return fmt.Errorf("prepare the data directory %s: %w", dirs.Data, err)
	}
	under := func(current, fallback string) string {
		switch {
		case filepath.IsAbs(current):
			return current
		case current == "":
			return filepath.Join(dirs.Data, fallback)
		default:
			return filepath.Join(dirs.Data, current)
		}
	}
	block := func(name string, nested ...string) map[string]any {
		m, _ := app.Config[name].(map[string]any)
		if m == nil {
			m = map[string]any{}
			app.Config[name] = m
		}
		for _, n := range nested {
			child, _ := m[n].(map[string]any)
			if child == nil {
				child = map[string]any{}
				m[n] = child
			}
			m = child
		}
		return m
	}
	db := block("database", "sqlite")
	cur, _ := db["path"].(string)
	db["path"] = under(cur, appdirs.DatabaseFile)
	id := block("identity")
	cur, _ = id["key_file"].(string)
	id["key_file"] = under(cur, appdirs.SigningKeyFile)
	sec := block("secrets")
	cur, _ = sec["state_dir"].(string)
	sec["state_dir"] = under(cur, appdirs.SecretsDir)
	return nil
}

// dbPathOf is the SQLite file the started database module opened.
func dbPathOf(app *module.App) (string, bool) {
	db, _ := app.Config["database"].(map[string]any)
	sq, _ := db["sqlite"].(map[string]any)
	p, ok := sq["path"].(string)
	return p, ok
}
