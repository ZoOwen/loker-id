package store

import (
	"context"
	"fmt"
)

// DeactivateStaleJobs marks jobs not seen in a scrape for more than
// staleAfterDays as inactive, and returns how many rows were affected.
func (s *Store) DeactivateStaleJobs(ctx context.Context, staleAfterDays int32) (int64, error) {
	n, err := s.queries.DeactivateStaleJobs(ctx, staleAfterDays)
	if err != nil {
		return 0, fmt.Errorf("store: deactivate stale jobs: %w", err)
	}
	return n, nil
}
