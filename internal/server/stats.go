package server

import (
	"net/http"
	"time"

	"github.com/ZoOwen/loker-id/internal/store"
)

type sourceStatResponse struct {
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	ActiveJobs int64      `json:"active_jobs"`
	LastRunAt  *time.Time `json:"last_run_at"`
}

type stackStatResponse struct {
	Stack    string `json:"stack"`
	JobCount int64  `json:"job_count"`
}

type statsResponse struct {
	TotalActive int64                `json:"total_active"`
	BySource    []sourceStatResponse `json:"by_source"`
	ByStack     []stackStatResponse  `json:"by_stack"`
}

// handleStats serves GET /api/stats — total active jobs, a per-source
// breakdown (including when each source was last scraped), and a
// per-technology breakdown.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		s.logger.Error("stats failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toStatsResponse(stats))
}

func toStatsResponse(stats store.Stats) statsResponse {
	resp := statsResponse{
		TotalActive: stats.TotalActive,
		BySource:    make([]sourceStatResponse, len(stats.BySource)),
		ByStack:     make([]stackStatResponse, len(stats.ByStack)),
	}
	for i, src := range stats.BySource {
		resp.BySource[i] = sourceStatResponse{
			Slug:       src.Slug,
			Name:       src.Name,
			ActiveJobs: src.ActiveJobs,
			LastRunAt:  src.LastRunAt,
		}
	}
	for i, st := range stats.ByStack {
		resp.ByStack[i] = stackStatResponse{Stack: st.Stack, JobCount: st.JobCount}
	}
	return resp
}
