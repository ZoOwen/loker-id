package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func mustRandomUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom(): %v", err)
	}
	return id
}

func TestGetJobByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	created := mustUpsert(t, s, testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/detail"))

	got, err := s.GetJobByID(ctx, created.Job.ID)
	if err != nil {
		t.Fatalf("GetJobByID() error = %v", err)
	}
	if got.ID != created.Job.ID || got.Title != created.Job.Title {
		t.Errorf("GetJobByID() = %+v, want id=%s title=%q", got, created.Job.ID, created.Job.Title)
	}
}

func TestGetJobByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetJobByID(ctx, mustRandomUUID(t))
	if err != ErrJobNotFound {
		t.Errorf("GetJobByID() error = %v, want ErrJobNotFound", err)
	}
}

func TestListDuplicatesOf(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	canonical := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/canonical-dup-list")
	canonical.Fingerprint = "dup-list-fp"
	canonicalResult := mustUpsert(t, s, canonical)

	dup1 := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/dup1")
	dup1.Fingerprint = "dup-list-fp"
	dup1Result := mustUpsert(t, s, dup1)

	dup2 := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/dup2")
	dup2.Fingerprint = "dup-list-fp"
	dup2Result := mustUpsert(t, s, dup2)

	got, err := s.ListDuplicatesOf(ctx, canonicalResult.Job.ID)
	if err != nil {
		t.Fatalf("ListDuplicatesOf() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListDuplicatesOf() returned %d jobs, want 2", len(got))
	}

	ids := map[string]bool{got[0].ID.String(): true, got[1].ID.String(): true}
	if !ids[dup1Result.Job.ID.String()] || !ids[dup2Result.Job.ID.String()] {
		t.Errorf("ListDuplicatesOf() = %v, want %s and %s", got, dup1Result.Job.ID, dup2Result.Job.ID)
	}
}

func TestListDuplicatesOf_EmptyForUnrelatedJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	result := mustUpsert(t, s, testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/lonely"))

	got, err := s.ListDuplicatesOf(ctx, result.Job.ID)
	if err != nil {
		t.Fatalf("ListDuplicatesOf() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListDuplicatesOf() = %v, want empty", got)
	}
}

func TestCompanyNames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	idA := testCompany(t, s, "Acme")
	idB := testCompany(t, s, "Widgets Inc")

	names, err := s.CompanyNames(ctx, []uuid.UUID{idA, idB, mustRandomUUID(t)})
	if err != nil {
		t.Fatalf("CompanyNames() error = %v", err)
	}
	if names[idA] != "Acme" {
		t.Errorf("names[idA] = %q, want %q", names[idA], "Acme")
	}
	if names[idB] != "Widgets Inc" {
		t.Errorf("names[idB] = %q, want %q", names[idB], "Widgets Inc")
	}
	if len(names) != 2 {
		t.Errorf("len(names) = %d, want 2 (unknown id must be absent, not zero-valued)", len(names))
	}
}

func TestCompanyNames_EmptyInput(t *testing.T) {
	s := newTestStore(t)
	names, err := s.CompanyNames(context.Background(), nil)
	if err != nil {
		t.Fatalf("CompanyNames(nil) error = %v", err)
	}
	if len(names) != 0 {
		t.Errorf("CompanyNames(nil) = %v, want empty map", names)
	}
}

func TestGetSourceBySlug(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	src, err := s.GetSourceBySlug(ctx, "kalibrr")
	if err != nil {
		t.Fatalf("GetSourceBySlug() error = %v", err)
	}
	if src.Slug != "kalibrr" || src.Name != "Kalibrr" {
		t.Errorf("GetSourceBySlug() = %+v, want slug=kalibrr name=Kalibrr", src)
	}
}

func TestGetSourceBySlug_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSourceBySlug(context.Background(), "does-not-exist")
	if err != ErrSourceNotFound {
		t.Errorf("GetSourceBySlug() error = %v, want ErrSourceNotFound", err)
	}
}

func TestUpdateSourceLastRunAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")

	before, err := s.GetSourceBySlug(ctx, "kalibrr")
	if err != nil {
		t.Fatalf("GetSourceBySlug() error = %v", err)
	}
	if before.LastRunAt.Valid {
		t.Fatalf("seeded source already has last_run_at set: %v", before.LastRunAt)
	}

	if err := s.UpdateSourceLastRunAt(ctx, sourceID); err != nil {
		t.Fatalf("UpdateSourceLastRunAt() error = %v", err)
	}

	after, err := s.GetSourceBySlug(ctx, "kalibrr")
	if err != nil {
		t.Fatalf("GetSourceBySlug() error = %v", err)
	}
	if !after.LastRunAt.Valid {
		t.Error("last_run_at still NULL after UpdateSourceLastRunAt")
	}
}

func TestStats(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	goJob := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/stats-go")
	goJob.Stack = []string{"Go", "PostgreSQL"}
	mustUpsert(t, s, goJob)

	phpJob := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/stats-php")
	phpJob.Stack = []string{"PHP"}
	mustUpsert(t, s, phpJob)

	if err := s.UpdateSourceLastRunAt(ctx, sourceID); err != nil {
		t.Fatalf("UpdateSourceLastRunAt() error = %v", err)
	}

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	if stats.TotalActive != 2 {
		t.Errorf("TotalActive = %d, want 2", stats.TotalActive)
	}

	var kalibrrStat *SourceStat
	for i := range stats.BySource {
		if stats.BySource[i].Slug == "kalibrr" {
			kalibrrStat = &stats.BySource[i]
		}
	}
	if kalibrrStat == nil {
		t.Fatal("kalibrr missing from BySource")
	}
	if kalibrrStat.ActiveJobs != 2 {
		t.Errorf("kalibrr ActiveJobs = %d, want 2", kalibrrStat.ActiveJobs)
	}
	if kalibrrStat.LastRunAt == nil {
		t.Error("kalibrr LastRunAt = nil, want set")
	}

	var dealslStat *SourceStat
	for i := range stats.BySource {
		if stats.BySource[i].Slug == "dealls" {
			dealslStat = &stats.BySource[i]
		}
	}
	if dealslStat == nil {
		t.Fatal("dealls missing from BySource (sources with 0 jobs must still appear)")
	}
	if dealslStat.ActiveJobs != 0 {
		t.Errorf("dealls ActiveJobs = %d, want 0", dealslStat.ActiveJobs)
	}

	stackCounts := map[string]int64{}
	for _, s := range stats.ByStack {
		stackCounts[s.Stack] = s.JobCount
	}
	if stackCounts["Go"] != 1 || stackCounts["PostgreSQL"] != 1 || stackCounts["PHP"] != 1 {
		t.Errorf("ByStack = %+v, want Go=1 PostgreSQL=1 PHP=1", stats.ByStack)
	}
}

func TestListJobs_FiltersByFreeTextQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	backend := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/q-backend")
	backend.Title = "Backend Engineer"
	mustUpsert(t, s, backend)

	designer := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/q-designer")
	designer.Title = "Product Designer"
	mustUpsert(t, s, designer)

	q := "backend"
	got, err := s.ListJobs(ctx, ListJobsFilter{Query: &q, Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	assertTitles(t, got, []string{"Backend Engineer"})
}
