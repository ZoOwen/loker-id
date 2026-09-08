package store

import (
	"context"
	"fmt"
	"time"
)

// SourceStat is one source's contribution to Stats.
type SourceStat struct {
	Slug       string
	Name       string
	ActiveJobs int64
	LastRunAt  *time.Time // nil if never scraped
}

// StackStat is one technology's share of Stats, most common first.
type StackStat struct {
	Stack    string
	JobCount int64
}

// Stats is the aggregate view backing GET /api/stats.
type Stats struct {
	TotalActive int64
	BySource    []SourceStat
	ByStack     []StackStat
}

// Stats computes the current aggregate counts: total active (canonical)
// jobs, a per-source breakdown (including sources with zero jobs), and a
// per-technology breakdown.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	total, err := s.queries.CountActiveJobs(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("store: count active jobs: %w", err)
	}

	sourceRows, err := s.queries.SourceStats(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("store: source stats: %w", err)
	}
	bySource := make([]SourceStat, len(sourceRows))
	for i, r := range sourceRows {
		stat := SourceStat{Slug: r.Slug, Name: r.Name, ActiveJobs: r.ActiveJobs}
		if r.LastRunAt.Valid {
			t := r.LastRunAt.Time
			stat.LastRunAt = &t
		}
		bySource[i] = stat
	}

	stackRows, err := s.queries.StackStats(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("store: stack stats: %w", err)
	}
	byStack := make([]StackStat, len(stackRows))
	for i, r := range stackRows {
		byStack[i] = StackStat{Stack: r.Stack, JobCount: r.JobCount}
	}

	return Stats{TotalActive: total, BySource: bySource, ByStack: byStack}, nil
}
