package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/store"
)

// jobSummary is the public JSON shape for a job — db.Job with its pgtype
// wrapper types (Text/Int8/Timestamptz) collapsed into plain Go values,
// and its company_id resolved to a display name.
type jobSummary struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Company      string     `json:"company"`
	Description  string     `json:"description,omitempty"`
	SalaryMin    *int64     `json:"salary_min,omitempty"`
	SalaryMax    *int64     `json:"salary_max,omitempty"`
	SalaryConf   string     `json:"salary_confidence"`
	SalaryRaw    string     `json:"salary_raw,omitempty"`
	Stack        []string   `json:"stack"`
	Location     string     `json:"location,omitempty"`
	LocationCity string     `json:"location_city,omitempty"`
	Mode         string     `json:"mode"`
	Level        string     `json:"level"`
	SourceURL    string     `json:"source_url"`
	PostedAt     *time.Time `json:"posted_at,omitempty"`
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	IsActive     bool       `json:"is_active"`
}

func toJobSummary(j db.Job, companyName string) jobSummary {
	return jobSummary{
		ID:           j.ID.String(),
		Title:        j.Title,
		Company:      companyName,
		Description:  pgTextOrEmpty(j.Description),
		SalaryMin:    pgInt8Ptr(j.SalaryMin),
		SalaryMax:    pgInt8Ptr(j.SalaryMax),
		SalaryConf:   string(j.SalaryConf),
		SalaryRaw:    pgTextOrEmpty(j.SalaryRaw),
		Stack:        j.Stack,
		Location:     pgTextOrEmpty(j.Location),
		LocationCity: pgTextOrEmpty(j.LocationCity),
		Mode:         string(j.Mode),
		Level:        string(j.Level),
		SourceURL:    j.SourceUrl,
		PostedAt:     pgTimePtr(j.PostedAt),
		FirstSeenAt:  pgTime(j.FirstSeenAt),
		LastSeenAt:   pgTime(j.LastSeenAt),
		IsActive:     j.IsActive,
	}
}

// companyNames batch-resolves company names for jobs, tolerating an
// empty slice.
func (s *Server) companyNames(w http.ResponseWriter, r *http.Request, jobs ...db.Job) (map[uuid.UUID]string, bool) {
	ids := make([]uuid.UUID, 0, len(jobs))
	seen := make(map[uuid.UUID]bool, len(jobs))
	for _, j := range jobs {
		if !seen[j.CompanyID] {
			seen[j.CompanyID] = true
			ids = append(ids, j.CompanyID)
		}
	}

	names, err := s.store.CompanyNames(r.Context(), ids)
	if err != nil {
		s.logger.Error("resolve company names failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	return names, true
}

type listJobsResponse struct {
	Jobs       []jobSummary `json:"jobs"`
	NextCursor *string      `json:"next_cursor"`
}

// handleListJobs serves GET /api/jobs — filter (stack, salary_min, mode,
// level, city, q) + keyset pagination via an opaque cursor.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.ListJobsFilter{}

	if v := q.Get("stack"); v != "" {
		filter.Stack = strings.Split(v, ",")
	}

	if v := q.Get("salary_min"); v != "" {
		min, err := strconv.ParseInt(v, 10, 64)
		if err != nil || min < 0 {
			writeError(w, http.StatusBadRequest, "invalid salary_min")
			return
		}
		filter.SalaryMin = &min
	}

	if v := q.Get("mode"); v != "" {
		mode := normalizer.WorkMode(v)
		if !validWorkMode(mode) {
			writeError(w, http.StatusBadRequest, "invalid mode")
			return
		}
		filter.Mode = &mode
	}

	if v := q.Get("level"); v != "" {
		level := normalizer.ExperienceLevel(v)
		if !validExperienceLevel(level) {
			writeError(w, http.StatusBadRequest, "invalid level")
			return
		}
		filter.Level = &level
	}

	if v := q.Get("city"); v != "" {
		filter.City = &v
	}

	if v := q.Get("q"); v != "" {
		filter.Query = &v
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = int32(n)
	}

	if v := q.Get("cursor"); v != "" {
		cursor, err := decodeCursor(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		filter.Cursor = cursor
	}

	jobs, err := s.store.ListJobs(r.Context(), filter)
	if err != nil {
		s.logger.Error("list jobs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	names, ok := s.companyNames(w, r, jobs...)
	if !ok {
		return
	}

	resp := listJobsResponse{Jobs: make([]jobSummary, len(jobs))}
	for i, j := range jobs {
		resp.Jobs[i] = toJobSummary(j, names[j.CompanyID])
	}
	resp.NextCursor = encodeCursor(store.NextCursor(jobs, store.ClampLimit(filter.Limit)))

	writeJSON(w, http.StatusOK, resp)
}

type jobDetailResponse struct {
	Job        jobSummary   `json:"job"`
	Duplicates []jobSummary `json:"duplicates"`
}

// handleGetJob serves GET /api/jobs/{id} — the job plus every listing
// merged into it as a duplicate.
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := s.store.GetJobByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		s.logger.Error("get job failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	duplicates, err := s.store.ListDuplicatesOf(r.Context(), id)
	if err != nil {
		s.logger.Error("list duplicates failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	names, ok := s.companyNames(w, r, append([]db.Job{job}, duplicates...)...)
	if !ok {
		return
	}

	resp := jobDetailResponse{
		Job:        toJobSummary(job, names[job.CompanyID]),
		Duplicates: make([]jobSummary, len(duplicates)),
	}
	for i, d := range duplicates {
		resp.Duplicates[i] = toJobSummary(d, names[d.CompanyID])
	}

	writeJSON(w, http.StatusOK, resp)
}

func validWorkMode(m normalizer.WorkMode) bool {
	switch m {
	case normalizer.ModeOnsite, normalizer.ModeRemote, normalizer.ModeHybrid, normalizer.ModeUnknown:
		return true
	default:
		return false
	}
}

func validExperienceLevel(l normalizer.ExperienceLevel) bool {
	switch l {
	case normalizer.LevelIntern, normalizer.LevelJunior, normalizer.LevelMid, normalizer.LevelSenior, normalizer.LevelLead, normalizer.LevelUnknown:
		return true
	default:
		return false
	}
}
