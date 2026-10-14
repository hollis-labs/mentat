//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hollis-labs/mentat/internal/symbols/model"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_py "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

type PythonIngester struct {
	store model.Store
}

func NewPythonIngester(store model.Store) *PythonIngester {
	return &PythonIngester{store: store}
}

func (g *PythonIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	if g.store == nil {
		return model.IngestResult{}, fmt.Errorf("tree-sitter python ingester requires a symbol store")
	}
	var result model.IngestResult
	err := filepath.WalkDir(req.RepoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".worktrees" || d.Name() == "__pycache__" || d.Name() == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if filepath.Ext(path) != ".py" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		relativePath, err := filepath.Rel(req.RepoPath, path)
		if err != nil {
			return fmt.Errorf("rel path %s: %w", path, err)
		}
		found, err := ParsePythonFile(req.RepoID, filepath.ToSlash(relativePath), src)
		if err != nil {
			return err
		}
		for _, sym := range found {
			existing, err := g.store.FindSymbolByFileQualifiedName(req.RepoID, sym.FilePath, sym.QualifiedName)
			if err != nil {
				return err
			}
			if existing != nil {
				sym.ID = existing.ID
				if existing.ContentHash != sym.ContentHash && req.CommitRef != "" {
					sym.StaleSinceCommit = &req.CommitRef
					result.Drifted++
				}
				model.CopyMissingSymbolFields(sym, existing)
				if model.SameStoredSymbol(existing, sym) {
					continue
				}
				result.Updated++
			} else {
				byHash, err := g.store.FindSymbolByFileContentHash(req.RepoID, sym.FilePath, sym.ContentHash)
				if err != nil {
					return err
				}
				if byHash != nil {
					sym.ID = byHash.ID
					model.CopyMissingSymbolFields(sym, byHash)
					if model.SameStoredSymbol(byHash, sym) {
						continue
					}
					result.Updated++
				} else {
					result.Inserted++
				}
			}
			if err := g.store.UpsertSymbol(sym); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func ParsePythonFile(repoID, relativePath string, src []byte) ([]*model.Symbol, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_py.Language())); err != nil {
		return nil, fmt.Errorf("set python language: %w", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	var out []*model.Symbol
	for _, child := range root.NamedChildren(cursor) {
		out = append(out, collectPythonNode(repoID, relativePath, src, "", child)...)
	}
	return out, nil
}

func collectPythonNode(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	switch node.Kind() {
	case "class_definition":
		return collectPythonClass(repoID, relativePath, src, prefix, node)
	case "function_definition":
		name := utf8Field(node, "name", src)
		return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, node, "function", pyQualified(prefix, name), name, "python")}
	case "expression_statement":
		if child := node.NamedChild(0); child != nil && child.Kind() == "assignment" {
			if left := child.ChildByFieldName("left"); left != nil && left.Kind() == "identifier" {
				name := left.Utf8Text(src)
				return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, *child, "var", pyQualified(prefix, name), name, "python")}
			}
		}
	}
	return nil
}

func collectPythonClass(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	name := utf8Field(node, "name", src)
	if name == "" {
		return nil
	}
	qualified := pyQualified(prefix, name)
	out := []*model.Symbol{
		newTreeSitterSymbol(repoID, relativePath, src, node, "type", qualified, name, "python"),
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return out
	}
	cursor := body.Walk()
	defer cursor.Close()
	for _, child := range body.NamedChildren(cursor) {
		if child.Kind() != "function_definition" {
			continue
		}
		methodName := utf8Field(child, "name", src)
		if methodName == "" {
			continue
		}
		out = append(out, newTreeSitterSymbol(repoID, relativePath, src, child, "method", qualified+"."+methodName, methodName, "python"))
	}
	return out
}

func pyQualified(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
