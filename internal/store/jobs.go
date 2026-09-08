package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
)

// UpsertJobParams is everything UpsertJob needs beyond what a NormalizedJob
// already carries: the resolved company id, which source scraped it, and
// the fields that only the raw posting has (description, raw location
// text, when it was posted). Optional text fields use "" for "not known";
// PostedAt uses nil for the same reason, matching scraper.RawJob.
type UpsertJobParams struct {
	Title           string
	TitleNormalized string
	CompanyID       uuid.UUID
	Description     string

	Salary parser.Salary

	Stack        []string
	Location     string
	LocationCity string
	Mode         normalizer.WorkMode
	Level        normalizer.ExperienceLevel

	SourceID    int32
	SourceURL   string
	SourceJobID string

	Fingerprint string
	PostedAt    *time.Time
}

// UpsertJobResult reports what happened to the job: whether it ended up
// marked as a duplicate (of itself, on this call or a previous one), and
// of which canonical job.
type UpsertJobResult struct {
	Job         db.Job
	IsDuplicate bool
	CanonicalID uuid.UUID
}

// UpsertJob inserts or refreshes a job (keyed on source_id+source_url),
// then — for a job not already known to be a duplicate — runs dedup
// resolution against other canonical jobs. Both steps run in one
// transaction: a re-scrape must never observably exist without its dedup
// status being consistent.
func (s *Store) UpsertJob(ctx context.Context, p UpsertJobParams) (UpsertJobResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UpsertJobResult{}, fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)

	salaryMin, salaryMax := salaryBoundsToDB(p.Salary)

	job, err := q.UpsertJob(ctx, db.UpsertJobParams{
		Title:           p.Title,
		TitleNormalized: p.TitleNormalized,
		CompanyID:       p.CompanyID,
		Description:     pgText(p.Description),
		SalaryMin:       salaryMin,
		SalaryMax:       salaryMax,
		SalaryConf:      db.SalaryConfidence(p.Salary.Conf),
		SalaryRaw:       pgText(p.Salary.Raw),
		Stack:           nonNilStrings(p.Stack),
		Location:        pgText(p.Location),
		LocationCity:    pgText(p.LocationCity),
		Mode:            db.WorkMode(p.Mode),
		Level:           db.ExperienceLevel(p.Level),
		SourceID:        p.SourceID,
		SourceUrl:       p.SourceURL,
		SourceJobID:     pgText(p.SourceJobID),
		Fingerprint:     p.Fingerprint,
		PostedAt:        pgTimestamptz(p.PostedAt),
	})
	if err != nil {
		return UpsertJobResult{}, fmt.Errorf("store: upsert job: %w", err)
	}

	result := UpsertJobResult{Job: job}

	if job.CanonicalJobID.Valid {
		// Already resolved as a duplicate by an earlier scrape; the
		// canonical_job_id column survives this upsert unchanged (see
		// jobs.sql), so there's nothing new to resolve.
		result.IsDuplicate = true
		result.CanonicalID = uuid.UUID(job.CanonicalJobID.Bytes)
	} else {
		winnerID, isDup, err := s.resolveDuplicates(ctx, q, job)
		if err != nil {
			return UpsertJobResult{}, fmt.Errorf("store: resolve duplicates: %w", err)
		}
		if isDup {
			result.IsDuplicate = true
			result.CanonicalID = winnerID
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return UpsertJobResult{}, fmt.Errorf("store: commit: %w", err)
	}

	return result, nil
}

// nonNilStrings coerces a nil slice to an empty one. jobs.stack is
// NOT NULL DEFAULT '{}' — the default only applies when a column is
// omitted from the INSERT entirely, not when it's explicitly bound to a
// value, and pgx encodes a nil []string as SQL NULL, which the column
// rejects. ExtractStack legitimately returns nil for a job matching none
// of the curated tech list, so this isn't a hypothetical case.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// salaryBoundsToDB translates parser.Salary's "0 means not stated"
// convention into proper SQL NULLs: unknown salaries store neither bound,
// and an estimated salary (only one side known) stores just that side.
func salaryBoundsToDB(s parser.Salary) (min, max pgtype.Int8) {
	switch s.Conf {
	case parser.ConfUnknown:
		return pgtype.Int8{}, pgtype.Int8{}
	case parser.ConfEstimated:
		if s.Min > 0 {
			min = pgInt8(s.Min)
		}
		if s.Max > 0 {
			max = pgInt8(s.Max)
		}
		return min, max
	default: // exact, range
		return pgInt8(s.Min), pgInt8(s.Max)
	}
}

// Cursor is a keyset pagination position: the (posted_at, id) of the last
// row on the previous page. PostedAt is nil when that row's own posted_at
// was unknown.
type Cursor struct {
	PostedAt *time.Time
	ID       uuid.UUID
}

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// ClampLimit applies ListJobs' own bounds to a requested page size — a
// caller that wants to know the *effective* limit up front (e.g. to tell
// whether a page it got back was short, meaning definitively no more
// results, via NextCursor) computes it the same way ListJobs does
// internally.
func ClampLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

// ListJobsFilter narrows ListJobs. A nil/zero field means "no filter" on
// that dimension. Stack matching is "any of" (array overlap); SalaryMin
// matches jobs whose salary_max clears the bar (see jobs.sql for why).
// Query is free-text search over title/stack/description (see
// search_vector in migrations/001_init.sql), parsed websearch-style
// (supports "quoted phrases", OR, -exclusion).
type ListJobsFilter struct {
	Stack     []string
	SalaryMin *int64
	Mode      *normalizer.WorkMode
	Level     *normalizer.ExperienceLevel
	City      *string
	Query     *string
	Cursor    *Cursor
	Limit     int32
}

// ListJobs returns active, canonical jobs matching filter, newest-posted
// first, using keyset pagination — never OFFSET. Pass NextCursor(result)
// as the next call's filter.Cursor to get the following page.
func (s *Store) ListJobs(ctx context.Context, filter ListJobsFilter) ([]db.Job, error) {
	limit := ClampLimit(filter.Limit)

	params := db.ListJobsParams{
		Stack:     filter.Stack,
		Mode:      nullWorkMode(filter.Mode),
		Level:     nullExperienceLevel(filter.Level),
		City:      nullPgText(filter.City),
		Query:     nullPgText(filter.Query),
		HasCursor: filter.Cursor != nil,
		PageLimit: limit,
	}
	if filter.SalaryMin != nil {
		params.SalaryMin = pgInt8(*filter.SalaryMin)
	}
	if filter.Cursor != nil {
		params.CursorID = pgtype.UUID{Bytes: filter.Cursor.ID, Valid: true}
		params.CursorPostedAt = pgTimestamptz(filter.Cursor.PostedAt)
	}

	jobs, err := s.queries.ListJobs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	return jobs, nil
}

// NextCursor builds the cursor for the page after jobs (as returned by
// ListJobs called with limit, which orders posted_at DESC, id DESC).
// Returns nil if jobs is empty, or came back shorter than limit — a short
// page conclusively means there's nothing more (ListJobs would have
// filled it otherwise), so callers can trust a nil result instead of
// needing an extra round trip that would just come back empty to confirm
// it. Pass limit through store.ClampLimit first if the original request
// didn't specify one (0) or exceeded the max — that's the effective limit
// ListJobs actually applied.
func NextCursor(jobs []db.Job, limit int32) *Cursor {
	if len(jobs) == 0 || int32(len(jobs)) < limit {
		return nil
	}
	last := jobs[len(jobs)-1]
	c := &Cursor{ID: last.ID}
	if last.PostedAt.Valid {
		t := last.PostedAt.Time
		c.PostedAt = &t
	}
	return c
}

func nullPgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgText(*s)
}

func nullWorkMode(m *normalizer.WorkMode) db.NullWorkMode {
	if m == nil {
		return db.NullWorkMode{}
	}
	return db.NullWorkMode{WorkMode: db.WorkMode(*m), Valid: true}
}

func nullExperienceLevel(l *normalizer.ExperienceLevel) db.NullExperienceLevel {
	if l == nil {
		return db.NullExperienceLevel{}
	}
	return db.NullExperienceLevel{ExperienceLevel: db.ExperienceLevel(*l), Valid: true}
}

// ErrJobNotFound is returned by GetJobByID when no job has that id.
var ErrJobNotFound = errors.New("store: job not found")

// GetJobByID fetches one job by id, regardless of its canonical status.
func (s *Store) GetJobByID(ctx context.Context, id uuid.UUID) (db.Job, error) {
	job, err := s.queries.GetJobByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Job{}, ErrJobNotFound
		}
		return db.Job{}, fmt.Errorf("store: get job by id: %w", err)
	}
	return job, nil
}

// ListDuplicatesOf returns every job pointing at canonicalID, oldest
// first. Empty if canonicalID has no duplicates, or is itself a
// duplicate — duplicates never have their own sub-duplicates, since
// ReparentDuplicates keeps every pointer flat.
func (s *Store) ListDuplicatesOf(ctx context.Context, canonicalID uuid.UUID) ([]db.Job, error) {
	jobs, err := s.queries.ListDuplicatesOf(ctx, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("store: list duplicates of %s: %w", canonicalID, err)
	}
	return jobs, nil
}

// MarkAsDuplicate is exposed directly (beyond the automatic resolution
// UpsertJob performs) for callers that need to merge jobs by hand, e.g. a
// manual moderation action.
func (s *Store) MarkAsDuplicate(ctx context.Context, jobID, canonicalID uuid.UUID) error {
	if jobID == canonicalID {
		return errors.New("store: a job cannot be marked as a duplicate of itself")
	}
	if err := s.queries.MarkAsDuplicate(ctx, db.MarkAsDuplicateParams{
		CanonicalJobID: canonicalID,
		JobID:          jobID,
	}); err != nil {
		return fmt.Errorf("store: mark as duplicate: %w", err)
	}
	return nil
}
