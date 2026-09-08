package store

import (
	"context"
	"testing"
	"time"

	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
)

func TestUpsertJob_InsertsNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	params := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/1")
	result, err := s.UpsertJob(ctx, params)
	if err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}

	if result.IsDuplicate {
		t.Errorf("IsDuplicate = true for a fresh, unmatched job")
	}
	if result.Job.Title != params.Title {
		t.Errorf("Title = %q, want %q", result.Job.Title, params.Title)
	}
	if !result.Job.IsActive {
		t.Errorf("IsActive = false for a freshly inserted job")
	}
	if result.Job.CanonicalJobID.Valid {
		t.Errorf("CanonicalJobID = %v, want NULL for a fresh unmatched job", result.Job.CanonicalJobID)
	}
}

func TestUpsertJob_ReScrapeRefreshesContentAndReactivates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	params := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/1")
	first, err := s.UpsertJob(ctx, params)
	if err != nil {
		t.Fatalf("first UpsertJob() error = %v", err)
	}

	// Simulate the listing having gone inactive since the first scrape.
	if _, err := s.pool.Exec(ctx, "UPDATE jobs SET is_active = FALSE, last_seen_at = now() - interval '1 day' WHERE id = $1", first.Job.ID); err != nil {
		t.Fatalf("simulate stale job: %v", err)
	}

	updated := params
	updated.Title = "Senior Backend Engineer"
	updated.Salary = parser.Salary{Min: 15_000_000, Max: 20_000_000, Conf: parser.ConfRange}

	second, err := s.UpsertJob(ctx, updated)
	if err != nil {
		t.Fatalf("second UpsertJob() error = %v", err)
	}

	if second.Job.ID != first.Job.ID {
		t.Fatalf("re-scrape created a new row: %s, want same id %s", second.Job.ID, first.Job.ID)
	}
	if second.Job.Title != "Senior Backend Engineer" {
		t.Errorf("Title after re-scrape = %q, want %q", second.Job.Title, "Senior Backend Engineer")
	}
	if !second.Job.IsActive {
		t.Errorf("IsActive after re-scrape = false, want true (re-scrape must reactivate)")
	}
	if !second.Job.SalaryMin.Valid || second.Job.SalaryMin.Int64 != 15_000_000 {
		t.Errorf("SalaryMin after re-scrape = %+v, want 15000000", second.Job.SalaryMin)
	}

	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE source_id = $1 AND source_url = $2", sourceID, params.SourceURL).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Errorf("jobs with this (source_id, source_url) = %d, want 1 (upsert must not duplicate)", count)
	}
}

func TestUpsertJob_RescrapeNeverClobbersCanonicalJobID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	canonicalParams := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/canonical")
	canonicalParams.Fingerprint = "shared-fingerprint"
	canonical, err := s.UpsertJob(ctx, canonicalParams)
	if err != nil {
		t.Fatalf("upsert canonical: %v", err)
	}

	dupParams := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/dup")
	dupParams.Fingerprint = "shared-fingerprint"
	dup, err := s.UpsertJob(ctx, dupParams)
	if err != nil {
		t.Fatalf("upsert duplicate: %v", err)
	}
	if !dup.IsDuplicate || dup.CanonicalID != canonical.Job.ID {
		t.Fatalf("expected dup to be marked duplicate of canonical, got IsDuplicate=%v CanonicalID=%s", dup.IsDuplicate, dup.CanonicalID)
	}

	// Re-scrape the duplicate — its own content changes, but it must
	// remain a duplicate of the same canonical.
	reScraped := dupParams
	reScraped.Title = "Backend Engineer (updated)"
	again, err := s.UpsertJob(ctx, reScraped)
	if err != nil {
		t.Fatalf("re-scrape duplicate: %v", err)
	}

	if !again.IsDuplicate {
		t.Errorf("re-scraped duplicate lost its duplicate status")
	}
	if again.CanonicalID != canonical.Job.ID {
		t.Errorf("re-scraped duplicate's canonical = %s, want unchanged %s", again.CanonicalID, canonical.Job.ID)
	}
	if again.Job.Title != "Backend Engineer (updated)" {
		t.Errorf("re-scraped duplicate's own content wasn't refreshed: Title = %q", again.Job.Title)
	}
}

func TestUpsertJob_PostedAtNilStoresNull(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	params := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/no-date")
	params.PostedAt = nil

	result, err := s.UpsertJob(ctx, params)
	if err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	if result.Job.PostedAt.Valid {
		t.Errorf("PostedAt.Valid = true, want false (nil PostedAt should store NULL)")
	}

	posted := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	params2 := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/with-date")
	params2.PostedAt = &posted

	result2, err := s.UpsertJob(ctx, params2)
	if err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	if !result2.Job.PostedAt.Valid || !result2.Job.PostedAt.Time.Equal(posted) {
		t.Errorf("PostedAt = %+v, want %v", result2.Job.PostedAt, posted)
	}
}

func TestUpsertJob_ModeAndLevelRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	params := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/mode-level")
	params.Mode = normalizer.ModeHybrid
	params.Level = normalizer.LevelSenior

	result, err := s.UpsertJob(ctx, params)
	if err != nil {
		t.Fatalf("UpsertJob() error = %v", err)
	}
	if string(result.Job.Mode) != string(normalizer.ModeHybrid) {
		t.Errorf("Mode = %q, want %q", result.Job.Mode, normalizer.ModeHybrid)
	}
	if string(result.Job.Level) != string(normalizer.LevelSenior) {
		t.Errorf("Level = %q, want %q", result.Job.Level, normalizer.LevelSenior)
	}
}
