package semcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	gomcp "github.com/hollis-labs/libs/plugin-mcp/go-mcp/server"
	httptransport "github.com/hollis-labs/libs/plugin-mcp/go-mcp/transport/http"
	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func NewServer(store *sqlite.Store, jobSvc *jobs.Service) *gomcp.Server {
	svc := NewService(store, jobSvc)
	server := gomcp.NewServer("mentat", "v0.1.0",
		gomcp.WithInstructions("Use these tools to inspect Stack Explorer repo knowledge, prior art, findings, symbols, and audits. Prefer repo_id filters when available."),
	)

	server.RegisterTool(gomcp.Tool{
		Name:        "repo_context",
		Description: "Summarize the current context for a repo: findings, themes, audits, snapshot, and activity.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":   strProp("optional repo id"),
			"page":      numProp("pagination page number"),
			"page_size": numProp("pagination page size, default 25"),
		}),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return svc.RepoContext(
				argString(args, "repo_id", ""),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "prior_art_for_file",
		Description: "Return findings, themes, code references, and symbols touching a file path.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":   strProp("optional repo id"),
			"path":      strProp("repo-relative file path"),
			"page":      numProp("pagination page number"),
			"page_size": numProp("pagination page size, default 25"),
		}, "path"),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return svc.PriorArtForFile(
				argString(args, "repo_id", ""),
				argString(args, "path", ""),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "prior_art_for_symbol",
		Description: "Return findings, themes, and code references linked to a symbol id or qualified name.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id":        strProp("optional repo id"),
			"symbol_id":      numProp("optional symbol id"),
			"qualified_name": strProp("optional qualified name"),
			"page":           numProp("pagination page number"),
			"page_size":      numProp("pagination page size, default 25"),
		}),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return svc.PriorArtForSymbol(
				argString(args, "repo_id", ""),
				argString(args, "qualified_name", ""),
				argInt64(args, "symbol_id", 0),
				defaultPage(argInt(args, "page", 0)),
				defaultPageSize(argInt(args, "page_size", 0)),
			)
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "finding_search",
		Description: "Search findings using Stack Explorer hybrid retrieval with finding-focused defaults.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id": strProp("optional repo id"),
			"query":   strProp("freeform search query"),
			"limit":   numProp("result limit, default 10"),
		}, "query"),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			items, err := svc.FindingSearch(argString(args, "query", ""), argString(args, "repo_id", ""), defaultSearchLimit(argInt(args, "limit", 0)))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	})

	server.RegisterTool(gomcp.Tool{
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
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
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
	})

	server.RegisterTool(gomcp.Tool{
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
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
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
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "audit_show",
		Description: "Return a stored audit with its findings and themes.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"audit_id": numProp("audit id"),
		}, "audit_id"),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return svc.AuditShow(argInt64(args, "audit_id", 0))
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "knowledge_query",
		Description: "Run freeform hybrid retrieval across findings, symbols, and code references.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"repo_id": strProp("optional repo id"),
			"query":   strProp("freeform search query"),
			"kind":    strProp("optional kind filter: finding, symbol, or code-ref"),
			"limit":   numProp("result limit, default 10"),
		}, "query"),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			items, err := svc.KnowledgeQuery(argString(args, "query", ""), argString(args, "repo_id", ""), argString(args, "kind", ""), defaultSearchLimit(argInt(args, "limit", 0)))
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": toAnySlice(items)}, nil
		},
	})

	server.RegisterTool(gomcp.Tool{
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
			return svc.AddFinding(in, provenanceFromContext(ctx, "finding_add"))
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "finding_update_status",
		Description: "Update the lifecycle status of a finding.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"id":     numProp("finding id"),
			"status": strProp("new finding status: open, acknowledged, resolved, or wontfix"),
		}, "id", "status"),
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			in := FindingStatusUpdateInput{
				ID:     argInt64(args, "id", 0),
				Status: argString(args, "status", ""),
			}
			return svc.UpdateFindingStatus(in, provenanceFromContext(ctx, "finding_update_status"))
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "audit_import",
		Description: "Import one or more deep-review audit directories into Stack Explorer.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"folder":  strProp("folder containing one or more deep-review audits"),
			"repo_id": strProp("repo id to attach imported audits to"),
			"dry_run": boolProp("when true, parse only without writing"),
		}, "folder", "repo_id"),
		DestructiveHint: true,
		IdempotentHint:  true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			in := AuditImportInput{
				Folder: argString(args, "folder", ""),
				RepoID: argString(args, "repo_id", ""),
				DryRun: argBool(args, "dry_run", false),
			}
			return svc.AuditImport(in, provenanceFromContext(ctx, "audit_import"))
		},
	})

	server.RegisterTool(gomcp.Tool{
		Name:        "audit_export",
		Description: "Export a stored audit to deep-review markdown files.",
		InputSchema: gomcp.ObjectSchema(map[string]any{
			"audit_id": numProp("audit id to export"),
			"out_dir":  strProp("optional target directory"),
		}, "audit_id"),
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			in := AuditExportInput{
				AuditID: argInt64(args, "audit_id", 0),
				OutDir:  argString(args, "out_dir", ""),
			}
			return svc.AuditExport(in)
		},
	})

	server.RegisterTool(gomcp.Tool{
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
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
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
	})

	return server
}

func ServeStdio(ctx context.Context, store *sqlite.Store, jobSvc *jobs.Service) error {
	return NewServer(store, jobSvc).Run(ctx)
}

func NewHTTPHandler(store *sqlite.Store, jobSvc *jobs.Service) http.Handler {
	server := NewServer(store, jobSvc)
	return provenanceHeaderMiddleware(httptransport.NewHandler(server, httptransport.HandlerOptions{}))
}

func ListenAndServeHTTP(ctx context.Context, store *sqlite.Store, jobSvc *jobs.Service, host string, port int, path string) error {
	if path == "" {
		path = "/mcp"
	}
	mux := http.NewServeMux()
	mux.Handle(path, NewHTTPHandler(store, jobSvc))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
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
