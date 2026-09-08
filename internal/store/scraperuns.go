package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/db"
)

// CreateScrapeRun starts (and records) a scrape run for sourceID.
func (s *Store) CreateScrapeRun(ctx context.Context, sourceID int32) (uuid.UUID, error) {
	id, err := s.queries.CreateScrapeRun(ctx, sourceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: create scrape run: %w", err)
	}
	return id, nil
}

// FinishScrapeRunParams reports the outcome of a scrape run. ErrorMessage
// is "" when the run succeeded.
type FinishScrapeRunParams struct {
	ID            uuid.UUID
	Status        db.RunStatus
	JobsFound     int32
	JobsNew       int32
	JobsDuplicate int32
	ErrorMessage  string
}

// FinishScrapeRun records how a scrape run ended.
func (s *Store) FinishScrapeRun(ctx context.Context, p FinishScrapeRunParams) error {
	if err := s.queries.FinishScrapeRun(ctx, db.FinishScrapeRunParams{
		ID:            p.ID,
		Status:        p.Status,
		JobsFound:     p.JobsFound,
		JobsNew:       p.JobsNew,
		JobsDuplicate: p.JobsDuplicate,
		ErrorMessage:  pgText(p.ErrorMessage),
	}); err != nil {
		return fmt.Errorf("store: finish scrape run: %w", err)
	}
	return nil
}
