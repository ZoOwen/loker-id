package store

import (
	"context"
	"testing"
	"time"
)

func TestUpsertJob_DedupByExactFingerprint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "same-fingerprint"
	a.TitleNormalized = "backendengineer"
	firstResult, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if firstResult.IsDuplicate {
		t.Fatalf("first job of a fingerprint pair was marked duplicate")
	}

	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "same-fingerprint"
	b.TitleNormalized = "completelydifferenttitlethatdoesnotmatch"
	secondResult, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	if !secondResult.IsDuplicate {
		t.Fatalf("job b with identical fingerprint was not marked duplicate")
	}
	if secondResult.CanonicalID != firstResult.Job.ID {
		t.Errorf("b's canonical = %s, want a's id %s", secondResult.CanonicalID, firstResult.Job.ID)
	}
}

func TestUpsertJob_DedupByCompanyAndFuzzyTitle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "fp-a"
	a.TitleNormalized = "seniorbackendengineerjakarta"
	first, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	// Fuzzy match: same company, near-identical (but not identical)
	// normalized title, different fingerprint (as if scraped from a
	// second source with slightly different formatting).
	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "fp-b-completely-different"
	b.TitleNormalized = "seniorbackendengineerjakartaid"
	second, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	if !second.IsDuplicate {
		t.Fatalf("job with fuzzy-matching title at the same company was not marked duplicate")
	}
	if second.CanonicalID != first.Job.ID {
		t.Errorf("b's canonical = %s, want a's id %s", second.CanonicalID, first.Job.ID)
	}
}

func TestUpsertJob_DedupIgnoresDifferentCompanyEvenWithMatchingTitle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyA := testCompany(t, s, "Acme")
	companyB := testCompany(t, s, "Widgets Inc")

	a := testJobParams(companyA, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "fp-a"
	a.TitleNormalized = "backendengineer"
	if _, err := s.UpsertJob(ctx, a); err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	b := testJobParams(companyB, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "fp-b"
	b.TitleNormalized = "backendengineer"
	second, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	if second.IsDuplicate {
		t.Errorf("jobs at two different companies were merged as duplicates")
	}
}

func TestUpsertJob_DedupPicksEarliestPostedAtAsCanonical(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	later := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Scraped first, but posted later — should NOT stay canonical once
	// the earlier-posted duplicate shows up.
	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "same-fp"
	a.PostedAt = &later
	firstUpsert, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "same-fp"
	b.PostedAt = &earlier
	secondUpsert, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	if secondUpsert.IsDuplicate {
		t.Fatalf("earlier-posted job b was itself marked duplicate; want it to become canonical")
	}

	var gotCanonical *string
	if err := s.pool.QueryRow(ctx, "SELECT canonical_job_id::text FROM jobs WHERE id = $1", firstUpsert.Job.ID).Scan(&gotCanonical); err != nil {
		t.Fatalf("read back job a: %v", err)
	}
	if gotCanonical == nil || *gotCanonical != secondUpsert.Job.ID.String() {
		t.Errorf("job a's canonical_job_id = %v, want it repointed to earlier-posted job b (%s)", gotCanonical, secondUpsert.Job.ID)
	}
}

func TestUpsertJob_DedupTieBreaksOnFirstSeenAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	posted := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "same-fp"
	a.PostedAt = &posted
	first, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "same-fp"
	b.PostedAt = &posted // identical posted_at
	second, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	// a was first_seen_at earlier (inserted first, in an earlier
	// transaction/timestamp) — a should remain canonical on a tied
	// posted_at.
	if !second.IsDuplicate || second.CanonicalID != first.Job.ID {
		t.Errorf("tied posted_at: expected b (%s) to be a duplicate of a (%s); got IsDuplicate=%v CanonicalID=%s",
			second.Job.ID, first.Job.ID, second.IsDuplicate, second.CanonicalID)
	}
}

func TestUpsertJob_DedupReparentsExistingDuplicatesWhenCanonicalDisplaced(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	later := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// a: canonical for a while, posted "later".
	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "fp-shared"
	a.PostedAt = &later
	aResult, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	// c: a duplicate of a, from a different, unrelated fingerprint but a
	// fuzzy-matching title at the same company — establishes a as c's
	// canonical.
	c := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/c")
	c.Fingerprint = "fp-c-unrelated"
	c.TitleNormalized = a.TitleNormalized + "x" // fuzzy match against a
	cResult, err := s.UpsertJob(ctx, c)
	if err != nil {
		t.Fatalf("upsert c: %v", err)
	}
	if !cResult.IsDuplicate || cResult.CanonicalID != aResult.Job.ID {
		t.Fatalf("setup failed: c should be a duplicate of a first")
	}

	// b: shares a's exact fingerprint, but posted earlier — should
	// displace a as canonical, and c (currently pointing at a) must be
	// repointed straight at b.
	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "fp-shared"
	b.PostedAt = &earlier
	bResult, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}
	if bResult.IsDuplicate {
		t.Fatalf("earlier-posted b should have become canonical, not a duplicate")
	}

	var cCanonical *string
	if err := s.pool.QueryRow(ctx, "SELECT canonical_job_id::text FROM jobs WHERE id = $1", cResult.Job.ID).Scan(&cCanonical); err != nil {
		t.Fatalf("read back c: %v", err)
	}
	if cCanonical == nil || *cCanonical != bResult.Job.ID.String() {
		t.Errorf("c's canonical_job_id = %v, want repointed straight to b (%s), not left on displaced a", cCanonical, bResult.Job.ID)
	}

	var aCanonical *string
	if err := s.pool.QueryRow(ctx, "SELECT canonical_job_id::text FROM jobs WHERE id = $1", aResult.Job.ID).Scan(&aCanonical); err != nil {
		t.Fatalf("read back a: %v", err)
	}
	if aCanonical == nil || *aCanonical != bResult.Job.ID.String() {
		t.Errorf("a's canonical_job_id = %v, want b (%s)", aCanonical, bResult.Job.ID)
	}
}

func TestMarkAsDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	a.Fingerprint = "fp-a"
	aResult, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	b := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/b")
	b.Fingerprint = "fp-b" // unrelated, wouldn't auto-dedup
	bResult, err := s.UpsertJob(ctx, b)
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	if err := s.MarkAsDuplicate(ctx, bResult.Job.ID, aResult.Job.ID); err != nil {
		t.Fatalf("MarkAsDuplicate() error = %v", err)
	}

	var canonical *string
	if err := s.pool.QueryRow(ctx, "SELECT canonical_job_id::text FROM jobs WHERE id = $1", bResult.Job.ID).Scan(&canonical); err != nil {
		t.Fatalf("read back b: %v", err)
	}
	if canonical == nil || *canonical != aResult.Job.ID.String() {
		t.Errorf("b's canonical_job_id = %v, want %s", canonical, aResult.Job.ID)
	}
}

func TestMarkAsDuplicate_RejectsSelfReference(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	a := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/a")
	result, err := s.UpsertJob(ctx, a)
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}

	if err := s.MarkAsDuplicate(ctx, result.Job.ID, result.Job.ID); err == nil {
		t.Error("MarkAsDuplicate(x, x) succeeded, want an error")
	}
}
