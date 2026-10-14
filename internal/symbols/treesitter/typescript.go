//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/hollis-labs/mentat/internal/symbols/model"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type TypeScriptIngester struct {
	store model.Store
}

func NewTypeScriptIngester(store model.Store) *TypeScriptIngester {
	return &TypeScriptIngester{store: store}
}

func (g *TypeScriptIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	if g.store == nil {
		return model.IngestResult{}, fmt.Errorf("tree-sitter typescript ingester requires a symbol store")
	}
	var result model.IngestResult
	err := filepath.WalkDir(req.RepoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".worktrees" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".tsx" {
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
		found, err := ParseTypeScriptFile(req.RepoID, filepath.ToSlash(relativePath), src, ext == ".tsx")
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

func ParseTypeScriptFile(repoID, relativePath string, src []byte, tsx bool) ([]*model.Symbol, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	var lang unsafe.Pointer
	if tsx {
		lang = tree_sitter_ts.LanguageTSX()
	} else {
		lang = tree_sitter_ts.LanguageTypescript()
	}
	if err := parser.SetLanguage(tree_sitter.NewLanguage(lang)); err != nil {
		return nil, fmt.Errorf("set typescript language: %w", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	var out []*model.Symbol
	for _, child := range root.NamedChildren(cursor) {
		out = append(out, collectTypeScriptNode(repoID, relativePath, src, "", child)...)
	}
	return out, nil
}

func collectTypeScriptNode(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	switch node.Kind() {
	case "export_statement":
		if node.NamedChildCount() > 0 {
			return collectTypeScriptNode(repoID, relativePath, src, prefix, *node.NamedChild(0))
		}
	case "class_declaration":
		return collectTypeScriptClass(repoID, relativePath, src, prefix, node)
	case "function_declaration":
		name := utf8Field(node, "name", src)
		return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, node, "function", tsQualified(prefix, name), name, "typescript")}
	case "lexical_declaration":
		return collectTypeScriptVariables(repoID, relativePath, src, prefix, node)
	}
	return nil
}

func collectTypeScriptClass(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	name := utf8Field(node, "name", src)
	if name == "" {
		return nil
	}
	qualified := tsQualified(prefix, name)
	out := []*model.Symbol{
		newTreeSitterSymbol(repoID, relativePath, src, node, "type", qualified, name, "typescript"),
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return out
	}
	cursor := body.Walk()
	defer cursor.Close()
	for _, child := range body.NamedChildren(cursor) {
		if child.Kind() != "method_definition" {
			continue
		}
		methodName := utf8Field(child, "name", src)
		if methodName == "" {
			continue
		}
		out = append(out, newTreeSitterSymbol(repoID, relativePath, src, child, "method", qualified+"."+methodName, methodName, "typescript"))
	}
	return out
}

func collectTypeScriptVariables(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	cursor := node.Walk()
	defer cursor.Close()
	var out []*model.Symbol
	for _, child := range node.NamedChildren(cursor) {
		if child.Kind() != "variable_declarator" {
			continue
		}
		name := utf8Field(child, "name", src)
		if name == "" {
			continue
		}
		out = append(out, newTreeSitterSymbol(repoID, relativePath, src, child, "var", tsQualified(prefix, name), name, "typescript"))
	}
	return out
}

func tsQualified(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func utf8Field(node tree_sitter.Node, field string, src []byte) string {
	if child := node.ChildByFieldName(field); child != nil {
		return strings.TrimSpace(child.Utf8Text(src))
	}
	return ""
}
