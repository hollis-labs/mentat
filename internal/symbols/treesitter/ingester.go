//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hollis-labs/mentat/internal/symbols/model"
)

type multiIngester struct {
	store model.Store
}

func NewIngester(store model.Store) model.Ingester {
	return multiIngester{store: store}
}

func (m multiIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	languages := req.Languages
	if len(languages) == 0 {
		languages = detectLanguages(req.RepoPath)
	}

	var total model.IngestResult
	for _, language := range languages {
		var ingester model.Ingester
		switch language {
		case "go":
			ingester = NewGoIngester(m.store)
		case "typescript", "ts", "tsx":
			ingester = NewTypeScriptIngester(m.store)
		case "python", "py":
			ingester = NewPythonIngester(m.store)
		case "rust", "rs":
			ingester = NewRustIngester(m.store)
		default:
			continue
		}
		result, err := ingester.Ingest(ctx, req)
		if err != nil {
			return total, fmt.Errorf("%s tree-sitter ingest: %w", language, err)
		}
		total.Inserted += result.Inserted
		total.Updated += result.Updated
		total.Drifted += result.Drifted
	}
	return total, nil
}

func detectLanguages(repoPath string) []string {
	var languages []string
	if fileExists(filepath.Join(repoPath, "go.mod")) {
		languages = append(languages, "go")
	}
	if fileExists(filepath.Join(repoPath, "package.json")) || fileExists(filepath.Join(repoPath, "tsconfig.json")) {
		languages = append(languages, "typescript")
	}
	if fileExists(filepath.Join(repoPath, "pyproject.toml")) || fileExists(filepath.Join(repoPath, "setup.py")) || fileExists(filepath.Join(repoPath, "requirements.txt")) {
		languages = append(languages, "python")
	}
	if fileExists(filepath.Join(repoPath, "Cargo.toml")) {
		languages = append(languages, "rust")
	}
	return languages
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
