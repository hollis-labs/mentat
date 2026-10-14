package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	filter := sqlite.EventFilter{
		RepoID:     queryFilter(r, "repo_id"),
		ScheduleID: queryFilter(r, "schedule_id"),
		JobID:      queryFilter(r, "job_id"),
		JobKind:    queryFilter(r, "job_kind"),
		Status:     queryFilter(r, "status"),
		Limit:      parsePositiveInt(r.URL.Query().Get("limit"), 50),
	}
	filter.SinceID = parseInt64(r.URL.Query().Get("since_id"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	backlog, err := s.jobs.ListEvents(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, event := range backlog {
		if err := writeSSE(w, "job_event", event); err != nil {
			return
		}
		filter.SinceID = event.ID
	}
	flusher.Flush()

	ch, cancel := s.jobs.Subscribe(filter, 64)
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, "job_event", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func queryFilter(r *http.Request, key string) string {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		return value
	}
	return strings.TrimSpace(r.URL.Query().Get("filter[" + key + "]"))
}

func parsePositiveInt(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
