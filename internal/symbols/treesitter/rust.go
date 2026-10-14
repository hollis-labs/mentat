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
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

type RustIngester struct {
	store model.Store
}

func NewRustIngester(store model.Store) *RustIngester {
	return &RustIngester{store: store}
}

func (g *RustIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	if g.store == nil {
		return model.IngestResult{}, fmt.Errorf("tree-sitter rust ingester requires a symbol store")
	}
	var result model.IngestResult
	err := filepath.WalkDir(req.RepoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".worktrees" || d.Name() == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if filepath.Ext(path) != ".rs" {
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
		found, err := ParseRustFile(req.RepoID, filepath.ToSlash(relativePath), src)
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

func ParseRustFile(repoID, relativePath string, src []byte) ([]*model.Symbol, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_rust.Language()))); err != nil {
		return nil, fmt.Errorf("set rust language: %w", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	var out []*model.Symbol
	for _, child := range root.NamedChildren(cursor) {
		out = append(out, collectRustNode(repoID, relativePath, src, "", child)...)
	}
	return out, nil
}

func collectRustNode(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	switch node.Kind() {
	case "function_item":
		name := utf8Field(node, "name", src)
		if name == "" {
			return nil
		}
		return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, node, "function", rustQualified(prefix, name), name, "rust")}
	case "struct_item", "enum_item", "trait_item", "type_item", "union_item":
		name := utf8Field(node, "name", src)
		if name == "" {
			return nil
		}
		out := []*model.Symbol{
			newTreeSitterSymbol(repoID, relativePath, src, node, "type", rustQualified(prefix, name), name, "rust"),
		}
		if node.Kind() == "trait_item" {
			out = append(out, collectRustTraitMethods(repoID, relativePath, src, rustQualified(prefix, name), node)...)
		}
		return out
	case "const_item":
		name := utf8Field(node, "name", src)
		if name == "" {
			return nil
		}
		return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, node, "const", rustQualified(prefix, name), name, "rust")}
	case "static_item":
		name := utf8Field(node, "name", src)
		if name == "" {
			return nil
		}
		return []*model.Symbol{newTreeSitterSymbol(repoID, relativePath, src, node, "var", rustQualified(prefix, name), name, "rust")}
	case "mod_item":
		name := utf8Field(node, "name", src)
		if name == "" {
			return nil
		}
		out := []*model.Symbol{
			newTreeSitterSymbol(repoID, relativePath, src, node, "module", rustQualified(prefix, name), name, "rust"),
		}
		body := node.ChildByFieldName("body")
		if body == nil {
			return out
		}
		cursor := body.Walk()
		defer cursor.Close()
		for _, child := range body.NamedChildren(cursor) {
			out = append(out, collectRustNode(repoID, relativePath, src, rustQualified(prefix, name), child)...)
		}
		return out
	case "impl_item":
		return collectRustImpl(repoID, relativePath, src, prefix, node)
	}
	return nil
}

func collectRustImpl(repoID, relativePath string, src []byte, prefix string, node tree_sitter.Node) []*model.Symbol {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return nil
	}
	receiver := rustTypeName(typeNode.Utf8Text(src))
	if receiver == "" {
		return nil
	}
	qualifiedReceiver := rustQualified(prefix, receiver)
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	cursor := body.Walk()
	defer cursor.Close()
	var out []*model.Symbol
	for _, child := range body.NamedChildren(cursor) {
		if child.Kind() != "function_item" {
			continue
		}
		name := utf8Field(child, "name", src)
		if name == "" {
			continue
		}
		out = append(out, newTreeSitterSymbol(repoID, relativePath, src, child, "method", qualifiedReceiver+"::"+name, name, "rust"))
	}
	return out
}

func collectRustTraitMethods(repoID, relativePath string, src []byte, qualifiedTrait string, node tree_sitter.Node) []*model.Symbol {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	cursor := body.Walk()
	defer cursor.Close()
	var out []*model.Symbol
	for _, child := range body.NamedChildren(cursor) {
		if child.Kind() != "function_item" && child.Kind() != "function_signature_item" {
			continue
		}
		name := utf8Field(child, "name", src)
		if name == "" {
			continue
		}
		out = append(out, newTreeSitterSymbol(repoID, relativePath, src, child, "method", qualifiedTrait+"::"+name, name, "rust"))
	}
	return out
}

func rustQualified(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "::" + name
}

func rustTypeName(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "&")
	text = strings.TrimSpace(strings.TrimPrefix(text, "mut "))
	if idx := strings.Index(text, "<"); idx >= 0 {
		text = text[:idx]
	}
	text = strings.TrimPrefix(text, "self::")
	text = strings.TrimPrefix(text, "crate::")
	if idx := strings.LastIndex(text, "::"); idx >= 0 {
		text = text[idx+2:]
	}
	return strings.TrimSpace(text)
}
