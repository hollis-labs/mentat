package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	libauth "github.com/hollis-labs/libs/security/auth"
	"github.com/hollis-labs/mentat/internal/embed"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"github.com/hollis-labs/substrate/apierr"
)

// Scopes the guard enforces. Reads need ScopeRead, changes ScopeWrite, and the whole-database
// download ScopeAdmin.
const (
	ScopeRead  = "mentat.read"
	ScopeWrite = "mentat.write"
	ScopeAdmin = "mentat.admin"
)

// Server is the HTTP API for Mentat.
type Server struct {
	store       *sqlite.Store
	jobs        *jobs.Service
	embedder    *embed.Manager
	githubToken string
}

// Deps are what the API needs besides the store. GitHubToken is optional (unauthenticated GitHub
// calls without it).
type Deps struct {
	Jobs        *jobs.Service
	Embedder    *embed.Manager
	GitHubToken string
}

// NewServer creates the API over the given store.
func NewServer(store *sqlite.Store, deps Deps) *Server {
	return &Server{store: store, jobs: deps.Jobs, embedder: deps.Embedder, githubToken: deps.GitHubToken}
}

// Route is one API route: its method, its net/http mux pattern (always under /api/), its scope and
// its handler. The mux and the scope table are both built from this list, so a route cannot exist
// without a scope.
type Route struct {
	Method  string
	Pattern string
	Scope   string
	Handler http.HandlerFunc
}

func (s *Server) routes() []Route {
	r := func(method, pattern string, h http.HandlerFunc) Route {
		scope := ScopeWrite
		if method == http.MethodGet {
			scope = ScopeRead
		}
		return Route{method, pattern, scope, h}
	}
	const (
		get    = http.MethodGet
		post   = http.MethodPost
		put    = http.MethodPut
		delete = http.MethodDelete
	)
	backup := r(get, "/api/db/backup", s.backupDB)
	backup.Scope = ScopeAdmin
	return []Route{
		r(get, "/api/repos", s.listRepos),
		r(post, "/api/repos", s.createRepo),
		r(post, "/api/repos/import", s.importRepos),
		r(get, "/api/repos/export", s.exportRepos),
		r(get, "/api/repos/{id}", s.getRepo),
		r(put, "/api/repos/{id}", s.updateRepo),
		r(delete, "/api/repos/{id}", s.deleteRepo),
		r(post, "/api/repos/{id}/tags", s.addRepoTags),
		r(post, "/api/repos/{id}/embeddings/refresh", s.refreshRepoEmbeddings),
		r(get, "/api/tags", s.listTags),
		r(post, "/api/tags", s.createTag),
		r(delete, "/api/tags/{id}", s.deleteTag),
		r(get, "/api/snapshots", s.listSnapshots),
		r(post, "/api/snapshots", s.createSnapshot),
		r(get, "/api/snapshots/{id}", s.getSnapshot),
		r(get, "/api/dimensions", s.listDimensions),
		r(post, "/api/dimensions", s.createDimension),
		r(get, "/api/dimensions/{id}", s.getDimension),
		r(put, "/api/dimensions/{id}", s.updateDimension),
		r(delete, "/api/dimensions/{id}", s.deleteDimension),
		r(get, "/api/lenses", s.listLenses),
		r(post, "/api/lenses", s.createLens),
		r(get, "/api/lenses/{id}", s.getLens),
		r(put, "/api/lenses/{id}", s.updateLens),
		r(delete, "/api/lenses/{id}", s.deleteLens),
		r(get, "/api/scorecards", s.listScorecards),
		r(post, "/api/scorecards", s.createScorecard),
		r(get, "/api/scorecards/{id}", s.getScorecard),
		r(get, "/api/scores", s.listScores),
		r(post, "/api/scores", s.createScore),
		r(put, "/api/scores/{id}", s.updateScore),
		r(get, "/api/patterns", s.listPatterns),
		r(post, "/api/patterns", s.createPattern),
		r(get, "/api/patterns/{id}", s.getPattern),
		r(put, "/api/patterns/{id}", s.updatePattern),
		r(delete, "/api/patterns/{id}", s.deletePattern),
		r(get, "/api/findings", s.listFindings),
		r(post, "/api/findings", s.createFinding),
		r(get, "/api/findings/{id}", s.getFinding),
		r(put, "/api/findings/{id}", s.updateFinding),
		r(delete, "/api/findings/{id}", s.deleteFinding),
		r(get, "/api/audits", s.listAudits),
		r(post, "/api/audits", s.createAudit),
		r(get, "/api/audits/{id}", s.getAudit),
		r(get, "/api/audits/{id}/findings", s.getAuditFindings),
		r(get, "/api/audits/{id}/diff", s.diffAudit),
		r(get, "/api/symbols/search", s.searchSymbols),
		r(get, "/api/symbols/{id}", s.getSymbol),
		r(get, "/api/relationships", s.listRelationships),
		r(get, "/api/jobs", s.listJobs),
		r(get, "/api/search", s.searchKnowledge),
		r(get, "/api/events", s.streamEvents),
		r(get, "/api/comparison-sets", s.listComparisonSets),
		r(post, "/api/comparison-sets", s.createComparisonSet),
		r(get, "/api/comparison-sets/{id}", s.getComparisonSet),
		r(delete, "/api/comparison-sets/{id}", s.deleteComparisonSet),
		backup,
		r(get, "/api/scans", s.listScans),
		r(post, "/api/scans", s.createScan),
		r(get, "/api/scans/{id}", s.getScan),
		r(put, "/api/scans/{id}", s.updateScan),
		r(delete, "/api/scans/{id}", s.deleteScan),
		r(get, "/api/reports", s.listReports),
		r(post, "/api/reports", s.createReport),
		r(get, "/api/reports/{id}", s.getReport),
		r(put, "/api/reports/{id}", s.updateReport),
		r(delete, "/api/reports/{id}", s.deleteReport),
	}
}

// Handler is the API mux, mounted by httphost under /api/. Responses are JSON unless a handler
// sets its own type (the event stream, the database download).
func (s *Server) Handler() *http.ServeMux {
	m := http.NewServeMux()
	for _, rt := range s.routes() {
		h := rt.Handler
		m.HandleFunc(rt.Method+" "+rt.Pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			h(w, r)
		})
	}
	return m
}

// RegisterScopes declares each route's scope on the table the auth guard enforces. A route not
// listed is refused in enforce mode.
func (s *Server) RegisterScopes(t *libauth.ScopeTable) {
	for _, rt := range s.routes() {
		t.Route(rt.Method, rt.Pattern, rt.Scope)
	}
}

// Run runs the scan worker and returns once it has stopped after ctx ends.
func (s *Server) Run(ctx context.Context) {
	s.runScanWorker(ctx)
}

// --- Response helpers ---

type listMeta struct {
	Total     int `json:"total"`
	Page      int `json:"page"`
	PageSize  int `json:"pageSize"`
	PageCount int `json:"pageCount"`
}

type listResponse struct {
	Data any      `json:"data"`
	Meta listMeta `json:"meta"`
}

type itemResponse struct {
	Data any `json:"data"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeList(w http.ResponseWriter, data any, total, page, pageSize int) {
	pageCount := 0
	if pageSize > 0 {
		pageCount = int(math.Ceil(float64(total) / float64(pageSize)))
	}
	writeJSON(w, http.StatusOK, listResponse{
		Data: data,
		Meta: listMeta{Total: total, Page: page, PageSize: pageSize, PageCount: pageCount},
	})
}

func writeItem(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, itemResponse{Data: data})
}

func writeCreated(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, itemResponse{Data: data})
}

// writeError sends the flat {code, message} error body every Substrate route uses.
func writeError(w http.ResponseWriter, status int, msg string) {
	apierr.Write(w, status, errorCode(status), msg, nil)
}

func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "invalid"
	default:
		if status >= 500 {
			return "internal"
		}
		return "error"
	}
}

// --- Query param helpers ---

type listParams struct {
	Search    string
	Filters   map[string]string
	Sort      string
	Direction string
	Page      int
	PageSize  int
}

func parseListParams(r *http.Request, defaultSort string, defaultPageSize int) listParams {
	p := listParams{
		Search:    r.URL.Query().Get("search"),
		Sort:      r.URL.Query().Get("sort"),
		Direction: r.URL.Query().Get("direction"),
		Filters:   make(map[string]string),
	}

	if p.Sort == "" {
		p.Sort = defaultSort
	}
	if p.Direction == "" {
		p.Direction = "asc"
	}
	p.Direction = strings.ToUpper(p.Direction)
	if p.Direction != "ASC" && p.Direction != "DESC" {
		p.Direction = "ASC"
	}

	p.Page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if p.Page < 1 {
		p.Page = 1
	}
	p.PageSize, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if p.PageSize < 1 {
		p.PageSize = defaultPageSize
	}

	// Parse filter[field]=value
	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "filter[") && strings.HasSuffix(key, "]") {
			field := key[7 : len(key)-1]
			if len(values) > 0 && values[0] != "" {
				p.Filters[field] = values[0]
			}
		}
	}

	return p
}

func (p listParams) offset() int {
	return (p.Page - 1) * p.PageSize
}

// allowedSort validates and returns a safe column name for ORDER BY.
func allowedSort(requested string, allowed map[string]string) string {
	if col, ok := allowed[requested]; ok {
		return col
	}
	return ""
}
