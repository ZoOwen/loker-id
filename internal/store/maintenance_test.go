package store

import (
	"context"
	"testing"
)

func TestDeactivateStaleJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	stale := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/stale")
	stale.Title = "Stale"
	staleResult := mustUpsert(t, s, stale)
	if _, err := s.pool.Exec(ctx, "UPDATE jobs SET last_seen_at = now() - interval '10 days' WHERE id = $1", staleResult.Job.ID); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	fresh := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/fresh")
	fresh.Title = "Fresh"
	freshResult := mustUpsert(t, s, fresh)

	n, err := s.DeactivateStaleJobs(ctx, 7)
	if err != nil {
		t.Fatalf("DeactivateStaleJobs() error = %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected = %d, want 1", n)
	}

	var staleActive, freshActive bool
	if err := s.pool.QueryRow(ctx, "SELECT is_active FROM jobs WHERE id = $1", staleResult.Job.ID).Scan(&staleActive); err != nil {
		t.Fatalf("read back stale job: %v", err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT is_active FROM jobs WHERE id = $1", freshResult.Job.ID).Scan(&freshActive); err != nil {
		t.Fatalf("read back fresh job: %v", err)
	}

	if staleActive {
		t.Error("stale job is still active after DeactivateStaleJobs")
	}
	if !freshActive {
		t.Error("fresh job was deactivated but shouldn't have been")
	}
}

func TestDeactivateStaleJobs_NoRowsAffectedWhenNothingIsStale(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	mustUpsert(t, s, testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/just-scraped"))

	n, err := s.DeactivateStaleJobs(ctx, 30)
	if err != nil {
		t.Fatalf("DeactivateStaleJobs() error = %v", err)
	}
	if n != 0 {
		t.Errorf("rows affected = %d, want 0", n)
	}
}
