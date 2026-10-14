package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hollis-labs/mentat/internal/domain"
	"github.com/hollis-labs/mentat/internal/store/sqlite"
	"gopkg.in/yaml.v3"
)

// --- YAML Import/Export ---

type repoManifest struct {
	Repos []repoEntry `yaml:"repos" json:"repos"`
}

type repoEntry struct {
	ID               string   `yaml:"id" json:"id"`
	Name             string   `yaml:"name" json:"name"`
	URL              string   `yaml:"url" json:"url"`
	Description      string   `yaml:"description" json:"description"`
	Stack            string   `yaml:"stack" json:"stack"`
	Category         string   `yaml:"category" json:"category"`
	IsOwn            bool     `yaml:"is_own" json:"is_own"`
	LocalPath        string   `yaml:"local_path" json:"local_path"`
	Homepage         string   `yaml:"homepage" json:"homepage"`
	License          string   `yaml:"license" json:"license"`
	EmbeddingProfile string   `yaml:"embedding_profile" json:"embedding_profile"`
	Tags             []string `yaml:"tags" json:"tags"`
}

type importResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

func (s *Server) importRepos(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body: "+err.Error())
		return
	}

	var manifest repoManifest

	// Accept both YAML and JSON
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "json") {
		err = json.Unmarshal(body, &manifest)
	} else {
		err = yaml.Unmarshal(body, &manifest)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid manifest: "+err.Error())
		return
	}

	added, skipped := 0, 0
	for _, entry := range manifest.Repos {
		existing, _ := s.store.GetRepo(entry.ID)
		if existing != nil {
			skipped++
			continue
		}

		repo := &domain.Repo{
			ID:               entry.ID,
			Name:             entry.Name,
			URL:              entry.URL,
			Description:      entry.Description,
			Stack:            entry.Stack,
			Category:         entry.Category,
			IsOwn:            entry.IsOwn,
			LocalPath:        entry.LocalPath,
			Homepage:         entry.Homepage,
			License:          entry.License,
			EmbeddingProfile: entry.EmbeddingProfile,
		}
		if repo.Name == "" {
			repo.Name = repo.ID
		}

		if err := s.store.CreateRepo(repo); err != nil {
			continue
		}

		for _, tag := range entry.Tags {
			tagID := strings.ToLower(strings.ReplaceAll(tag, " ", "-"))
			s.store.EnsureTag(tagID, tag)
			s.store.AddRepoTag(entry.ID, tagID)
		}
		added++
	}

	writeJSON(w, http.StatusOK, importResult{Added: added, Skipped: skipped})
}

func (s *Server) exportRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.store.ListRepos(sqlite.RepoFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var entries []repoEntry
	for _, repo := range repos {
		tags, _ := s.store.ListRepoTags(repo.ID)
		var tagNames []string
		for _, t := range tags {
			tagNames = append(tagNames, t.Name)
		}
		entries = append(entries, repoEntry{
			ID:               repo.ID,
			Name:             repo.Name,
			URL:              repo.URL,
			Description:      repo.Description,
			Stack:            repo.Stack,
			Category:         repo.Category,
			IsOwn:            repo.IsOwn,
			LocalPath:        repo.LocalPath,
			Homepage:         repo.Homepage,
			License:          repo.License,
			EmbeddingProfile: repo.EmbeddingProfile,
			Tags:             tagNames,
		})
	}

	format := r.URL.Query().Get("format")
	if format == "json" {
		writeJSON(w, http.StatusOK, repoManifest{Repos: entries})
		return
	}

	data, err := yaml.Marshal(repoManifest{Repos: entries})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=repos.yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// --- Database Backup ---

func (s *Server) backupDB(w http.ResponseWriter, r *http.Request) {
	dbPath := s.store.Path()
	if dbPath == "" {
		writeError(w, http.StatusInternalServerError, "database path unknown")
		return
	}

	// Checkpoint WAL to ensure the main file is up to date
	s.store.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE)")

	f, err := os.Open(dbPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open database file: "+err.Error())
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filename := fmt.Sprintf("stack-explorer-%s.db", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Del("Content-Type") // Remove the JSON default
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}
