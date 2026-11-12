package semcp

import (
	"context"
	"encoding/json"
	"time"

	gomcp "github.com/hollis-labs/libs/mcp/plugin-mcp/go-mcp/server"
	libauth "github.com/hollis-labs/libs/security/auth"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/substrate/module"
	mcpmod "github.com/hollis-labs/substrate/modules/mcp"
)

// Scopes the tools require. The /mcp route itself needs the read scope (config mcp.scope); a tool
// that changes data also needs the write scope.
const (
	ScopeRead  = "mentat.read"
	ScopeWrite = "mentat.write"
	ScopeAdmin = "mentat.admin"
)

var toolNames = []string{
	"repo_context", "prior_art_for_file", "prior_art_for_symbol", "finding_search", "symbol_lookup", "graph_neighbors",
	"audit_show", "knowledge_query", "finding_add", "finding_update_status", "audit_import", "audit_export",
	"event_stream_subscribe",
}

// writeTools change stored data.
var writeTools = map[string]bool{"finding_add": true, "finding_update_status": true}

// adminTools read or write files at paths the caller names on the server's filesystem.
var adminTools = map[string]bool{"audit_import": true, "audit_export": true}

func toolScope(name string) string {
	switch {
	case adminTools[name]:
		return ScopeAdmin
	case writeTools[name]:
		return ScopeWrite
	}
	return ScopeRead
}

// Register adds Mentat's tools to the MCP module's server and declares their scopes on the auth
// table. Call after boot.Start.
func Register(app *module.App, svc *Service, table *libauth.ScopeTable) {
	server := mcpmod.Server(app)
	authorize := func(ctx context.Context, tool string) error { return mcpmod.Authorize(ctx, app, tool) }
	for _, name := range toolNames {
		table.Tool(name, toolScope(name))
	}

	server.RegisterChecked(gomcp.Tool{
		Name:        "repo_context",
		Description: "Summarize the current context for a repo: findings, themes, audits, snapshot, and activity.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":   strProp("optional repo id"),
			"page":      numProp("pagination page number"),
			"page_size": numProp("pagination page size, default 25"),
		}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "repo_context"); err != nil {
				return nil, err
			}
			return svc.RepoContext(
				argString(args, "repo_id", ""),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	}, gomcp.Reads("summarizes stored findings, audits, snapshots and activity for a repo").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "prior_art_for_file",
		Description: "Return findings, themes, code references, and symbols touching a file path.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":   strProp("optional repo id"),
			"path":      strProp("repo-relative file path"),
			"page":      numProp("pagination page number"),
			"page_size": numProp("pagination page size, default 25"),
		}, "path"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "prior_art_for_file"); err != nil {
				return nil, err
			}
			return svc.PriorArtForFile(
				argString(args, "repo_id", ""),
				argString(args, "path", ""),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	}, gomcp.Reads("looks up stored findings, themes, references and symbols for a file path").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "prior_art_for_symbol",
		Description: "Return findings, themes, and code references linked to a symbol id or qualified name.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":        strProp("optional repo id"),
			"symbol_id":      numProp("optional symbol id"),
			"qualified_name": strProp("optional qualified name"),
			"page":           numProp("pagination page number"),
			"page_size":      numProp("pagination page size, default 25"),
		}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "prior_art_for_symbol"); err != nil {
				return nil, err
			}
			return svc.PriorArtForSymbol(
				argString(args, "repo_id", ""),
				argString(args, "qualified_name", ""),
				argInt64(args, "symbol_id", 0),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	}, gomcp.Reads("looks up stored findings and references for a symbol").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "finding_search",
		Description: "Search findings using Stack Explorer hybrid retrieval with finding-focused defaults.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id": strProp("optional repo id"),
			"query":   strProp("freeform search query"),
			"limit":   numProp("result limit, default 10"),
		}, "query"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "finding_search"); err != nil {
				return nil, err
			}
			items, err := svc.FindingSearch(argString(args, "query", ""), argString(args, "repo_id", ""), defaultSearchLimit(argInt(args, "limit", 0)))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	}, gomcp.Reads("searches stored findings").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "symbol_lookup",
		Description: "Resolve symbol names to symbol metadata and locations.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":           strProp("optional repo id"),
			"query":             strProp("symbol name or qualified name"),
			"kind":              strProp("optional symbol kind"),
			"language":          strProp("optional language filter"),
			"limit":             numProp("result limit, default 10"),
			"include_neighbors": boolProp("include relationship neighbors in each symbol result"),
			"neighbor_kind":     strProp("optional relationship kind filter for included neighbors"),
			"neighbor_source":   strProp("optional relationship source filter for included neighbors"),
			"neighbor_depth":    numProp("neighbor traversal depth, default 1"),
			"neighbor_limit":    numProp("neighbor result limit per symbol, default 10"),
		}, "query"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "symbol_lookup"); err != nil {
				return nil, err
			}
			items, err := svc.SymbolLookupWithOptions(ctx,
				argString(args, "repo_id", ""),
				argString(args, "query", ""),
				argString(args, "kind", ""),
				argString(args, "language", ""),
				defaultSearchLimit(argInt(args, "limit", 0)),
				SymbolLookupOptions{
					IncludeNeighbors: argBool(args, "include_neighbors", false),
					NeighborKind:     argString(args, "neighbor_kind", ""),
					NeighborSource:   argString(args, "neighbor_source", ""),
					NeighborDepth:    argInt(args, "neighbor_depth", 0),
					NeighborLimit:    defaultSearchLimit(argInt(args, "neighbor_limit", 0)),
				})
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	}, gomcp.Reads("looks up stored symbols").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "graph_neighbors",
		Description: "Return neighboring symbols and relationship metadata for a symbol.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":        strProp("optional repo id when resolving by qualified_name"),
			"symbol_id":      numProp("optional symbol id"),
			"qualified_name": strProp("optional qualified name"),
			"kind":           strProp("optional relationship kind filter"),
			"source":         strProp("optional relationship source filter"),
			"depth":          numProp("traversal depth, default 1"),
			"limit":          numProp("result limit, default 10"),
		}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "graph_neighbors"); err != nil {
				return nil, err
			}
			items, err := svc.GraphNeighbors(ctx,
				argString(args, "repo_id", ""),
				argString(args, "qualified_name", ""),
				argInt64(args, "symbol_id", 0),
				argString(args, "kind", ""),
				argString(args, "source", ""),
				argInt(args, "depth", 0),
				defaultSearchLimit(argInt(args, "limit", 0)))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	}, gomcp.Reads("walks stored symbol relationships").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "audit_show",
		Description: "Return a stored audit with its findings and themes.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"audit_id": numProp("audit id"),
		}, "audit_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "audit_show"); err != nil {
				return nil, err
			}
			return svc.AuditShow(argInt64(args, "audit_id", 0))
		},
	}, gomcp.Reads("returns one stored audit").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "knowledge_query",
		Description: "Run freeform hybrid retrieval across findings, symbols, and code references.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id": strProp("optional repo id"),
			"query":   strProp("freeform search query"),
			"kind":    strProp("optional kind filter: finding, symbol, or code-ref"),
			"limit":   numProp("result limit, default 10"),
		}, "query"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "knowledge_query"); err != nil {
				return nil, err
			}
			items, err := svc.KnowledgeQuery(argString(args, "query", ""), argString(args, "repo_id", ""), argString(args, "kind", ""), defaultSearchLimit(argInt(args, "limit", 0)))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	}, gomcp.Reads("searches the stored knowledge index").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "finding_add",
		Description: "Create a finding. Provenance is derived from the MCP caller context or environment.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":       strProp("optional repo filter for the finding"),
			"title":         strProp("finding title"),
			"category":      strProp("gap, strength, opportunity, or risk"),
			"severity":      strProp("critical, high, medium, low, or info"),
			"status":        strProp("open, acknowledged, resolved, or wontfix"),
			"description":   strProp("finding body text"),
			"audit_id":      numProp("optional linked audit id"),
			"symbol_id":     numProp("optional linked symbol id"),
			"body_markdown": strProp("optional markdown body"),
		}, "title"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "finding_add"); err != nil {
				return nil, err
			}
			in := FindingCreateInput{
				RepoID:       argString(args, "repo_id", ""),
				Title:        argString(args, "title", ""),
				Category:     argString(args, "category", ""),
				Severity:     argString(args, "severity", ""),
				Status:       argString(args, "status", ""),
				Description:  argString(args, "description", ""),
				AuditID:      argInt64Ptr(args, "audit_id"),
				SymbolID:     argInt64Ptr(args, "symbol_id"),
				BodyMarkdown: argString(args, "body_markdown", ""),
			}
			return svc.AddFinding(in, svc.provenance(ctx, "finding_add"))
		},
	}, gomcp.Writes())

	server.RegisterChecked(gomcp.Tool{
		Name:        "finding_update_status",
		Description: "Update the lifecycle status of a finding.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"id":     numProp("finding id"),
			"status": strProp("new finding status: open, acknowledged, resolved, or wontfix"),
		}, "id", "status"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "finding_update_status"); err != nil {
				return nil, err
			}
			in := FindingStatusUpdateInput{
				ID:     argInt64(args, "id", 0),
				Status: argString(args, "status", ""),
			}
			return svc.UpdateFindingStatus(in, svc.provenance(ctx, "finding_update_status"))
		},
	}, gomcp.Writes().Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "audit_import",
		Description: "Import one or more deep-review audit directories into Stack Explorer.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"folder":  strProp("folder containing one or more deep-review audits"),
			"repo_id": strProp("repo id to attach imported audits to"),
			"dry_run": boolProp("when true, parse only without writing"),
		}, "folder", "repo_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "audit_import"); err != nil {
				return nil, err
			}
			in := AuditImportInput{
				Folder: argString(args, "folder", ""),
				RepoID: argString(args, "repo_id", ""),
				DryRun: argBool(args, "dry_run", false),
			}
			return svc.AuditImport(in, svc.provenance(ctx, "audit_import"))
		},
	}, gomcp.Destroys("re-importing replaces the stored audit, its findings and themes for the same scope").Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "audit_export",
		Description: "Export a stored audit to deep-review markdown files.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"audit_id": numProp("audit id to export"),
			"out_dir":  strProp("optional target directory"),
		}, "audit_id"),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "audit_export"); err != nil {
				return nil, err
			}
			in := AuditExportInput{
				AuditID: argInt64(args, "audit_id", 0),
				OutDir:  argString(args, "out_dir", ""),
			}
			return svc.AuditExport(in)
		},
	}, gomcp.Writes().Idempotent())

	server.RegisterChecked(gomcp.Tool{
		Name:        "event_stream_subscribe",
		Description: "Subscribe to scheduler job events. Historical backlog is returned first, then live events until the timeout or limit is reached.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":         strProp("optional repo id"),
			"schedule_id":     strProp("optional schedule id"),
			"job_id":          strProp("optional job id"),
			"job_kind":        strProp("optional job kind"),
			"status":          strProp("optional status filter"),
			"since_id":        numProp("optional event id cursor"),
			"limit":           numProp("max events to return, default 25"),
			"timeout_seconds": numProp("max subscribe duration, default 30"),
		}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := authorize(ctx, "event_stream_subscribe"); err != nil {
				return nil, err
			}
			limit := argInt(args, "limit", 0)
			if limit <= 0 {
				limit = 25
			}
			timeout := 30 * time.Second
			if ts := argInt(args, "timeout_seconds", 0); ts > 0 {
				timeout = time.Duration(ts) * time.Second
			}
			filter := sqlite.EventFilter{
				RepoID:     argString(args, "repo_id", ""),
				ScheduleID: argString(args, "schedule_id", ""),
				JobID:      argString(args, "job_id", ""),
				JobKind:    argString(args, "job_kind", ""),
				Status:     argString(args, "status", ""),
				SinceID:    argInt64(args, "since_id", 0),
				Limit:      limit,
			}
			var progressToken any
			if meta := gomcp.MetaFromContext(ctx); meta != nil {
				progressToken = meta["progressToken"]
			}
			items, err := svc.EventStreamSubscribe(ctx, filter, limit, timeout, func(event domain.JobEvent) {
				data, _ := json.Marshal(event)
				gomcp.NotifyProgress(ctx, progressToken, float64(event.ID), 0, string(data))
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": items}, nil
		},
	}, gomcp.Reads("returns stored job events and waits for new ones").Idempotent())

}

func defaultPage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func defaultPageSize(pageSize int) int {
	if pageSize <= 0 {
		return 25
	}
	return pageSize
}

func defaultSearchLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	return limit
}

func toAnySlice[T any](items []T) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// Small accessors over a tool call's decoded arguments map: go-mcp's
// ToolHandler receives a plain map[string]any (arguments are JSON-decoded
// before the handler runs, so a JSON number always arrives as float64), in
// place of the typed struct binding the official SDK's generic AddTool used
// to infer via reflection.

func argString(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

func argFloat(args map[string]any, key string, def float64) float64 {
	if v, ok := args[key].(float64); ok {
		return v
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	return int(argFloat(args, key, float64(def)))
}

func argInt64(args map[string]any, key string, def int64) int64 {
	return int64(argFloat(args, key, float64(def)))
}

func argInt64Ptr(args map[string]any, key string) *int64 {
	if v, ok := args[key].(float64); ok {
		i := int64(v)
		return &i
	}
	return nil
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
