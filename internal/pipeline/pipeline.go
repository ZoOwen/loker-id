// Package pipeline orchestrates one scrape end to end: create a
// scrape_runs row, run the source's scraper, normalize and persist each
// job (deduping as it goes), close out the run, and sweep stale listings.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/scraper"
	"github.com/ZoOwen/loker-id/internal/store"
)

const defaultStaleAfterDays = 7

// ErrUnknownSource is returned by Run for a slug with no registered
// scraper.
var ErrUnknownSource = errors.New("pipeline: no scraper registered for this source")

// Config holds Pipeline's tunables. Zero-value fields fall back to
// sensible defaults in New.
type Config struct {
	// StaleAfterDays: a job not seen in a scrape for this many days is
	// deactivated at the end of every run. Default 7.
	StaleAfterDays int32
	// Logger receives structured progress/outcome logs. Default
	// slog.Default().
	Logger *slog.Logger
}

// Pipeline runs scrapes for a fixed set of sources against a Store.
type Pipeline struct {
	store          *store.Store
	scrapers       map[string]scraper.Scraper
	staleAfterDays int32
	logger         *slog.Logger
}

// New builds a Pipeline. scrapers maps a source slug (matching the
// sources.slug column, e.g. "kalibrr") to the Scraper that handles it.
func New(st *store.Store, scrapers map[string]scraper.Scraper, cfg Config) *Pipeline {
	if cfg.StaleAfterDays <= 0 {
		cfg.StaleAfterDays = defaultStaleAfterDays
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Pipeline{
		store:          st,
		scrapers:       scrapers,
		staleAfterDays: cfg.StaleAfterDays,
		logger:         cfg.Logger,
	}
}

// HasSource reports whether slug has a registered scraper, so callers
// (e.g. an HTTP handler) can validate input before dispatching Run.
func (p *Pipeline) HasSource(slug string) bool {
	_, ok := p.scrapers[slug]
	return ok
}

// RunResult summarizes one Run. Errors collects every problem
// encountered — a failed job never aborts the run, it's recorded here
// and processing continues.
type RunResult struct {
	SourceSlug    string
	ScrapeRunID   uuid.UUID
	JobsFound     int
	JobsNew       int
	JobsDuplicate int
	JobsFailed    int
	Errors        []error
	Deactivated   int64
	Duration      time.Duration
}

// Run scrapes sourceSlug end to end. The returned error is non-nil only
// for a setup failure that prevents the run from starting at all (unknown
// source, can't create the scrape_runs row) — once a run is underway,
// problems (a failed job, the scraper erroring partway, a canceled
// context) are recorded in RunResult.Errors instead of failing Run,
// exactly so one bad job can't take down the whole run.
func (p *Pipeline) Run(ctx context.Context, sourceSlug string) (*RunResult, error) {
	start := time.Now()

	sc, ok := p.scrapers[sourceSlug]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, sourceSlug)
	}

	source, err := p.store.GetSourceBySlug(ctx, sourceSlug)
	if err != nil {
		return nil, fmt.Errorf("pipeline: look up source %q: %w", sourceSlug, err)
	}

	runID, err := p.store.CreateScrapeRun(ctx, source.ID)
	if err != nil {
		return nil, fmt.Errorf("pipeline: create scrape run: %w", err)
	}

	logger := p.logger.With("source", sourceSlug, "scrape_run_id", runID.String())
	logger.Info("scrape run started")

	result := &RunResult{SourceSlug: sourceSlug, ScrapeRunID: runID}

	rawJobs, scrapeErr := sc.Scrape(ctx)
	result.JobsFound = len(rawJobs)
	if scrapeErr != nil {
		logger.Error("scraper reported an error", "error", scrapeErr, "jobs_collected", len(rawJobs))
		result.Errors = append(result.Errors, fmt.Errorf("scrape: %w", scrapeErr))
	}

	for _, raw := range rawJobs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("context canceled after %d/%d jobs: %w",
				result.JobsNew+result.JobsDuplicate+result.JobsFailed, len(rawJobs), ctxErr))
			logger.Warn("context canceled, stopping early", "processed", result.JobsNew+result.JobsDuplicate+result.JobsFailed, "found", len(rawJobs))
			break
		}

		upserted, err := p.processJob(ctx, source.ID, raw)
		if err != nil {
			result.JobsFailed++
			result.Errors = append(result.Errors, fmt.Errorf("job %q: %w", raw.SourceURL, err))
			logger.Warn("failed to process job", "source_url", raw.SourceURL, "error", err)
			continue
		}

		if upserted.IsDuplicate {
			result.JobsDuplicate++
		} else {
			result.JobsNew++
		}
	}

	// From here on, use a context that survives the caller's own
	// cancellation: a run that got cut off mid-loop must still reach a
	// clean terminal state (not stuck at "running" forever) and still
	// get its stale-job sweep, since that's unrelated to whatever
	// canceled the original context.
	cleanupCtx := context.WithoutCancel(ctx)

	status := db.RunStatusSuccess
	if scrapeErr != nil && len(rawJobs) == 0 {
		status = db.RunStatusFailed
	}

	var errMsg string
	if len(result.Errors) > 0 {
		errMsg = errors.Join(result.Errors...).Error()
	}

	if err := p.store.FinishScrapeRun(cleanupCtx, store.FinishScrapeRunParams{
		ID:            runID,
		Status:        status,
		JobsFound:     int32(result.JobsFound),
		JobsNew:       int32(result.JobsNew),
		JobsDuplicate: int32(result.JobsDuplicate),
		ErrorMessage:  errMsg,
	}); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("finish scrape run: %w", err))
		logger.Error("failed to finish scrape run", "error", err)
	}

	if err := p.store.UpdateSourceLastRunAt(cleanupCtx, source.ID); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("update source last_run_at: %w", err))
		logger.Error("failed to update source last_run_at", "error", err)
	}

	deactivated, err := p.store.DeactivateStaleJobs(cleanupCtx, p.staleAfterDays)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("deactivate stale jobs: %w", err))
		logger.Error("failed to deactivate stale jobs", "error", err)
	} else {
		result.Deactivated = deactivated
	}

	result.Duration = time.Since(start)
	logger.Info("scrape run finished",
		"status", status,
		"jobs_found", result.JobsFound,
		"jobs_new", result.JobsNew,
		"jobs_duplicate", result.JobsDuplicate,
		"jobs_failed", result.JobsFailed,
		"deactivated", result.Deactivated,
		"duration", result.Duration,
		"errors", len(result.Errors),
	)

	return result, nil
}

// processJob normalizes and persists a single raw posting.
func (p *Pipeline) processJob(ctx context.Context, sourceID int32, raw scraper.RawJob) (store.UpsertJobResult, error) {
	normalized := normalizer.Normalize(raw)

	companyID, err := p.store.UpsertCompany(ctx, normalized.CompanyName, normalized.CompanyNormalized)
	if err != nil {
		return store.UpsertJobResult{}, fmt.Errorf("upsert company %q: %w", normalized.CompanyName, err)
	}

	result, err := p.store.UpsertJob(ctx, store.UpsertJobParams{
		Title:           normalized.Title,
		TitleNormalized: normalized.TitleNormalized,
		CompanyID:       companyID,
		Description:     normalized.Description,
		Salary:          normalized.Salary,
		Stack:           normalized.Stack,
		Location:        raw.Location,
		LocationCity:    normalized.LocationCity,
		Mode:            normalized.Mode,
		Level:           normalized.Level,
		SourceID:        sourceID,
		SourceURL:       raw.SourceURL,
		SourceJobID:     raw.SourceJobID,
		Fingerprint:     normalized.Fingerprint,
		PostedAt:        raw.PostedAt,
	})
	if err != nil {
		return store.UpsertJobResult{}, fmt.Errorf("upsert job: %w", err)
	}

	return result, nil
}
