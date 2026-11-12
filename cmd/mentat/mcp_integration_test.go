package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hollis-labs/mentat/internal/audits"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/mentat/internal/symbols/model"
)

// installation is a built binary with its own data directory, config and port. Nothing here
// touches the default directories: every path is under t.TempDir().
type installation struct {
	t    *testing.T
	bin  string
	dir  string
	port int
}

func newInstallation(t *testing.T) *installation {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "mentat")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &installation{t: t, bin: bin, dir: dir, port: port}
}

// env is the whole environment of a child: explicit directories and a config file, nothing inherited
// from the real installation.
func (in *installation) env(tokenFile string) []string {
	cfg := filepath.Join(filepath.Dir(in.dir), "config.yaml")
	text := "httphost:\n  addr: \"127.0.0.1:" + strconv.Itoa(in.port) + "\"\n"
	if tokenFile != "" {
		text += "mcp:\n  stdio_token_file: \"" + filepath.ToSlash(tokenFile) + "\"\n"
	}
	if err := os.WriteFile(cfg, []byte(text), 0o600); err != nil {
		in.t.Fatal(err)
	}
	root := filepath.Dir(in.dir)
	return append(os.Environ(),
		"MENTAT_DATA_DIR="+in.dir,
		"SUBSTRATE_CONFIG="+cfg,
		"XDG_DATA_HOME="+filepath.Join(root, "xdg-data"),
		"XDG_CONFIG_HOME="+filepath.Join(root, "xdg-config"),
		"XDG_STATE_HOME="+filepath.Join(root, "xdg-state"),
		"XDG_CACHE_HOME="+filepath.Join(root, "xdg-cache"),
	)
}

// serveAndMint runs `mentat serve`, creates the owner, issues one API key per scope set and stops the
// server again. It returns the tokens in order.
func (in *installation) serveAndMint(sets ...[]string) []string {
	in.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, in.bin, "serve")
	cmd.Env = in.env("")
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		cancel()
		in.t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()

	base := "http://127.0.0.1:" + strconv.Itoa(in.port)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			in.t.Fatalf("serve did not become healthy:\n%s", logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	post := func(path string, body any) []byte {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "test")
		resp, err := client.Do(req)
		if err != nil {
			in.t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 != 2 {
			in.t.Fatalf("POST %s: %d %s", path, resp.StatusCode, out)
		}
		return out
	}
	post("/api/setup", map[string]string{"display_name": "Test", "username": "tester", "password": strings.Join([]string{"throwaway", "owner", "phrase", "42"}, "-")})
	// The stdio token file holds an identity API key (it does not expire within the hour the way a
	// minted token does).
	var tokens []string
	for i, scopes := range sets {
		var key struct {
			Key string `json:"key"`
		}
		body := post("/api/identity/keys", map[string]any{"name": "stdio-test-" + strconv.Itoa(i), "scopes": scopes, "ttl_hours": 24})
		if err := json.Unmarshal(body, &key); err != nil || !strings.HasPrefix(key.Key, "key_") {
			in.t.Fatalf("api key reply: %v %s", err, body)
		}
		tokens = append(tokens, key.Key)
	}
	return tokens
}

// session starts `mentat mcp` over stdio with the given token and connects a client.
func (in *installation) session(token string) *gomcp.ClientSession {
	in.t.Helper()
	tokenFile := filepath.Join(filepath.Dir(in.dir), "stdio.token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		in.t.Fatal(err)
	}
	cmd := exec.Command(in.bin, "mcp")
	cmd.Env = in.env(tokenFile)
	client := gomcp.NewClient(&gomcp.Implementation{Name: "integration-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		in.t.Fatal(err)
	}
	in.t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestStdioRoundTrip(t *testing.T) {
	in := newInstallation(t)
	seed, err := sqlite.Open(filepath.Join(in.dir, "mentat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := seedIntegrationFixture(seed); err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()

	tokens := in.serveAndMint([]string{"mentat.read", "mentat.write", "mentat.admin"}, []string{"mentat.read"})
	full, readOnly := tokens[0], tokens[1]
	session := in.session(full)

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

	// A read-only token lists the tools but may not change data or touch server paths.
	ro := in.session(readOnly)
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"finding_add", map[string]any{"repo_id": "nanite", "title": "x", "category": "risk", "severity": "low", "status": "open", "description": "x"}},
		{"audit_export", map[string]any{"audit_id": float64(1), "out_dir": filepath.Join(t.TempDir(), "out")}},
	} {
		res, err := ro.CallTool(context.Background(), &gomcp.CallToolParams{Name: call.tool, Arguments: call.args})
		if err == nil && !res.IsError {
			t.Fatalf("%s succeeded with a read-only token", call.tool)
		}
	}
	if res := callToolMap(t, ro, "audit_show", map[string]any{"audit_id": float64(1)}); res["audit"] == nil {
		t.Fatal("audit_show failed with a read-only token")
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
