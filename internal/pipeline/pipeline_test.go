package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
	"github.com/ZoOwen/loker-id/internal/scraper"
	"github.com/ZoOwen/loker-id/internal/store"
	"github.com/ZoOwen/loker-id/internal/testdb"
)

// fakeScraper is a canned scraper.Scraper — no network calls, so pipeline
// tests are fast and deterministic instead of depending on kalibrr.com.
type fakeScraper struct {
	slug string
	jobs []scraper.RawJob
	err  error

	// onScrape, if set, runs synchronously when Scrape is called — e.g.
	// to trigger context cancellation deterministically at the exact
	// point between scraping and the per-job loop, instead of a
	// timing-based "cancel after N ms" that could flake.
	onScrape func()

	// gotMaxPages records the maxPages Scrape was actually called with,
	// so a test can assert Pipeline.Run threads its own maxPages
	// argument through unchanged.
	gotMaxPages int
}

func (f *fakeScraper) Source() string { return f.slug }

func (f *fakeScraper) Scrape(ctx context.Context, maxPages int) ([]scraper.RawJob, error) {
	f.gotMaxPages = maxPages
	if f.onScrape != nil {
		f.onScrape()
	}
	return f.jobs, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestPipeline sets up a Pipeline backed by a real, isolated test
// database, returning the pool alongside it so tests can read back rows
// directly for assertions the Store API doesn't otherwise expose.
func newTestPipeline(t *testing.T, sc *fakeScraper) (*Pipeline, *store.Store, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.NewPool(t)
	st := store.New(pool)
	p := New(st, map[string]scraper.Scraper{sc.slug: sc}, Config{Logger: testLogger(), StaleAfterDays: 7})
	return p, st, pool
}

func rawJob(sourceURL, title, company string) scraper.RawJob {
	return scraper.RawJob{
		Title:       title,
		Company:     company,
		Location:    "Jakarta, Indonesia",
		Description: "A job description.",
		SourceURL:   sourceURL,
		SourceJobID: sourceURL,
	}
}

func TestRun_ThreadsMaxPagesThroughToTheScraperUnchanged(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr"}
	p, _, _ := newTestPipeline(t, sc)

	if _, err := p.Run(context.Background(), "kalibrr", 17); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if sc.gotMaxPages != 17 {
		t.Errorf("scraper received maxPages = %d, want 17", sc.gotMaxPages)
	}
}

func TestRun_HappyPath(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/1", "Backend Engineer", "Acme"),
		rawJob("https://kalibrr.com/jobs/2", "Frontend Engineer", "Widgets Inc"),
	}}
	p, st, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.JobsFound != 2 {
		t.Errorf("JobsFound = %d, want 2", result.JobsFound)
	}
	if result.JobsNew != 2 {
		t.Errorf("JobsNew = %d, want 2", result.JobsNew)
	}
	if result.JobsDuplicate != 0 {
		t.Errorf("JobsDuplicate = %d, want 0", result.JobsDuplicate)
	}
	if result.JobsFailed != 0 {
		t.Errorf("JobsFailed = %d, want 0", result.JobsFailed)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}

	var status string
	var jobsFound, jobsNew, jobsDup int32
	err = pool.QueryRow(ctx,
		"SELECT status, jobs_found, jobs_new, jobs_duplicate FROM scrape_runs WHERE id = $1", result.ScrapeRunID,
	).Scan(&status, &jobsFound, &jobsNew, &jobsDup)
	if err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusSuccess) {
		t.Errorf("scrape_runs.status = %q, want %q", status, db.RunStatusSuccess)
	}
	if jobsFound != 2 || jobsNew != 2 || jobsDup != 0 {
		t.Errorf("scrape_runs counts = found=%d new=%d dup=%d, want 2/2/0", jobsFound, jobsNew, jobsDup)
	}

	src, err := st.GetSourceBySlug(ctx, "kalibrr")
	if err != nil {
		t.Fatalf("GetSourceBySlug() error = %v", err)
	}
	if !src.LastRunAt.Valid {
		t.Error("sources.last_run_at is still NULL after a run")
	}
}

// TestRun_StoresSanitizedDescriptionNotRawHTML guards against exactly the
// wiring bug this fixed: processJob used to pass raw.Description (straight
// HTML from the scraper) to the store instead of normalized.Description
// (SanitizeDescription's cleaned output). Checked through the real
// database, not just at the normalizer unit level, since that's the layer
// where the bug actually was.
func TestRun_StoresSanitizedDescriptionNotRawHTML(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		{
			Title:       "Backend Engineer",
			Company:     "Acme",
			Description: `<ul><li>Familiar with <strong>Docker</strong></li><li class="foo">Second point</li></ul>`,
			SourceURL:   "https://kalibrr.com/jobs/html-description",
			SourceJobID: "1",
		},
	}}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	if _, err := p.Run(ctx, "kalibrr", 3); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var description string
	err := pool.QueryRow(ctx, "SELECT description FROM jobs WHERE source_url = $1", "https://kalibrr.com/jobs/html-description").Scan(&description)
	if err != nil {
		t.Fatalf("read back job: %v", err)
	}

	if strings.Contains(description, "<") {
		t.Errorf("stored description still contains raw HTML: %q", description)
	}
	want := "- Familiar with Docker\n- Second point"
	if description != want {
		t.Errorf("stored description = %q, want %q", description, want)
	}
}

func TestRun_DetectsDuplicates(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/orig", "Backend Engineer", "Acme"),
		rawJob("https://kalibrr.com/jobs/repost", "Backend Engineer", "Acme"), // same company + title -> fuzzy dedup
	}}
	p, _, _ := newTestPipeline(t, sc)

	result, err := p.Run(context.Background(), "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.JobsNew != 1 {
		t.Errorf("JobsNew = %d, want 1", result.JobsNew)
	}
	if result.JobsDuplicate != 1 {
		t.Errorf("JobsDuplicate = %d, want 1", result.JobsDuplicate)
	}
}

// TestRun_SameRawJobTwiceCountsAsOneNewAndOneDuplicate guards the scenario
// multi-keyword scraping introduces: a scraper (e.g. Kalibrr, searched
// once per keyword) can return the exact same posting — same
// SourceURL/SourceJobID — more than once in a single Scrape() call,
// because the posting matched more than one keyword. That must land as
// one row in the DB, counted once as new and once as a duplicate — not
// two rows, and not "new" twice.
func TestRun_SameRawJobTwiceCountsAsOneNewAndOneDuplicate(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/cross-keyword", "Backend Engineer", "Acme"),
		rawJob("https://kalibrr.com/jobs/cross-keyword", "Backend Engineer", "Acme"),
	}}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.JobsFound != 2 {
		t.Errorf("JobsFound = %d, want 2 (the scraper did return it twice)", result.JobsFound)
	}
	if result.JobsNew != 1 {
		t.Errorf("JobsNew = %d, want 1", result.JobsNew)
	}
	if result.JobsDuplicate != 1 {
		t.Errorf("JobsDuplicate = %d, want 1 (the second occurrence is a re-scrape of the same row, not a fresh one)", result.JobsDuplicate)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE source_url = $1", "https://kalibrr.com/jobs/cross-keyword").Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Errorf("jobs with this source_url = %d, want 1 (must not create a second row)", count)
	}
}

func TestRun_UnknownSourceReturnsErrorWithoutCreatingARun(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr"}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	_, err := p.Run(ctx, "does-not-exist", 3)
	if err == nil {
		t.Fatal("Run() error = nil, want an error for an unregistered source slug")
	}
	if !errors.Is(err, ErrUnknownSource) {
		t.Errorf("Run() error = %v, want it to wrap ErrUnknownSource", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM scrape_runs").Scan(&count); err != nil {
		t.Fatalf("count scrape_runs: %v", err)
	}
	if count != 0 {
		t.Errorf("scrape_runs count = %d, want 0 (no run should be created for an unknown source)", count)
	}
}

func TestRun_OneJobFailingDoesNotStopOthers(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/good1", "Backend Engineer", "Acme"),
		{
			// A NUL byte in a text column is genuinely rejected by
			// Postgres ("null character not permitted") — a real,
			// deterministic failure mode a scraper could hit from a
			// mis-encoded page, not a contrived one.
			Title:       "Broken Engineer",
			Company:     "Acme",
			Description: "bad\x00description",
			SourceURL:   "https://kalibrr.com/jobs/broken",
			SourceJobID: "broken",
		},
		rawJob("https://kalibrr.com/jobs/good2", "Data Engineer", "Acme"),
	}}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (per-job failures must not fail Run itself)", err)
	}

	if result.JobsFound != 3 {
		t.Errorf("JobsFound = %d, want 3", result.JobsFound)
	}
	if result.JobsFailed != 1 {
		t.Errorf("JobsFailed = %d, want 1", result.JobsFailed)
	}
	if result.JobsNew != 2 {
		t.Errorf("JobsNew = %d, want 2 (the two good jobs, despite the bad one in between)", result.JobsNew)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(result.Errors))
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs").Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 2 {
		t.Errorf("jobs in DB = %d, want 2 (both good jobs persisted despite the failure)", count)
	}

	// The run is still reported as an overall success — most of it
	// worked, and the failure is visible via error_message /
	// jobs_found-jobs_new-jobs_duplicate, not by hiding a real run
	// behind a blanket "failed".
	var status string
	var errMsg *string
	err = pool.QueryRow(ctx,
		"SELECT status, error_message FROM scrape_runs WHERE id = $1", result.ScrapeRunID,
	).Scan(&status, &errMsg)
	if err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusSuccess) {
		t.Errorf("scrape_runs.status = %q, want %q", status, db.RunStatusSuccess)
	}
	if errMsg == nil || *errMsg == "" {
		t.Error("scrape_runs.error_message is empty, want the per-job failure recorded")
	}
}

func TestRun_ScraperFailsCompletely(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", err: errBoom}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (the failure is reported via RunResult, not as Run's own error)", err)
	}

	if result.JobsFound != 0 {
		t.Errorf("JobsFound = %d, want 0", result.JobsFound)
	}
	if len(result.Errors) == 0 {
		t.Error("Errors is empty, want the scrape failure recorded")
	}

	var status string
	err = pool.QueryRow(ctx, "SELECT status FROM scrape_runs WHERE id = $1", result.ScrapeRunID).Scan(&status)
	if err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusFailed) {
		t.Errorf("scrape_runs.status = %q, want %q (scraper produced nothing at all)", status, db.RunStatusFailed)
	}
}

func TestRun_ScraperFailsPartway_StillProcessesWhatItCollected(t *testing.T) {
	sc := &fakeScraper{
		slug: "kalibrr",
		jobs: []scraper.RawJob{rawJob("https://kalibrr.com/jobs/collected", "Backend Engineer", "Acme")},
		err:  errBoom,
	}
	p, _, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.JobsNew != 1 {
		t.Errorf("JobsNew = %d, want 1 (the job collected before the scraper's error must still be saved)", result.JobsNew)
	}
	if len(result.Errors) == 0 {
		t.Error("Errors is empty, want the scrape error recorded even though some jobs were collected")
	}

	// Partial results still count as an overall success: the run
	// produced something real, it just didn't finish scraping.
	var status string
	err = pool.QueryRow(ctx, "SELECT status FROM scrape_runs WHERE id = $1", result.ScrapeRunID).Scan(&status)
	if err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status != string(db.RunStatusSuccess) {
		t.Errorf("scrape_runs.status = %q, want %q", status, db.RunStatusSuccess)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs").Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Errorf("jobs in DB = %d, want 1", count)
	}
}

func TestRun_DeactivatesStaleJobsFromAnyPreviousRun(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/fresh", "Backend Engineer", "Acme"),
	}}
	p, st, pool := newTestPipeline(t, sc)
	ctx := context.Background()

	// A stale job from some earlier run, unrelated to what's being
	// scraped now.
	src, err := st.GetSourceBySlug(ctx, "kalibrr")
	if err != nil {
		t.Fatalf("GetSourceBySlug() error = %v", err)
	}
	companyID, err := st.UpsertCompany(ctx, "Old Co", "old co")
	if err != nil {
		t.Fatalf("UpsertCompany() error = %v", err)
	}
	staleResult, err := st.UpsertJob(ctx, store.UpsertJobParams{
		Title: "Stale Job", TitleNormalized: "stalejob", CompanyID: companyID,
		Salary:   parser.Salary{Conf: parser.ConfUnknown},
		Mode:     normalizer.ModeUnknown,
		Level:    normalizer.LevelUnknown,
		SourceID: src.ID, SourceURL: "https://kalibrr.com/jobs/stale", Fingerprint: "stale-fp",
	})
	if err != nil {
		t.Fatalf("seed stale job: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE jobs SET last_seen_at = now() - interval '30 days' WHERE id = $1", staleResult.Job.ID); err != nil {
		t.Fatalf("backdate stale job: %v", err)
	}

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Deactivated != 1 {
		t.Errorf("Deactivated = %d, want 1", result.Deactivated)
	}

	var isActive bool
	if err := pool.QueryRow(ctx, "SELECT is_active FROM jobs WHERE id = $1", staleResult.Job.ID).Scan(&isActive); err != nil {
		t.Fatalf("read back stale job: %v", err)
	}
	if isActive {
		t.Error("stale job is still active after a run")
	}
}

func TestRun_FailsCleanlyWithAlreadyCanceledContext(t *testing.T) {
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/1", "Backend Engineer", "Acme"),
	}}
	p, _, pool := newTestPipeline(t, sc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Can't even create the scrape_runs row with a dead context — Run
	// should fail fast and cleanly here, not panic or hang.
	_, err := p.Run(ctx, "kalibrr", 3)
	if err == nil {
		t.Fatal("Run() error = nil, want an error for an already-canceled context")
	}

	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM scrape_runs").Scan(&count); err != nil {
		t.Fatalf("count scrape_runs: %v", err)
	}
	if count != 0 {
		t.Errorf("scrape_runs count = %d, want 0 (nothing should be created if the run can't even start)", count)
	}
}

func TestRun_CancellationMidLoopStillFinishesTheRunRow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sc := &fakeScraper{slug: "kalibrr", jobs: []scraper.RawJob{
		rawJob("https://kalibrr.com/jobs/1", "Backend Engineer", "Acme"),
		rawJob("https://kalibrr.com/jobs/2", "Frontend Engineer", "Acme"),
	}}
	sc.onScrape = cancel // cancel right as Scrape returns, before the per-job loop runs
	p, _, pool := newTestPipeline(t, sc)

	result, err := p.Run(ctx, "kalibrr", 3)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil — a mid-run cancellation is reported via RunResult, and the run row must still be finished", err)
	}
	if len(result.Errors) == 0 {
		t.Error("Errors is empty, want the cancellation recorded")
	}

	// Despite the cancellation, FinishScrapeRun/UpdateSourceLastRunAt/
	// DeactivateStaleJobs must still have run to completion (using a
	// context that survives the caller's cancellation) — status must not
	// be left at "running" forever.
	var status string
	err = pool.QueryRow(context.Background(), "SELECT status FROM scrape_runs WHERE id = $1", result.ScrapeRunID).Scan(&status)
	if err != nil {
		t.Fatalf("read back scrape run: %v", err)
	}
	if status == string(db.RunStatusRunning) {
		t.Errorf("scrape_runs.status = %q, want it resolved to success or failed, not left running", status)
	}
}

var errBoom = errors.New("scraper: boom")
