// Package appdirs decides where Mentat keeps its files and makes them private. Durable
// data (the database, the signing key and the secrets store) lives under XDG_DATA_HOME,
// resolved by the shared apppaths layer, or under MENTAT_DATA_DIR when the operator sets it.
package appdirs

import (
	"fmt"
	"os"
	"path/filepath"

	paths "github.com/hollis-labs/libs/core/util/apppaths"
)

const (
	appName = "mentat"
	// DataDirEnv overrides the whole data directory.
	DataDirEnv = "MENTAT_DATA_DIR"

	// DatabaseFile, SigningKeyFile and SecretsDir are the names inside the data directory.
	DatabaseFile   = "mentat.db"
	SigningKeyFile = "identity-signing.key"
	SecretsDir     = "secrets"
)

// Dirs are the resolved directories.
type Dirs struct {
	Data string
}

// Resolve returns the data directory. getenv is os.Getenv outside tests. XDG_DATA_HOME is
// honored through the shared apppaths layer. Nothing is created.
func Resolve(getenv func(string) string) (Dirs, error) {
	if v := getenv(DataDirEnv); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return Dirs{}, fmt.Errorf("resolve %s: %w", DataDirEnv, err)
		}
		return Dirs{Data: abs}, nil
	}
	layout, err := paths.Resolve(appName, paths.WithoutMaterialize())
	if err != nil {
		return Dirs{}, fmt.Errorf("resolve application directories: %w", err)
	}
	return Dirs{Data: layout.DataDir()}, nil
}

// Database is the SQLite file.
func (d Dirs) Database() string { return filepath.Join(d.Data, DatabaseFile) }

// SigningKey is the identity module's Ed25519 key file.
func (d Dirs) SigningKey() string { return filepath.Join(d.Data, SigningKeyFile) }

// Secrets is the managed file store for the secrets referenced by config.
func (d Dirs) Secrets() string { return filepath.Join(d.Data, SecretsDir) }

// Prepare creates the data directory (0700) and the database file (0600) when missing and
// tightens both when they exist, so private data is never readable by other users. The
// signing key and the secrets store create themselves private.
func (d Dirs) Prepare() error {
	if err := os.MkdirAll(d.Data, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(d.Data, 0o700); err != nil { //nolint:gosec // 0700 on a directory is the point: owner-only
		return err
	}
	f, err := os.OpenFile(d.Database(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
