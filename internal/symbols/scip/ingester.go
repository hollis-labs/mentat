package scip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/symbols/model"
	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

type Ingester struct {
	store            model.Store
	preserveIdentity bool
}

type relationshipWriter interface {
	UpsertRelationship(item *domain.Relationship) error
	DeleteRelationshipsByRepoSource(repoID, source string) error
}

func NewIngester(store model.Store) *Ingester {
	return &Ingester{store: store}
}

func NewAugmentingIngester(store model.Store) *Ingester {
	return &Ingester{store: store, preserveIdentity: true}
}

func (i *Ingester) Ingest(ctx context.Context, req model.IngestRequest) (model.IngestResult, error) {
	if i.store == nil {
		return model.IngestResult{}, fmt.Errorf("scip ingester requires a symbol store")
	}
	languages, err := i.languagesForRequest(req)
	if err != nil {
		return model.IngestResult{}, err
	}
	if len(languages) == 0 {
		if i.preserveIdentity {
			return model.IngestResult{}, nil
		}
		return model.IngestResult{}, fmt.Errorf("no supported SCIP languages detected in %s", req.RepoPath)
	}
	if relStore, ok := i.store.(relationshipWriter); ok {
		if err := relStore.DeleteRelationshipsByRepoSource(req.RepoID, "scip"); err != nil {
			return model.IngestResult{}, err
		}
	}
	var total model.IngestResult
	for _, language := range languages {
		outPath, err := runIndexer(ctx, language, req)
		if err != nil {
			return total, err
		}
		data, err := os.ReadFile(outPath)
		if err != nil {
			return total, fmt.Errorf("read %s index: %w", language, err)
		}
		result, err := i.ingestIndex(data, req)
		if err != nil {
			return total, fmt.Errorf("ingest %s index: %w", language, err)
		}
		total.Inserted += result.Inserted
		total.Updated += result.Updated
		total.Drifted += result.Drifted
	}
	return total, nil
}

func (i *Ingester) languagesForRequest(req model.IngestRequest) ([]string, error) {
	if len(req.Languages) > 0 {
		languages := normalizeLanguages(req.Languages)
		if i.preserveIdentity {
			return filterIndexableLanguages(languages, req.RepoPath), nil
		}
		if len(languages) == 0 {
			return nil, fmt.Errorf("no supported SCIP languages requested")
		}
		return languages, nil
	}
	languages, err := detectLanguages(req)
	if err != nil {
		if i.preserveIdentity {
			return nil, nil
		}
		return nil, err
	}
	if i.preserveIdentity {
		return filterIndexableLanguages(languages, req.RepoPath), nil
	}
	return languages, nil
}

func (i *Ingester) ingestIndex(data []byte, req model.IngestRequest) (model.IngestResult, error) {
	index := &scippb.Index{}
	if err := proto.Unmarshal(data, index); err != nil {
		return model.IngestResult{}, fmt.Errorf("unmarshal scip index: %w", err)
	}

	parsed, err := collectSymbols(index, req)
	if err != nil {
		return model.IngestResult{}, err
	}

	var result model.IngestResult
	byRawSymbol := map[string]*model.Symbol{}
	parentKeys := map[int64]string{}
	for _, item := range parsed {
		if i.preserveIdentity && shouldSkipAugmentSymbol(item.symbol) {
			continue
		}
		existing, err := i.store.FindSymbolByFileQualifiedName(req.RepoID, item.symbol.FilePath, item.symbol.QualifiedName)
		if err != nil {
			return result, err
		}
		if existing != nil {
			byRawSymbol[item.rawSymbol] = existing
			item.symbol.ID = existing.ID
			if existing.ContentHash != item.symbol.ContentHash && req.CommitRef != "" {
				item.symbol.StaleSinceCommit = &req.CommitRef
				result.Drifted++
			}
			if i.preserveIdentity {
				if existing.Docstring == "" && item.symbol.Docstring != "" {
					existing.Docstring = item.symbol.Docstring
					if err := i.store.UpsertSymbol(existing); err != nil {
						return result, err
					}
					result.Updated++
				}
				continue
			}
			model.CopyMissingSymbolFields(item.symbol, existing)
			if model.SameStoredSymbol(existing, item.symbol) {
				continue
			}
			result.Updated++
		} else {
			if i.preserveIdentity {
				matched, err := i.findExistingByLocation(item.symbol)
				if err != nil {
					return result, err
				}
				if matched != nil {
					byRawSymbol[item.rawSymbol] = matched
					if matched.Docstring == "" && item.symbol.Docstring != "" {
						matched.Docstring = item.symbol.Docstring
						if err := i.store.UpsertSymbol(matched); err != nil {
							return result, err
						}
						result.Updated++
					}
					continue
				}
			}
			byHash, err := i.store.FindSymbolByFileContentHash(req.RepoID, item.symbol.FilePath, item.symbol.ContentHash)
			if err != nil {
				return result, err
			}
			if byHash != nil {
				byRawSymbol[item.rawSymbol] = byHash
				if i.preserveIdentity {
					if byHash.Docstring == "" && item.symbol.Docstring != "" {
						byHash.Docstring = item.symbol.Docstring
						if err := i.store.UpsertSymbol(byHash); err != nil {
							return result, err
						}
						result.Updated++
					}
					continue
				}
				item.symbol.ID = byHash.ID
				model.CopyMissingSymbolFields(item.symbol, byHash)
				if model.SameStoredSymbol(byHash, item.symbol) {
					continue
				}
				result.Updated++
			} else {
				result.Inserted++
			}
		}
		if err := i.store.UpsertSymbol(item.symbol); err != nil {
			return result, err
		}
		byRawSymbol[item.rawSymbol] = item.symbol
		if item.parentRawSymbol != "" {
			parentKeys[item.symbol.ID] = item.parentRawSymbol
		}
	}

	for childID, parentRaw := range parentKeys {
		child := byID(byRawSymbol, childID)
		parent := byRawSymbol[parentRaw]
		if child == nil || parent == nil {
			continue
		}
		if child.ParentSymbolID != nil && *child.ParentSymbolID == parent.ID {
			continue
		}
		child.ParentSymbolID = &parent.ID
		if err := i.store.UpsertSymbol(child); err != nil {
			return result, err
		}
	}

	if relStore, ok := i.store.(relationshipWriter); ok {
		relationships := collectRelationships(index, req.RepoID, byRawSymbol)
		relationships = append(relationships, collectStructuralRelationships(index, req.RepoID, byRawSymbol)...)
		relationships = append(relationships, collectFileRelationships(index, req.RepoID, byRawSymbol)...)
		for idx := range relationships {
			if err := relStore.UpsertRelationship(&relationships[idx]); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

type parsedSymbol struct {
	rawSymbol       string
	parentRawSymbol string
	symbol          *model.Symbol
}

func collectRelationships(index *scippb.Index, repoID string, byRawSymbol map[string]*model.Symbol) []domain.Relationship {
	now := time.Now().UTC()
	out := make([]domain.Relationship, 0)
	seen := map[string]struct{}{}
	for _, doc := range index.GetDocuments() {
		ctx := buildDocumentContext(doc, byRawSymbol)
		for _, info := range doc.GetSymbols() {
			src := byRawSymbol[symbolMapKey(doc.GetRelativePath(), info.GetSymbol())]
			if src == nil {
				continue
			}
			for _, rel := range info.GetRelationships() {
				dst := byRawSymbol[symbolMapKey(doc.GetRelativePath(), rel.GetSymbol())]
				if dst == nil {
					continue
				}
				for _, kind := range relationshipKinds(rel) {
					key := fmt.Sprintf("%d:%d:%s:%s", src.ID, dst.ID, kind, "scip")
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}
					out = append(out, domain.Relationship{
						RepoID:       repoID,
						SrcSymbolID:  src.ID,
						DstSymbolID:  dst.ID,
						Kind:         kind,
						Weight:       1.0,
						Source:       "scip",
						DiscoveredAt: now,
					})
				}
			}
			for _, occ := range info.GetSignatureDocumentation().GetOccurrences() {
				dst := byRawSymbol[symbolMapKey(doc.GetRelativePath(), occ.GetSymbol())]
				if dst == nil || dst.ID == src.ID {
					continue
				}
				appendRelationship(&out, seen, domain.Relationship{
					RepoID:       repoID,
					SrcSymbolID:  src.ID,
					DstSymbolID:  dst.ID,
					Kind:         "documents",
					Weight:       1.0,
					Source:       "scip",
					DiscoveredAt: now,
				})
			}
		}
		for _, occ := range doc.GetOccurrences() {
			if occ.GetSymbol() == "" {
				continue
			}
			if scippb.SymbolRole_Definition.Matches(occ) || scippb.SymbolRole_ForwardDefinition.Matches(occ) {
				continue
			}
			dst := byRawSymbol[symbolMapKey(doc.GetRelativePath(), occ.GetSymbol())]
			if dst == nil {
				continue
			}
			ownerRaw, owner := ctx.ownerForOccurrence(occ)
			if owner == nil || owner.ID == dst.ID {
				continue
			}
			if scippb.SymbolRole_Import.Matches(occ) {
				appendRelationship(&out, seen, domain.Relationship{
					RepoID:       repoID,
					SrcSymbolID:  owner.ID,
					DstSymbolID:  dst.ID,
					Kind:         "imports",
					Weight:       1.0,
					Source:       "scip",
					DiscoveredAt: now,
				})
			}
			if isCallableSymbol(owner) && isCallableSymbol(dst) && isCallOccurrence(occ) {
				appendRelationship(&out, seen, domain.Relationship{
					RepoID:       repoID,
					SrcSymbolID:  owner.ID,
					DstSymbolID:  dst.ID,
					Kind:         "calls",
					Weight:       1.0,
					Source:       "scip",
					DiscoveredAt: now,
				})
			}
			if ctx.isTestSymbol(ownerRaw, owner) && !ctx.isTestSymbol(symbolMapKey(doc.GetRelativePath(), occ.GetSymbol()), dst) {
				appendRelationship(&out, seen, domain.Relationship{
					RepoID:       repoID,
					SrcSymbolID:  owner.ID,
					DstSymbolID:  dst.ID,
					Kind:         "tests",
					Weight:       1.0,
					Source:       "scip",
					DiscoveredAt: now,
				})
			}
		}
	}
	return out
}

func collectStructuralRelationships(index *scippb.Index, repoID string, byRawSymbol map[string]*model.Symbol) []domain.Relationship {
	now := time.Now().UTC()
	out := make([]domain.Relationship, 0)
	seen := map[string]struct{}{}
	for _, doc := range index.GetDocuments() {
		ctx := buildDocumentContext(doc, byRawSymbol)
		for _, info := range doc.GetSymbols() {
			child := byRawSymbol[symbolMapKey(doc.GetRelativePath(), info.GetSymbol())]
			if child == nil {
				continue
			}
			parentRaw := symbolMapKey(doc.GetRelativePath(), info.GetEnclosingSymbol())
			if parentRaw == "" && ctx.fallbackRaw != "" && ctx.fallbackRaw != symbolMapKey(doc.GetRelativePath(), info.GetSymbol()) && child.Kind != "package" && child.Kind != "module" {
				parentRaw = ctx.fallbackRaw
			}
			parent := byRawSymbol[parentRaw]
			if parent == nil || parent.ID == child.ID {
				continue
			}
			appendRelationship(&out, seen, domain.Relationship{
				RepoID:       repoID,
				SrcSymbolID:  parent.ID,
				DstSymbolID:  child.ID,
				Kind:         "contains",
				Weight:       1.0,
				Source:       "scip",
				DiscoveredAt: now,
			})
		}
	}
	return out
}

func collectFileRelationships(index *scippb.Index, repoID string, byRawSymbol map[string]*model.Symbol) []domain.Relationship {
	now := time.Now().UTC()
	out := make([]domain.Relationship, 0)
	seen := map[string]struct{}{}
	for _, doc := range index.GetDocuments() {
		var items []*model.Symbol
		for _, info := range doc.GetSymbols() {
			sym := byRawSymbol[symbolMapKey(doc.GetRelativePath(), info.GetSymbol())]
			if !isStructuralSymbol(sym) {
				continue
			}
			items = append(items, sym)
		}
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				appendRelationship(&out, seen, domain.Relationship{
					RepoID:       repoID,
					SrcSymbolID:  items[i].ID,
					DstSymbolID:  items[j].ID,
					Kind:         "co-located",
					Weight:       1.0,
					Source:       "scip",
					DiscoveredAt: now,
				})
			}
		}
	}
	return out
}

func relationshipKinds(rel *scippb.Relationship) []string {
	kinds := make([]string, 0, 4)
	if rel.GetIsReference() {
		kinds = append(kinds, "references")
	}
	if rel.GetIsImplementation() {
		kinds = append(kinds, "implements")
	}
	if rel.GetIsTypeDefinition() {
		kinds = append(kinds, "type-defines")
	}
	if rel.GetIsDefinition() {
		kinds = append(kinds, "defines")
	}
	return kinds
}

type occurrenceRange struct {
	startLine int
	startChar int
	endLine   int
	endChar   int
}

type symbolRange struct {
	raw   string
	rng   occurrenceRange
	depth int
}

type documentContext struct {
	path        string
	owners      []symbolRange
	fallbackRaw string
	testSymbols map[string]struct{}
	byRawSymbol map[string]*model.Symbol
}

func buildDocumentContext(doc *scippb.Document, byRawSymbol map[string]*model.Symbol) documentContext {
	ctx := documentContext{
		path:        doc.GetRelativePath(),
		testSymbols: map[string]struct{}{},
		byRawSymbol: byRawSymbol,
	}
	for _, occ := range doc.GetOccurrences() {
		if occ.GetSymbol() == "" {
			continue
		}
		raw := symbolMapKey(doc.GetRelativePath(), occ.GetSymbol())
		isDefinition := scippb.SymbolRole_Definition.Matches(occ) || scippb.SymbolRole_ForwardDefinition.Matches(occ)
		if isDefinition && scippb.SymbolRole_Test.Matches(occ) {
			ctx.testSymbols[raw] = struct{}{}
		}
		if !isDefinition {
			continue
		}
		rng, ok := occurrenceRangeFromInts(occ.GetEnclosingRange())
		if !ok {
			rng, ok = occurrenceRangeFromInts(occ.GetRange())
			if !ok {
				continue
			}
		}
		ctx.owners = append(ctx.owners, symbolRange{
			raw:   raw,
			rng:   rng,
			depth: symbolDepth(occ.GetSymbol()),
		})
	}
	for _, info := range doc.GetSymbols() {
		raw := symbolMapKey(doc.GetRelativePath(), info.GetSymbol())
		sym := byRawSymbol[raw]
		if sym == nil {
			continue
		}
		if sym.Kind == "package" || sym.Kind == "module" {
			ctx.fallbackRaw = raw
			break
		}
	}
	return ctx
}

func (c documentContext) ownerForOccurrence(occ *scippb.Occurrence) (string, *model.Symbol) {
	rng, ok := occurrenceRangeFromInts(occ.GetEnclosingRange())
	if !ok {
		rng, ok = occurrenceRangeFromInts(occ.GetRange())
	}
	if ok {
		bestRaw := ""
		bestDepth := -1
		bestSpan := -1
		for _, owner := range c.owners {
			if owner.raw == occ.GetSymbol() {
				continue
			}
			if !owner.rng.contains(rng) {
				continue
			}
			span := owner.rng.span()
			if owner.depth > bestDepth || (owner.depth == bestDepth && (bestSpan == -1 || span < bestSpan)) {
				bestRaw = owner.raw
				bestDepth = owner.depth
				bestSpan = span
			}
		}
		if bestRaw != "" {
			return bestRaw, c.byRawSymbol[bestRaw]
		}
	}
	if c.fallbackRaw != "" {
		return c.fallbackRaw, c.byRawSymbol[c.fallbackRaw]
	}
	return "", nil
}

func (c documentContext) isTestSymbol(raw string, sym *model.Symbol) bool {
	if raw != "" {
		if _, ok := c.testSymbols[raw]; ok {
			return true
		}
	}
	if sym == nil {
		return false
	}
	if strings.Contains(strings.ToLower(sym.FilePath), "_test.") {
		return true
	}
	return strings.HasPrefix(sym.Name, "Test")
}

func occurrenceRangeFromInts(raw []int32) (occurrenceRange, bool) {
	if len(raw) < 3 {
		return occurrenceRange{}, false
	}
	rng := occurrenceRange{
		startLine: int(raw[0]),
		startChar: int(raw[1]),
		endLine:   int(raw[0]),
		endChar:   int(raw[2]),
	}
	if len(raw) >= 4 {
		rng.endLine = int(raw[2])
		rng.endChar = int(raw[3])
	}
	return rng, true
}

func (r occurrenceRange) contains(other occurrenceRange) bool {
	if other.startLine < r.startLine || other.endLine > r.endLine {
		return false
	}
	if other.startLine == r.startLine && other.startChar < r.startChar {
		return false
	}
	if other.endLine == r.endLine && other.endChar > r.endChar {
		return false
	}
	return true
}

func (r occurrenceRange) span() int {
	return ((r.endLine - r.startLine) * 100000) + (r.endChar - r.startChar)
}

func symbolDepth(raw string) int {
	raw = symbolIdentity(raw)
	parsed, err := scippb.ParseSymbol(raw)
	if err != nil {
		return 0
	}
	return len(parsed.GetDescriptors())
}

func symbolMapKey(docPath, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "local ") {
		return docPath + "::" + raw
	}
	return raw
}

func symbolIdentity(raw string) string {
	if idx := strings.Index(raw, "::local "); idx >= 0 {
		return raw[idx+2:]
	}
	return raw
}

func appendRelationship(out *[]domain.Relationship, seen map[string]struct{}, rel domain.Relationship) {
	key := fmt.Sprintf("%d:%d:%s:%s", rel.SrcSymbolID, rel.DstSymbolID, rel.Kind, rel.Source)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, rel)
}

func isCallableSymbol(sym *model.Symbol) bool {
	if sym == nil {
		return false
	}
	return sym.Kind == "function" || sym.Kind == "method"
}

func isStructuralSymbol(sym *model.Symbol) bool {
	if sym == nil {
		return false
	}
	switch sym.Kind {
	case "function", "method", "type", "module", "package":
		return true
	default:
		return false
	}
}

func isCallOccurrence(occ *scippb.Occurrence) bool {
	switch occ.GetSyntaxKind() {
	case scippb.SyntaxKind_IdentifierFunction, scippb.SyntaxKind_IdentifierFunctionDefinition:
		return true
	default:
		return false
	}
}

func collectSymbols(index *scippb.Index, req model.IngestRequest) ([]parsedSymbol, error) {
	fileCache := map[string][]string{}
	var out []parsedSymbol
	for _, doc := range index.GetDocuments() {
		definitions := definitionRanges(doc)
		for _, info := range doc.GetSymbols() {
			if info.GetSymbol() == "" {
				continue
			}
			sym, err := toStackSymbol(info, doc, definitions[info.GetSymbol()], req, fileCache)
			if err != nil {
				return nil, err
			}
			out = append(out, sym)
		}
	}
	return out, nil
}

type sourceRange struct {
	startLine int
	endLine   int
}

func definitionRanges(doc *scippb.Document) map[string]sourceRange {
	out := map[string]sourceRange{}
	for _, occ := range doc.GetOccurrences() {
		if occ.GetSymbol() == "" {
			continue
		}
		if !scippb.SymbolRole_Definition.Matches(occ) && !scippb.SymbolRole_ForwardDefinition.Matches(occ) {
			continue
		}
		rng := occ.GetEnclosingRange()
		if len(rng) == 0 {
			rng = occ.GetRange()
		}
		if len(rng) < 3 {
			continue
		}
		endLine := int(rng[0])
		if len(rng) >= 4 {
			endLine = int(rng[2])
		}
		out[occ.GetSymbol()] = sourceRange{
			startLine: int(rng[0]),
			endLine:   endLine,
		}
	}
	return out
}

func toStackSymbol(info *scippb.SymbolInformation, doc *scippb.Document, rng sourceRange, req model.IngestRequest, fileCache map[string][]string) (parsedSymbol, error) {
	parsed, err := scippb.ParseSymbol(info.GetSymbol())
	if err != nil {
		return parsedSymbol{}, fmt.Errorf("parse scip symbol %q: %w", info.GetSymbol(), err)
	}

	name := info.GetDisplayName()
	if name == "" && len(parsed.GetDescriptors()) > 0 {
		last := parsed.GetDescriptors()[len(parsed.GetDescriptors())-1]
		name = last.GetName()
	}
	if name == "" {
		name = info.GetSymbol()
	}

	descriptorOnly := scippb.DescriptorOnlyFormatter.FormatSymbol(parsed)
	packageName := ""
	if parsed.GetPackage() != nil {
		packageName = parsed.GetPackage().GetName()
	}
	qualifiedName := descriptorOnly
	if packageName != "" {
		qualifiedName = packageName + ":" + descriptorOnly
	}

	contentHash, signatureHash, err := hashForRange(req.RepoPath, doc.GetRelativePath(), rng, info, fileCache)
	if err != nil {
		return parsedSymbol{}, err
	}

	parentRaw := info.GetEnclosingSymbol()
	if parentRaw == "" && len(parsed.GetDescriptors()) > 1 {
		parent := &scippb.Symbol{
			Scheme:      parsed.GetScheme(),
			Package:     parsed.GetPackage(),
			Descriptors: parsed.GetDescriptors()[:len(parsed.GetDescriptors())-1],
		}
		parentRaw = scippb.VerboseSymbolFormatter.FormatSymbol(parent)
	}

	var lineStart, lineEnd *int
	if rng.startLine >= 0 {
		start := rng.startLine + 1
		end := rng.endLine + 1
		lineStart = &start
		lineEnd = &end
	}

	docstring := strings.TrimSpace(strings.Join(info.GetDocumentation(), "\n\n"))
	return parsedSymbol{
		rawSymbol:       symbolMapKey(doc.GetRelativePath(), info.GetSymbol()),
		parentRawSymbol: symbolMapKey(doc.GetRelativePath(), parentRaw),
		symbol: &model.Symbol{
			RepoID:        req.RepoID,
			Kind:          mapKind(info.GetKind()),
			Name:          name,
			QualifiedName: qualifiedName,
			FilePath:      doc.GetRelativePath(),
			LineStart:     lineStart,
			LineEnd:       lineEnd,
			ContentHash:   contentHash,
			SignatureHash: signatureHash,
			Language:      doc.GetLanguage(),
			Visibility:    inferVisibility(name),
			Docstring:     docstring,
		},
	}, nil
}

func hashForRange(repoPath, relativePath string, rng sourceRange, info *scippb.SymbolInformation, fileCache map[string][]string) (string, string, error) {
	snippet, err := fileSnippet(repoPath, relativePath, rng, fileCache)
	if err != nil {
		return "", "", err
	}
	if snippet == "" {
		snippet = info.GetSymbol()
	}
	signature := strings.TrimSpace(info.GetSignatureDocumentation().GetText())
	if signature == "" {
		signature = firstNonEmptyLine(snippet)
	}
	if signature == "" {
		signature = info.GetDisplayName()
	}
	return hashText(snippet), hashText(signature), nil
}

func fileSnippet(repoPath, relativePath string, rng sourceRange, fileCache map[string][]string) (string, error) {
	if rng.startLine < 0 || relativePath == "" {
		return "", nil
	}
	lines, ok := fileCache[relativePath]
	if !ok {
		path := filepath.Join(repoPath, relativePath)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", fmt.Errorf("read source file %s: %w", relativePath, err)
		}
		lines = strings.Split(string(data), "\n")
		fileCache[relativePath] = lines
	}
	if rng.startLine >= len(lines) {
		return "", nil
	}
	end := rng.endLine
	if end < rng.startLine {
		end = rng.startLine
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	var picked []string
	for idx := rng.startLine; idx <= end; idx++ {
		picked = append(picked, strings.TrimRight(lines[idx], " \t\r"))
	}
	return strings.TrimSpace(strings.Join(picked, "\n")), nil
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func mapKind(kind scippb.SymbolInformation_Kind) string {
	switch kind {
	case scippb.SymbolInformation_Function:
		return "function"
	case scippb.SymbolInformation_Method, scippb.SymbolInformation_MethodAlias, scippb.SymbolInformation_MethodReceiver, scippb.SymbolInformation_MethodSpecification:
		return "method"
	case scippb.SymbolInformation_Class, scippb.SymbolInformation_Struct, scippb.SymbolInformation_Interface, scippb.SymbolInformation_Trait, scippb.SymbolInformation_Object:
		return "type"
	case scippb.SymbolInformation_Constant, scippb.SymbolInformation_EnumMember:
		return "const"
	case scippb.SymbolInformation_Variable, scippb.SymbolInformation_Field, scippb.SymbolInformation_Parameter:
		return "var"
	case scippb.SymbolInformation_Module, scippb.SymbolInformation_Namespace:
		return "module"
	case scippb.SymbolInformation_Package:
		return "package"
	default:
		return strings.ToLower(kind.String())
	}
}

func inferVisibility(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) {
			if unicode.IsUpper(r) {
				return "public"
			}
			return "private"
		}
	}
	return ""
}

func detectLanguages(req model.IngestRequest) ([]string, error) {
	if len(req.Languages) > 0 {
		languages := normalizeLanguages(req.Languages)
		if len(languages) == 0 {
			return nil, fmt.Errorf("no supported SCIP languages requested")
		}
		return languages, nil
	}
	var out []string
	if fileExists(filepath.Join(req.RepoPath, "go.mod")) {
		out = append(out, "go")
	}
	if fileExists(filepath.Join(req.RepoPath, "package.json")) || fileExists(filepath.Join(req.RepoPath, "tsconfig.json")) {
		out = append(out, "typescript")
	}
	if fileExists(filepath.Join(req.RepoPath, "pyproject.toml")) || fileExists(filepath.Join(req.RepoPath, "setup.py")) || fileExists(filepath.Join(req.RepoPath, "requirements.txt")) {
		out = append(out, "python")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no supported SCIP languages detected in %s", req.RepoPath)
	}
	return out, nil
}

func normalizeLanguages(languages []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, language := range languages {
		switch strings.ToLower(strings.TrimSpace(language)) {
		case "go":
			if _, ok := seen["go"]; !ok {
				seen["go"] = struct{}{}
				out = append(out, "go")
			}
		case "typescript", "ts", "javascript", "js":
			if _, ok := seen["typescript"]; !ok {
				seen["typescript"] = struct{}{}
				out = append(out, "typescript")
			}
		case "python", "py":
			if _, ok := seen["python"]; !ok {
				seen["python"] = struct{}{}
				out = append(out, "python")
			}
		}
	}
	return out
}

func filterIndexableLanguages(languages []string, repoPath string) []string {
	var out []string
	for _, language := range languages {
		switch language {
		case "go":
			if fileExists(filepath.Join(repoPath, "go.mod")) {
				out = append(out, language)
			}
		case "typescript":
			if fileExists(filepath.Join(repoPath, "tsconfig.json")) {
				out = append(out, language)
			}
		case "python":
			if fileExists(filepath.Join(repoPath, "pyproject.toml")) || fileExists(filepath.Join(repoPath, "setup.py")) || fileExists(filepath.Join(repoPath, "requirements.txt")) {
				out = append(out, language)
			}
		}
	}
	return out
}

func runIndexer(ctx context.Context, language string, req model.IngestRequest) (string, error) {
	tmpDir, err := os.MkdirTemp("", "stack-explorer-scip-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	outPath := filepath.Join(tmpDir, language+".scip")
	var name string
	var args []string
	switch language {
	case "go":
		name = "scip-go"
		args = []string{"index", "--module-root", req.RepoPath, "--output", outPath, "./..."}
	case "typescript", "ts", "javascript", "js":
		name = "scip-typescript"
		args = []string{"index", "--cwd", req.RepoPath, "--output", outPath}
	case "python", "py":
		name = "scip-python"
		args = []string{"index", "--cwd", req.RepoPath, "--project-name", req.RepoID, "--output", outPath}
	default:
		return "", fmt.Errorf("unsupported SCIP language %q", language)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = req.RepoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return outPath, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func byID(items map[string]*model.Symbol, id int64) *model.Symbol {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

func (i *Ingester) findExistingByLocation(sym *model.Symbol) (*model.Symbol, error) {
	query := normalizeSymbolName(sym.Name)
	if query == "" {
		query = sym.Name
	}
	items, err := i.store.SearchSymbols(model.SearchFilter{
		RepoID: sym.RepoID,
		Query:  query,
		Limit:  50,
	})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.FilePath != sym.FilePath {
			continue
		}
		if !sameSymbolName(item.Name, sym.Name) {
			continue
		}
		if sym.Kind != "" && sym.Kind != "unspecifiedkind" && item.Kind != sym.Kind {
			continue
		}
		if item.LineStart != nil && sym.LineStart != nil && *item.LineStart == *sym.LineStart {
			candidate := item
			return &candidate, nil
		}
	}
	return nil, nil
}

func sameSymbolName(left, right string) bool {
	return normalizeSymbolName(left) == normalizeSymbolName(right)
}

func normalizeSymbolName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "()")
	return strings.TrimSpace(name)
}

func shouldSkipAugmentSymbol(sym *model.Symbol) bool {
	return sym.Kind == "unspecifiedkind" && strings.HasSuffix(sym.QualifiedName, "/")
}
