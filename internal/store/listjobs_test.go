package store

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/normalizer"
	"github.com/ZoOwen/loker-id/internal/parser"
)

func TestListJobs_FiltersStack(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	go1 := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/go")
	go1.Stack = []string{"Go", "PostgreSQL"}
	mustUpsert(t, s, go1)

	php1 := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/php")
	php1.Stack = []string{"PHP", "Laravel"}
	mustUpsert(t, s, php1)

	got, err := s.ListJobs(ctx, ListJobsFilter{Stack: []string{"Go"}, Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	assertTitles(t, got, []string{go1.Title})
}

func TestListJobs_FiltersSalaryMin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	low := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/low")
	low.Title = "Low Pay"
	low.Salary = parser.Salary{Min: 3_000_000, Max: 5_000_000, Conf: parser.ConfRange}
	mustUpsert(t, s, low)

	high := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/high")
	high.Title = "High Pay"
	high.Salary = parser.Salary{Min: 15_000_000, Max: 20_000_000, Conf: parser.ConfRange}
	mustUpsert(t, s, high)

	unknown := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/unknown-salary")
	unknown.Title = "Unknown Pay"
	unknown.Salary = parser.Salary{Conf: parser.ConfUnknown}
	mustUpsert(t, s, unknown)

	min := int64(10_000_000)
	got, err := s.ListJobs(ctx, ListJobsFilter{SalaryMin: &min, Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	assertTitles(t, got, []string{"High Pay"})
}

func TestListJobs_FiltersModeLevelCity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	match := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/match")
	match.Title = "Match"
	match.Mode = normalizer.ModeRemote
	match.Level = normalizer.LevelSenior
	match.LocationCity = "Bandung"
	mustUpsert(t, s, match)

	wrongMode := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/wrong-mode")
	wrongMode.Title = "Wrong Mode"
	wrongMode.Mode = normalizer.ModeOnsite
	wrongMode.Level = normalizer.LevelSenior
	wrongMode.LocationCity = "Bandung"
	mustUpsert(t, s, wrongMode)

	wrongLevel := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/wrong-level")
	wrongLevel.Title = "Wrong Level"
	wrongLevel.Mode = normalizer.ModeRemote
	wrongLevel.Level = normalizer.LevelJunior
	wrongLevel.LocationCity = "Bandung"
	mustUpsert(t, s, wrongLevel)

	wrongCity := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/wrong-city")
	wrongCity.Title = "Wrong City"
	wrongCity.Mode = normalizer.ModeRemote
	wrongCity.Level = normalizer.LevelSenior
	wrongCity.LocationCity = "Surabaya"
	mustUpsert(t, s, wrongCity)

	mode := normalizer.ModeRemote
	level := normalizer.LevelSenior
	city := "Bandung"
	got, err := s.ListJobs(ctx, ListJobsFilter{Mode: &mode, Level: &level, City: &city, Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	assertTitles(t, got, []string{"Match"})
}

func TestListJobs_CityFilterIsCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	job := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/case")
	job.LocationCity = "Jakarta"
	mustUpsert(t, s, job)

	city := "jakarta"
	got, err := s.ListJobs(ctx, ListJobsFilter{City: &city, Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	assertTitles(t, got, []string{job.Title})
}

func TestListJobs_ExcludesDuplicatesAndInactive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	active := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/active")
	active.Title = "Active"
	mustUpsert(t, s, active)

	inactive := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/inactive")
	inactive.Title = "Inactive"
	inactiveResult := mustUpsert(t, s, inactive)
	if _, err := s.pool.Exec(ctx, "UPDATE jobs SET is_active = FALSE WHERE id = $1", inactiveResult.Job.ID); err != nil {
		t.Fatalf("deactivate job: %v", err)
	}

	canonical := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/canonical-for-dup-test")
	canonical.Title = "Canonical"
	canonical.Fingerprint = "list-dedup-fp"
	mustUpsert(t, s, canonical)

	dup := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/dup-for-list-test")
	dup.Title = "Duplicate"
	dup.Fingerprint = "list-dedup-fp"
	mustUpsert(t, s, dup)

	got, err := s.ListJobs(ctx, ListJobsFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}

	gotTitles := map[string]bool{}
	for _, j := range got {
		gotTitles[j.Title] = true
	}
	if !gotTitles["Active"] {
		t.Error("active job missing from results")
	}
	if gotTitles["Inactive"] {
		t.Error("inactive job present in results")
	}
	if !gotTitles["Canonical"] {
		t.Error("canonical job missing from results")
	}
	if gotTitles["Duplicate"] {
		t.Error("duplicate (non-canonical) job present in results")
	}
}

func TestListJobs_KeysetPaginationCoversEveryRowExactlyOnceInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sourceID := mustSourceID(t, s, "kalibrr")
	companyID := testCompany(t, s, "Acme")

	const n = 9
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantOrder := make([]string, 0, n+2)

	// Distinct posted_at, deliberately inserted out of chronological
	// order, to prove ListJobs sorts rather than relying on insert order.
	for i := n - 1; i >= 0; i-- {
		posted := base.Add(time.Duration(i) * 24 * time.Hour)
		p := testJobParams(companyID, sourceID, sourceURLForIndex(i))
		p.Title = titleForIndex(i)
		p.PostedAt = &posted
		mustUpsert(t, s, p)
	}
	for i := 0; i < n; i++ {
		wantOrder = append(wantOrder, titleForIndex(n-1-i)) // newest posted_at first
	}

	// Two jobs with unknown posted_at sort after every known posted_at.
	// Between themselves their order is intentionally id DESC, not
	// insertion order (id is a stable tiebreaker; first_seen_at is not —
	// two upserts a moment apart can land in the same transaction-start
	// timestamp), so which of the two comes first is unpredictable and
	// checked as a set below, not a fixed sequence.
	unknownA := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/unknown-a")
	unknownA.Title = "unknown-a"
	mustUpsert(t, s, unknownA)
	unknownB := testJobParams(companyID, sourceID, "https://kalibrr.com/jobs/unknown-b")
	unknownB.Title = "unknown-b"
	mustUpsert(t, s, unknownB)

	const pageSize = 4
	var (
		gotOrder []string
		cursor   *Cursor
		pages    int
	)
	for {
		pages++
		if pages > 20 {
			t.Fatal("too many pages — pagination is probably not terminating")
		}

		page, err := s.ListJobs(ctx, ListJobsFilter{Limit: pageSize, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListJobs() error = %v", err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > pageSize {
			t.Fatalf("page returned %d rows, want at most %d", len(page), pageSize)
		}

		for _, j := range page {
			gotOrder = append(gotOrder, j.Title)
		}

		cursor = NextCursor(page)
		if len(page) < pageSize {
			break // short page: this was the last one
		}
	}

	if len(gotOrder) != n+2 {
		t.Fatalf("walked %d jobs across %d pages, want %d\ngot:  %v\nwant: %v (+ unknown-a/unknown-b in either order)", len(gotOrder), pages, n+2, gotOrder, wantOrder)
	}

	// First n: exact order, since posted_at strictly decreases.
	for i := 0; i < n; i++ {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("order mismatch at position %d: got %v, want %v", i, gotOrder, wantOrder)
		}
	}

	// Last 2: unknown posted_at sorts after every known one, but which of
	// the two comes first is an unspecified, id-based tiebreak — checked
	// as a set.
	tail := map[string]bool{gotOrder[n]: true, gotOrder[n+1]: true}
	if !tail["unknown-a"] || !tail["unknown-b"] {
		t.Fatalf("last two rows = %v, want {unknown-a, unknown-b} in some order", gotOrder[n:])
	}
}

func sourceURLForIndex(i int) string {
	return "https://kalibrr.com/jobs/page-" + titleForIndex(i)
}

func titleForIndex(i int) string {
	return string(rune('a' + i))
}

func mustUpsert(t *testing.T, s *Store, p UpsertJobParams) UpsertJobResult {
	t.Helper()
	result, err := s.UpsertJob(context.Background(), p)
	if err != nil {
		t.Fatalf("UpsertJob(%q): %v", p.SourceURL, err)
	}
	return result
}

// assertTitles checks that got's titles are exactly want, ignoring order.
func assertTitles(t *testing.T, got []db.Job, want []string) {
	t.Helper()

	gotTitles := make([]string, len(got))
	for i, j := range got {
		gotTitles[i] = j.Title
	}

	sort.Strings(gotTitles)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)

	if len(gotTitles) != len(wantSorted) {
		t.Fatalf("titles = %v, want %v", gotTitles, wantSorted)
	}
	for i := range wantSorted {
		if gotTitles[i] != wantSorted[i] {
			t.Fatalf("titles = %v, want %v", gotTitles, wantSorted)
		}
	}
}
