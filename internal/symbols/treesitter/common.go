//go:build treesitter

package treesitter

import (
	"bytes"
	"strings"

	"github.com/hollis-labs/mentat/internal/symbols/model"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func newTreeSitterSymbol(repoID, relativePath string, src []byte, node tree_sitter.Node, kind, qualifiedName, name, language string) *model.Symbol {
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1
	content := strings.TrimSpace(node.Utf8Text(src))
	signature := signatureText(node, src)
	return &model.Symbol{
		RepoID:        repoID,
		Kind:          kind,
		Name:          name,
		QualifiedName: qualifiedName,
		FilePath:      relativePath,
		LineStart:     &startLine,
		LineEnd:       &endLine,
		ContentHash:   hashText(content),
		SignatureHash: hashText(signature),
		Language:      language,
		Visibility:    inferVisibility(name),
	}
}

func signatureText(node tree_sitter.Node, src []byte) string {
	if body := node.ChildByFieldName("body"); body != nil {
		start, _ := node.ByteRange()
		bodyStart, _ := body.ByteRange()
		if bodyStart > start {
			return strings.TrimSpace(string(bytes.TrimSpace(src[start:bodyStart])))
		}
	}
	return strings.TrimSpace(node.Utf8Text(src))
}
