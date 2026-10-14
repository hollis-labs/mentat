package scip

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func TestCollectSymbolsFromIndex(t *testing.T) {
	repoPath := t.TempDir()
	source := "package demo\n\nfunc Hello(name string) string {\n\treturn name\n}\n"
	sourcePath := filepath.Join(repoPath, "hello.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	rawSymbol := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{
			{Name: "Hello", Suffix: scippb.Descriptor_Method},
		},
	})

	index := &scippb.Index{
		Documents: []*scippb.Document{
			{
				Language:     "go",
				RelativePath: "hello.go",
				Symbols: []*scippb.SymbolInformation{
					{
						Symbol:      rawSymbol,
						DisplayName: "Hello",
						Kind:        scippb.SymbolInformation_Function,
						Documentation: []string{
							"Hello returns the provided name.",
						},
						SignatureDocumentation: &scippb.Document{Language: "go", Text: "func Hello(name string) string"},
					},
				},
				Occurrences: []*scippb.Occurrence{
					{
						Range:       []int32{2, 0, 2, 5},
						Symbol:      rawSymbol,
						SymbolRoles: int32(scippb.SymbolRole_Definition),
					},
				},
			},
		},
	}
	data, err := proto.Marshal(index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}

	parsed, err := collectSymbols(&scippb.Index{Documents: index.Documents}, model.IngestRequest{
		RepoID:   "demo",
		RepoPath: repoPath,
	})
	if err != nil {
		t.Fatalf("collect symbols: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(parsed) = %d, want 1", len(parsed))
	}
	got := parsed[0].symbol
	if got.QualifiedName == "" || got.ContentHash == "" || got.SignatureHash == "" {
		t.Fatalf("unexpected parsed symbol: %#v", got)
	}
	if got.LineStart == nil || *got.LineStart != 3 {
		t.Fatalf("line start = %#v, want 3", got.LineStart)
	}
	_ = data
}

func TestCollectRelationshipsFromIndex(t *testing.T) {
	pkgRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "demo", Suffix: scippb.Descriptor_Package}},
	})
	srcRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "TestDog", Suffix: scippb.Descriptor_Method}},
	})
	dstRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "FeedDog", Suffix: scippb.Descriptor_Method}},
	})
	importRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "fmt",
			Version: "v1.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "Println", Suffix: scippb.Descriptor_Method}},
	})

	index := &scippb.Index{
		Documents: []*scippb.Document{{
			Language:     "go",
			RelativePath: "dog.go",
			Symbols: []*scippb.SymbolInformation{
				{
					Symbol:      pkgRaw,
					DisplayName: "demo",
					Kind:        scippb.SymbolInformation_Package,
				},
				{
					Symbol:      srcRaw,
					DisplayName: "TestDog",
					Kind:        scippb.SymbolInformation_Function,
					Relationships: []*scippb.Relationship{{
						Symbol:           dstRaw,
						IsImplementation: true,
						IsReference:      true,
						IsTypeDefinition: true,
						IsDefinition:     true,
					}},
					SignatureDocumentation: &scippb.Document{
						Language: "go",
						Text:     "func TestDog() { FeedDog() }",
						Occurrences: []*scippb.Occurrence{{
							Symbol: dstRaw,
						}},
					},
				},
				{
					Symbol:      dstRaw,
					DisplayName: "FeedDog",
					Kind:        scippb.SymbolInformation_Function,
				},
				{
					Symbol:      importRaw,
					DisplayName: "Println",
					Kind:        scippb.SymbolInformation_Function,
				},
			},
			Occurrences: []*scippb.Occurrence{
				{
					Range:       []int32{0, 0, 9, 0},
					Symbol:      pkgRaw,
					SymbolRoles: int32(scippb.SymbolRole_Definition),
				},
				{
					Range:       []int32{3, 0, 6, 0},
					Symbol:      srcRaw,
					SymbolRoles: int32(scippb.SymbolRole_Definition | scippb.SymbolRole_Test),
				},
				{
					Range:       []int32{8, 0, 8, 7},
					Symbol:      dstRaw,
					SymbolRoles: int32(scippb.SymbolRole_Definition),
				},
				{
					Range:          []int32{1, 7, 1, 14},
					EnclosingRange: []int32{0, 0, 9, 0},
					Symbol:         importRaw,
					SymbolRoles:    int32(scippb.SymbolRole_Import | scippb.SymbolRole_ReadAccess),
				},
				{
					Range:          []int32{4, 1, 4, 8},
					EnclosingRange: []int32{3, 0, 6, 0},
					Symbol:         dstRaw,
					SymbolRoles:    int32(scippb.SymbolRole_ReadAccess | scippb.SymbolRole_Test),
					SyntaxKind:     scippb.SyntaxKind_IdentifierFunction,
				},
			},
		}},
	}

	got := collectRelationships(index, "demo", map[string]*model.Symbol{
		pkgRaw:    {ID: 1, RepoID: "demo", Kind: "package", Name: "demo", FilePath: "dog.go"},
		srcRaw:    {ID: 2, RepoID: "demo", Kind: "function", Name: "TestDog", FilePath: "dog_test.go"},
		dstRaw:    {ID: 3, RepoID: "demo", Kind: "function", Name: "FeedDog", FilePath: "dog.go"},
		importRaw: {ID: 4, RepoID: "demo", Kind: "function", Name: "Println", FilePath: "dog.go"},
	})
	assertRelationship := func(srcID, dstID int64, kind string) {
		t.Helper()
		for _, rel := range got {
			if rel.Kind == kind && rel.SrcSymbolID == srcID && rel.DstSymbolID == dstID && rel.Source == "scip" && rel.Weight == 1.0 && !rel.DiscoveredAt.Equal(time.Time{}) {
				return
			}
		}
		t.Fatalf("relationship kind %q missing in %#v", kind, got)
	}
	assertRelationship(2, 3, "references")
	assertRelationship(2, 3, "implements")
	assertRelationship(2, 3, "type-defines")
	assertRelationship(2, 3, "defines")
	assertRelationship(2, 3, "documents")
	assertRelationship(2, 3, "calls")
	assertRelationship(2, 3, "tests")
	assertRelationship(1, 4, "imports")
	_ = domain.Relationship{}
}

func TestCollectStructuralRelationshipsFromIndex(t *testing.T) {
	pkgRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "demo", Suffix: scippb.Descriptor_Package}},
	})
	fnRaw := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{{Name: "Hello", Suffix: scippb.Descriptor_Method}},
	})

	index := &scippb.Index{
		Documents: []*scippb.Document{{
			Language:     "go",
			RelativePath: "hello.go",
			Symbols: []*scippb.SymbolInformation{
				{Symbol: pkgRaw, DisplayName: "demo", Kind: scippb.SymbolInformation_Package},
				{Symbol: fnRaw, DisplayName: "Hello", Kind: scippb.SymbolInformation_Function},
			},
			Occurrences: []*scippb.Occurrence{
				{Range: []int32{0, 0, 4, 0}, Symbol: pkgRaw, SymbolRoles: int32(scippb.SymbolRole_Definition)},
				{Range: []int32{2, 0, 2, 5}, EnclosingRange: []int32{0, 0, 4, 0}, Symbol: fnRaw, SymbolRoles: int32(scippb.SymbolRole_Definition)},
			},
		}},
	}

	got := collectStructuralRelationships(index, "demo", map[string]*model.Symbol{
		pkgRaw: {ID: 1, RepoID: "demo", Kind: "package", Name: "demo", FilePath: "hello.go"},
		fnRaw:  {ID: 2, RepoID: "demo", Kind: "function", Name: "Hello", FilePath: "hello.go"},
	})
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Kind != "contains" || got[0].SrcSymbolID != 1 || got[0].DstSymbolID != 2 || got[0].Source != "scip" {
		t.Fatalf("unexpected relationship: %#v", got[0])
	}
}

func TestAugmentingIngesterNoOpWhenUnchanged(t *testing.T) {
	repoPath := t.TempDir()
	source := "package demo\n\nfunc Hello(name string) string {\n\treturn name\n}\n"
	sourcePath := filepath.Join(repoPath, "hello.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	rawSymbol := scippb.VerboseSymbolFormatter.FormatSymbol(&scippb.Symbol{
		Scheme: "scip-go",
		Package: &scippb.Package{
			Manager: "gomod",
			Name:    "example.com/demo",
			Version: "v0.0.0",
		},
		Descriptors: []*scippb.Descriptor{
			{Name: "Hello", Suffix: scippb.Descriptor_Method},
		},
	})

	index := &scippb.Index{
		Documents: []*scippb.Document{
			{
				Language:     "go",
				RelativePath: "hello.go",
				Symbols: []*scippb.SymbolInformation{
					{
						Symbol:      rawSymbol,
						DisplayName: "Hello",
						Kind:        scippb.SymbolInformation_Function,
						Documentation: []string{
							"Hello returns the provided name.",
						},
						SignatureDocumentation: &scippb.Document{Language: "go", Text: "func Hello(name string) string"},
					},
				},
				Occurrences: []*scippb.Occurrence{
					{
						Range:       []int32{2, 0, 2, 5},
						Symbol:      rawSymbol,
						SymbolRoles: int32(scippb.SymbolRole_Definition),
					},
				},
			},
		},
	}
	data, err := proto.Marshal(index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "symbols.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	if err := store.CreateRepo(&domain.Repo{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	ingester := NewAugmentingIngester(store)
	req := model.IngestRequest{
		RepoID:   "demo",
		RepoPath: repoPath,
	}

	first, err := ingester.ingestIndex(data, req)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Inserted == 0 {
		t.Fatalf("first ingest inserted = %d, want > 0", first.Inserted)
	}

	second, err := ingester.ingestIndex(data, req)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 0 || second.Drifted != 0 {
		t.Fatalf("second ingest = %#v, want zero-diff result", second)
	}

	items, err := store.SearchSymbols(model.SearchFilter{RepoID: "demo", Query: "Hello", Limit: 10})
	if err != nil {
		t.Fatalf("search symbols: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Docstring == "" {
		t.Fatalf("docstring missing after augmenting ingest")
	}

	_ = context.Background()
}
