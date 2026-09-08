package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ZoOwen/loker-id/internal/db"
	"github.com/ZoOwen/loker-id/internal/parser"
)

// These exercise the pure decision logic (canonicalOf/postedBefore,
// salaryBoundsToDB) directly, without touching a database — the DB-backed
// tests in dedup_test.go and jobs_test.go cover the SQL side.

func jobWith(posted *time.Time, firstSeen time.Time) db.Job {
	j := db.Job{ID: uuid.New(), FirstSeenAt: pgtype.Timestamptz{Time: firstSeen, Valid: true}}
	if posted != nil {
		j.PostedAt = pgtype.Timestamptz{Time: *posted, Valid: true}
	}
	return j
}

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func timePtr(t time.Time) *time.Time { return &t }

func TestCanonicalOf(t *testing.T) {
	earlyPosted := jobWith(timePtr(ts("2026-01-01T00:00:00Z")), ts("2026-01-05T00:00:00Z"))
	latePosted := jobWith(timePtr(ts("2026-01-03T00:00:00Z")), ts("2026-01-02T00:00:00Z"))
	unknownPostedEarlySeen := jobWith(nil, ts("2026-01-01T00:00:00Z"))
	unknownPostedLateSeen := jobWith(nil, ts("2026-01-10T00:00:00Z"))
	tiedPostedEarlySeen := jobWith(timePtr(ts("2026-01-01T00:00:00Z")), ts("2026-01-01T00:00:00Z"))
	tiedPostedLateSeen := jobWith(timePtr(ts("2026-01-01T00:00:00Z")), ts("2026-01-02T00:00:00Z"))

	tests := []struct {
		name  string
		group []db.Job
		want  uuid.UUID
	}{
		{
			"earliest posted_at wins regardless of first_seen_at",
			[]db.Job{latePosted, earlyPosted},
			earlyPosted.ID,
		},
		{
			"known posted_at beats unknown posted_at",
			[]db.Job{unknownPostedEarlySeen, earlyPosted},
			earlyPosted.ID,
		},
		{
			"both unknown posted_at: earliest first_seen_at wins",
			[]db.Job{unknownPostedLateSeen, unknownPostedEarlySeen},
			unknownPostedEarlySeen.ID,
		},
		{
			"tied posted_at: earliest first_seen_at breaks the tie",
			[]db.Job{tiedPostedLateSeen, tiedPostedEarlySeen},
			tiedPostedEarlySeen.ID,
		},
		{
			"single job group returns itself",
			[]db.Job{earlyPosted},
			earlyPosted.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalOf(tt.group)
			if got.ID != tt.want {
				t.Errorf("canonicalOf(...) picked %s, want %s", got.ID, tt.want)
			}
		})
	}
}

func TestSalaryBoundsToDB(t *testing.T) {
	tests := []struct {
		name    string
		salary  parser.Salary
		wantMin pgtype.Int8
		wantMax pgtype.Int8
	}{
		{
			"unknown salary stores no bounds at all",
			parser.Salary{Min: 0, Max: 0, Conf: parser.ConfUnknown},
			pgtype.Int8{}, pgtype.Int8{},
		},
		{
			"exact salary stores both bounds equal",
			parser.Salary{Min: 10_000_000, Max: 10_000_000, Conf: parser.ConfExact},
			pgtype.Int8{Int64: 10_000_000, Valid: true}, pgtype.Int8{Int64: 10_000_000, Valid: true},
		},
		{
			"range salary stores both bounds",
			parser.Salary{Min: 8_000_000, Max: 12_000_000, Conf: parser.ConfRange},
			pgtype.Int8{Int64: 8_000_000, Valid: true}, pgtype.Int8{Int64: 12_000_000, Valid: true},
		},
		{
			"estimated max-only stores max, leaves min NULL (not zero)",
			parser.Salary{Min: 0, Max: 15_000_000, Conf: parser.ConfEstimated},
			pgtype.Int8{}, pgtype.Int8{Int64: 15_000_000, Valid: true},
		},
		{
			"estimated min-only stores min, leaves max NULL (not zero)",
			parser.Salary{Min: 8_000_000, Max: 0, Conf: parser.ConfEstimated},
			pgtype.Int8{Int64: 8_000_000, Valid: true}, pgtype.Int8{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			min, max := salaryBoundsToDB(tt.salary)
			if min != tt.wantMin {
				t.Errorf("salaryBoundsToDB(%+v) min = %+v, want %+v", tt.salary, min, tt.wantMin)
			}
			if max != tt.wantMax {
				t.Errorf("salaryBoundsToDB(%+v) max = %+v, want %+v", tt.salary, max, tt.wantMax)
			}
		})
	}
}
