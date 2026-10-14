package semcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServerHelper(t *testing.T) {
	if os.Getenv("STACK_EXPLORER_TEST_MCP_SERVER") != "1" {
		t.Skip("helper process")
	}
	dbPath := os.Getenv("STACK_EXPLORER_TEST_DB")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobSvc := jobs.NewService(jobs.Config{Store: store, Workers: 1})
	if err := jobSvc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer jobSvc.Close()
	if err := ServeStdio(context.Background(), store, jobSvc); err != nil {
		t.Fatal(err)
	}
}

func TestStdioRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stack-explorer.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedIntegrationFixture(store); err != nil {
		t.Fatal(err)
	}
	store.Close()

	cmd := exec.Command("go", "run", "./cmd/mentat", "--db", dbPath, "mcp", "--transport", "stdio")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(),
		"STACK_EXPLORER_MCP_ACTOR_ID=integration-client",
		"STACK_EXPLORER_MCP_MODEL=gpt-test",
	)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "integration-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	fileRes := callToolMap(t, session, "prior_art_for_file", map[string]any{
		"repo_id": "nanite",
		"path":    "internal/chat/engine.go",
	})
	if got := fileRes["path"]; got != "internal/chat/engine.go" {
		t.Fatalf("prior_art_for_file path = %v", got)
	}
	if findings := fileRes["findings"].([]any); len(findings) == 0 {
		t.Fatalf("prior_art_for_file returned no findings")
	}

	symbolRes := callToolMap(t, session, "prior_art_for_symbol", map[string]any{
		"repo_id":        "nanite",
		"qualified_name": "nanite.Engine.Run",
	})
	if symbolRes["symbol"] == nil {
		t.Fatalf("prior_art_for_symbol returned no symbol")
	}

	lookupRes := callToolItems(t, session, "symbol_lookup", map[string]any{
		"repo_id":           "nanite",
		"query":             "Engine.Run",
		"limit":             5,
		"include_neighbors": true,
		"neighbor_kind":     "references",
		"neighbor_source":   "scip",
		"neighbor_limit":    5,
	})
	if len(lookupRes) == 0 {
		t.Fatalf("symbol_lookup returned no results")
	}
	firstLookup, ok := lookupRes[0].(map[string]any)
	if !ok {
		t.Fatalf("symbol_lookup returned unexpected item shape: %#v", lookupRes[0])
	}
	if neighbors, ok := firstLookup["neighbors"].([]any); !ok || len(neighbors) == 0 {
		t.Fatalf("symbol_lookup returned no neighbors: %#v", firstLookup)
	}

	graphRes := callToolItems(t, session, "graph_neighbors", map[string]any{
		"repo_id":        "nanite",
		"qualified_name": "nanite.Engine.Run",
		"kind":           "references",
		"source":         "scip",
		"limit":          5,
	})
	if len(graphRes) == 0 {
		t.Fatalf("graph_neighbors returned no results")
	}

	auditRes := callToolMap(t, session, "audit_show", map[string]any{
		"audit_id": float64(1),
	})
	if auditRes["audit"] == nil {
		t.Fatalf("audit_show returned no audit")
	}

	searchRes := callToolItems(t, session, "knowledge_query", map[string]any{
		"repo_id": "nanite",
		"query":   "panic recovery",
		"limit":   5,
	})
	if len(searchRes) == 0 {
		t.Fatalf("knowledge_query returned no results")
	}

	added := callToolMap(t, session, "finding_add", map[string]any{
		"repo_id":     "nanite",
		"title":       "Integration-created finding",
		"category":    "risk",
		"severity":    "low",
		"status":      "open",
		"description": "Created through MCP integration test",
	})
	addedID := int64(added["id"].(float64))

	updated := callToolMap(t, session, "finding_update_status", map[string]any{
		"id":     addedID,
		"status": "resolved",
	})
	if got := updated["status"]; got != "resolved" {
		t.Fatalf("finding_update_status status = %v", got)
	}

	rejected, err := session.CallTool(context.Background(), &gomcp.CallToolParams{
		Name:      "finding_update_status",
		Arguments: map[string]any{"id": addedID, "status": "adressed"},
	})
	if err != nil {
		t.Fatalf("finding_update_status (invalid status): %v", err)
	}
	if !rejected.IsError {
		t.Fatalf("finding_update_status accepted unrecognized status")
	}

	exportDir := filepath.Join(t.TempDir(), "audit-export")
	exported := callToolMap(t, session, "audit_export", map[string]any{
		"audit_id": float64(1),
		"out_dir":  exportDir,
	})
	if got := exported["out_dir"]; got != exportDir {
		t.Fatalf("audit_export out_dir = %v", got)
	}

	imported := callToolMap(t, session, "audit_import", map[string]any{
		"folder":  exportDir,
		"repo_id": "nanite",
		"dry_run": true,
	})
	if summaries := imported["summaries"].([]any); len(summaries) == 0 {
		t.Fatalf("audit_import dry run returned no summaries")
	}
}

func seedIntegrationFixture(store *sqlite.Store) error {
	repo := &domain.Repo{
		ID:               "nanite",
		Name:             "Nanite",
		URL:              "https://example.com/nanite",
		Description:      "Fixture repo",
		Category:         "desktop-app",
		EmbeddingProfile: "none",
	}
	if err := store.CreateRepo(repo); err != nil {
		return err
	}

	snap := &domain.Snapshot{RepoID: repo.ID}
	if err := store.CreateSnapshot(snap); err != nil {
		return err
	}

	lineStart := 42
	lineEnd := 88
	sym := &model.Symbol{
		RepoID:        repo.ID,
		Kind:          "method",
		Name:          "Run",
		QualifiedName: "nanite.Engine.Run",
		FilePath:      "internal/chat/engine.go",
		LineStart:     &lineStart,
		LineEnd:       &lineEnd,
		ContentHash:   "hash-engine-run",
		SignatureHash: "sig-engine-run",
		Language:      "go",
		Docstring:     "Run drives panic recovery through the chat engine.",
	}
	if err := store.UpsertSymbol(sym); err != nil {
		return err
	}
	helperStart := 17
	helperEnd := 25
	helper := &model.Symbol{
		RepoID:        repo.ID,
		Kind:          "function",
		Name:          "recoverPanic",
		QualifiedName: "nanite.recoverPanic",
		FilePath:      "internal/chat/recover.go",
		LineStart:     &helperStart,
		LineEnd:       &helperEnd,
		ContentHash:   "hash-recover-panic",
		SignatureHash: "sig-recover-panic",
		Language:      "go",
		Docstring:     "recoverPanic centralizes panic handling.",
	}
	if err := store.UpsertSymbol(helper); err != nil {
		return err
	}
	if err := store.UpsertRelationship(&domain.Relationship{
		RepoID:       repo.ID,
		SrcSymbolID:  sym.ID,
		DstSymbolID:  helper.ID,
		Kind:         "references",
		Source:       "scip",
		Weight:       1,
		DiscoveredAt: time.Now().UTC(),
	}); err != nil {
		return err
	}

	refStart := 57
	refEnd := 73
	auditStore := audits.NewStore(store.DB())
	ref := "commit-123"
	bundle, err := auditStore.ReplaceImportedAudit(audits.ImportBundle{
		Audit: &audits.Audit{
			RepoID:       repo.ID,
			Scope:        "chat engine",
			ScopePaths:   []string{"internal/chat/engine.go"},
			AuditType:    "deep-review",
			Auditor:      "fixture-agent",
			AuditedAtRef: &ref,
			Status:       "completed",
			Provenance: audits.Provenance{
				ActorKind: "agent",
				ActorID:   "fixture-agent",
				SessionID: "fixture-session",
				ToolName:  "fixture",
				ModelName: "fixture-model",
			},
		},
		Findings: []audits.Finding{
			{
				Finding: domain.Finding{
					RepoID:      &repo.ID,
					Title:       "Panic recovery is inconsistent",
					Category:    "risk",
					Severity:    "high",
					Description: "The engine recovers some panic paths but misses nested tool execution errors.",
					Status:      "open",
				},
				BodyMarkdown: "## Body\n\nThe engine misses panic recovery in nested tool execution.",
				SymbolID:     &sym.ID,
				Provenance: audits.Provenance{
					ActorKind: "agent",
					ActorID:   "fixture-agent",
					SessionID: "fixture-session",
					ToolName:  "fixture",
					ModelName: "fixture-model",
				},
				CodeRefs: []domain.CodeReference{
					{
						RepoID:      repo.ID,
						FilePath:    "internal/chat/engine.go",
						LineStart:   &refStart,
						LineEnd:     &refEnd,
						Description: "panic recovery block",
						RefType:     "function",
					},
				},
				Themes: []audits.Theme{
					{Name: "panic-recovery", Description: "panic and crash handling"},
				},
			},
		},
	})
	if err != nil {
		return err
	}
	if bundle == nil || bundle.Audit == nil {
		return context.Canceled
	}
	return nil
}

func callToolMap(t *testing.T, session *gomcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error", name)
	}
	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		blob, _ := json.Marshal(res.StructuredContent)
		t.Fatalf("%s returned unexpected structured content: %s", name, string(blob))
	}
	return out
}

func callToolItems(t *testing.T, session *gomcp.ClientSession, name string, args map[string]any) []any {
	t.Helper()
	out := callToolMap(t, session, name, args)
	items, ok := out["items"].([]any)
	if !ok {
		blob, _ := json.Marshal(out)
		t.Fatalf("%s returned unexpected items payload: %s", name, string(blob))
	}
	return items
}
