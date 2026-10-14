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
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

type GoIngester struct {
	store model.Store
}

func NewGoIngester(store model.Store) *GoIngester {
	return &GoIngester{store: store}
}

func (g *GoIngester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	if g.store == nil {
		return model.IngestResult{}, fmt.Errorf("tree-sitter go ingester requires a symbol store")
	}
	var result model.IngestResult
	err := filepath.WalkDir(req.RepoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".worktrees" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if filepath.Ext(path) != ".go" {
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
		found, err := ParseGoFile(req.RepoID, filepath.ToSlash(relativePath), src)
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

func ParseGoFile(repoID, relativePath string, src []byte) ([]*model.Symbol, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return nil, fmt.Errorf("set go language: %w", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()

	var packageName string
	var out []*model.Symbol
	for _, child := range root.NamedChildren(cursor) {
		switch child.Kind() {
		case "package_clause":
			if child.NamedChildCount() > 0 {
				packageName = child.NamedChild(0).Utf8Text(src)
			}
		case "function_declaration":
			out = append(out, newGoSymbol(repoID, relativePath, src, packageName, child, "function", funcName(child, src), ""))
		case "method_declaration":
			out = append(out, newGoSymbol(repoID, relativePath, src, packageName, child, "method", methodName(child, src), receiverType(child, src)))
		case "type_declaration":
			out = append(out, collectSpecs(repoID, relativePath, src, packageName, child, "type")...)
		case "const_declaration":
			out = append(out, collectSpecs(repoID, relativePath, src, packageName, child, "const")...)
		case "var_declaration":
			out = append(out, collectSpecs(repoID, relativePath, src, packageName, child, "var")...)
		}
	}
	return out, nil
}

func collectSpecs(repoID, relativePath string, src []byte, packageName string, decl tree_sitter.Node, kind string) []*model.Symbol {
	cursor := decl.Walk()
	defer cursor.Close()
	var out []*model.Symbol
	for _, child := range decl.NamedChildren(cursor) {
		switch child.Kind() {
		case "type_spec":
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				name := nameNode.Utf8Text(src)
				out = append(out, newGoSymbol(repoID, relativePath, src, packageName, *nameNode, kind, name, ""))
			}
		case "const_spec", "var_spec":
			for _, nameNode := range child.ChildrenByFieldName("name", cursor) {
				name := nameNode.Utf8Text(src)
				out = append(out, newGoSymbol(repoID, relativePath, src, packageName, nameNode, kind, name, ""))
			}
		}
	}
	return out
}

func newGoSymbol(repoID, relativePath string, src []byte, packageName string, node tree_sitter.Node, kind, name, receiver string) *model.Symbol {
	qualifiedName := packageName + "." + name
	if receiver != "" {
		qualifiedName = packageName + "." + receiver + "." + name
	}
	return newTreeSitterSymbol(repoID, relativePath, src, node, kind, qualifiedName, name, "go")
}

func funcName(node tree_sitter.Node, src []byte) string {
	if name := node.ChildByFieldName("name"); name != nil {
		return name.Utf8Text(src)
	}
	return ""
}

func methodName(node tree_sitter.Node, src []byte) string {
	if name := node.ChildByFieldName("name"); name != nil {
		return name.Utf8Text(src)
	}
	return ""
}

func receiverType(node tree_sitter.Node, src []byte) string {
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil || receiver.NamedChildCount() == 0 {
		return ""
	}
	param := receiver.NamedChild(0)
	if param == nil {
		return ""
	}
	typeNode := param.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	switch typeNode.Kind() {
	case "pointer_type":
		if inner := typeNode.NamedChild(0); inner != nil {
			return inner.Utf8Text(src)
		}
	default:
		return typeNode.Utf8Text(src)
	}
	return ""
}
