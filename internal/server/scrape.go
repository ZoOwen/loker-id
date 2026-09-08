package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"

	"github.com/ZoOwen/loker-id/internal/scraper"
)

// authorizeInternal checks the X-Internal-Token header using a
// constant-time comparison (this gates a real internal endpoint, so
// timing side-channels matter). An unconfigured server token always
// fails closed rather than accepting any/no token.
func (s *Server) authorizeInternal(r *http.Request) bool {
	if s.internalToken == "" {
		return false
	}
	token := r.Header.Get("X-Internal-Token")
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.internalToken)) == 1
}

// handleTriggerScrape serves
// POST /internal/scrape?source=<slug>&pages=<1-20>. It starts the run in
// a background goroutine (not tied to this request's context, which ends
// the moment the 202 response is written) and returns immediately — the
// caller checks GET /api/stats or the scrape_runs table for the outcome,
// not this response.
func (s *Server) handleTriggerScrape(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeInternal(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing X-Internal-Token")
		return
	}

	source := r.URL.Query().Get("source")
	if source == "" {
		writeError(w, http.StatusBadRequest, "source query parameter is required")
		return
	}
	if !s.pipeline.HasSource(source) {
		writeError(w, http.StatusBadRequest, "unknown source: "+source)
		return
	}

	// pages is optional — omitted means "let the scraper use its own
	// default" (scraper.DefaultMaxPages). When given, it's validated here
	// rather than left to the scraper's own clamping, so a typo'd value
	// fails loudly with a 400 instead of silently getting clamped.
	maxPages := 0
	if v := r.URL.Query().Get("pages"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > scraper.MaxPagesCap {
			writeError(w, http.StatusBadRequest, "pages must be an integer between 1 and 20")
			return
		}
		maxPages = n
	}

	go func() {
		result, err := s.pipeline.Run(context.Background(), source, maxPages)
		if err != nil {
			s.logger.Error("triggered scrape run failed to start", "source", source, "error", err)
			return
		}
		s.logger.Info("triggered scrape run finished",
			"source", source,
			"scrape_run_id", result.ScrapeRunID.String(),
			"jobs_found", result.JobsFound,
			"jobs_new", result.JobsNew,
			"jobs_duplicate", result.JobsDuplicate,
			"jobs_failed", result.JobsFailed,
			"errors", len(result.Errors),
		)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "source": source})
}
