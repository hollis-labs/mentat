package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/hollis-labs/mentat/internal/audits"
)

type auditCreateRequest struct {
	RepoID     string            `json:"repo_id"`
	Scope      string            `json:"scope"`
	AuditType  string            `json:"audit_type"`
	Auditor    string            `json:"auditor"`
	Summary    string            `json:"summary_markdown"`
	Verdict    string            `json:"verdict"`
	Status     string            `json:"status"`
	ScopePaths []string          `json:"scope_paths"`
	Provenance audits.Provenance `json:"provenance"`
}

func (s *Server) listAudits(w http.ResponseWriter, r *http.Request) {
	store := audits.NewStore(s.store.DB())
	items, err := store.ListAudits(audits.ListFilter{
		RepoID: r.URL.Query().Get("repo"),
		Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, items, len(items), 1, len(items))
}

func (s *Server) createAudit(w http.ResponseWriter, r *http.Request) {
	var req auditCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.RepoID == "" || req.Scope == "" {
		writeError(w, http.StatusBadRequest, "repo_id and scope are required")
		return
	}
	if req.AuditType == "" {
		req.AuditType = "deep-review"
	}
	if req.Status == "" {
		req.Status = "in_progress"
	}
	store := audits.NewStore(s.store.DB())
	item := &audits.Audit{
		RepoID:          req.RepoID,
		Scope:           req.Scope,
		ScopePaths:      req.ScopePaths,
		AuditType:       req.AuditType,
		Auditor:         req.Auditor,
		SummaryMarkdown: req.Summary,
		Status:          req.Status,
		Provenance:      req.Provenance,
	}
	if req.Verdict != "" {
		item.Verdict = &req.Verdict
	}
	if err := store.CreateAudit(item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCreated(w, item)
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid audit id")
		return
	}
	store := audits.NewStore(s.store.DB())
	bundle, err := store.GetAuditBundle(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if bundle == nil {
		writeError(w, http.StatusNotFound, "audit not found")
		return
	}
	writeItem(w, bundle)
}

func (s *Server) getAuditFindings(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid audit id")
		return
	}
	store := audits.NewStore(s.store.DB())
	findings, err := store.ListAuditFindings(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeList(w, findings, len(findings), 1, len(findings))
}

func (s *Server) diffAudit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid audit id")
		return
	}
	against, err := strconv.ParseInt(r.URL.Query().Get("against"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid against id")
		return
	}
	store := audits.NewStore(s.store.DB())
	left, err := store.GetAuditBundle(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	right, err := store.GetAuditBundle(against)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	diff, err := audits.DiffBundles(left, right)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeItem(w, diff)
}
