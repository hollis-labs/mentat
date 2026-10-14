package api

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hollis-labs/mentat/internal/jobs"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

// Server is the HTTP API server for Stack Explorer.
type Server struct {
	store  *sqlite.Store
	jobs   *jobs.Service
	router chi.Router
	opts   ServeOptions
}

// NewServer creates a new API server backed by the given store.
func NewServer(store *sqlite.Store) *Server {
	s := &Server{store: store, jobs: jobs.NewService(jobs.Config{Store: store, Workers: 2})}
	s.router = s.buildRouter()
	return s
}

// ListenAndServe starts the HTTP server. It binds to loopback unless
// opts.Host says otherwise, and refuses a non-loopback bind without a token.
func (s *Server) ListenAndServe(opts ServeOptions) error {
	if err := opts.validate(); err != nil {
		return err
	}
	s.opts = opts
	s.router = s.buildRouter()
	addr := opts.addr()
	if err := s.jobs.Start(context.Background()); err != nil {
		return err
	}
	s.startScanWorker()
	defer s.jobs.Close()
	auth := "disabled (loopback only)"
	if opts.Token != "" {
		auth = "bearer token required"
	}
	log.Printf("Stack Explorer API listening on %s (auth: %s)", addr, auth)
	srv := &http.Server{Addr: addr, Handler: s.router, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	origins := s.opts.CORSOrigins
	if len(origins) == 0 {
		origins = DefaultCORSOrigins
	}

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})

	r.Route("/api", func(r chi.Router) {
		if s.opts.Token != "" {
			r.Use(requireToken(s.opts.Token))
		}
		r.Route("/repos", func(r chi.Router) {
			r.Get("/", s.listRepos)
			r.Post("/", s.createRepo)
			r.Post("/import", s.importRepos)
			r.Get("/export", s.exportRepos)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getRepo)
				r.Put("/", s.updateRepo)
				r.Delete("/", s.deleteRepo)
				r.Post("/tags", s.addRepoTags)
				r.Post("/embeddings/refresh", s.refreshRepoEmbeddings)
			})
		})
		r.Route("/tags", func(r chi.Router) {
			r.Get("/", s.listTags)
			r.Post("/", s.createTag)
			r.Delete("/{id}", s.deleteTag)
		})
		r.Route("/snapshots", func(r chi.Router) {
			r.Get("/", s.listSnapshots)
			r.Post("/", s.createSnapshot)
			r.Get("/{id}", s.getSnapshot)
		})
		r.Route("/dimensions", func(r chi.Router) {
			r.Get("/", s.listDimensions)
			r.Post("/", s.createDimension)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getDimension)
				r.Put("/", s.updateDimension)
				r.Delete("/", s.deleteDimension)
			})
		})
		r.Route("/lenses", func(r chi.Router) {
			r.Get("/", s.listLenses)
			r.Post("/", s.createLens)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getLens)
				r.Put("/", s.updateLens)
				r.Delete("/", s.deleteLens)
			})
		})
		r.Route("/scorecards", func(r chi.Router) {
			r.Get("/", s.listScorecards)
			r.Post("/", s.createScorecard)
			r.Get("/{id}", s.getScorecard)
		})
		r.Route("/scores", func(r chi.Router) {
			r.Get("/", s.listScores)
			r.Post("/", s.createScore)
			r.Put("/{id}", s.updateScore)
		})
		r.Route("/patterns", func(r chi.Router) {
			r.Get("/", s.listPatterns)
			r.Post("/", s.createPattern)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getPattern)
				r.Put("/", s.updatePattern)
				r.Delete("/", s.deletePattern)
			})
		})
		r.Route("/findings", func(r chi.Router) {
			r.Get("/", s.listFindings)
			r.Post("/", s.createFinding)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getFinding)
				r.Put("/", s.updateFinding)
				r.Delete("/", s.deleteFinding)
			})
		})
		r.Route("/audits", func(r chi.Router) {
			r.Get("/", s.listAudits)
			r.Post("/", s.createAudit)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getAudit)
				r.Get("/findings", s.getAuditFindings)
				r.Get("/diff", s.diffAudit)
			})
		})
		r.Route("/symbols", func(r chi.Router) {
			r.Get("/search", s.searchSymbols)
			r.Get("/{id}", s.getSymbol)
		})
		r.Get("/relationships", s.listRelationships)
		r.Get("/jobs", s.listJobs)
		r.Get("/search", s.searchKnowledge)
		r.Get("/events", s.streamEvents)
		r.Route("/comparison-sets", func(r chi.Router) {
			r.Get("/", s.listComparisonSets)
			r.Post("/", s.createComparisonSet)
			r.Get("/{id}", s.getComparisonSet)
			r.Delete("/{id}", s.deleteComparisonSet)
		})
		r.Route("/db", func(r chi.Router) {
			r.Get("/backup", s.backupDB)
		})
		r.Route("/scans", func(r chi.Router) {
			r.Get("/", s.listScans)
			r.Post("/", s.createScan)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getScan)
				r.Put("/", s.updateScan)
				r.Delete("/", s.deleteScan)
			})
		})
		r.Route("/reports", func(r chi.Router) {
			r.Get("/", s.listReports)
			r.Post("/", s.createReport)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getReport)
				r.Put("/", s.updateReport)
				r.Delete("/", s.deleteReport)
			})
		})
	})

	return r
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

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
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
