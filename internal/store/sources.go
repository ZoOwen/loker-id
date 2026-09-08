package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ZoOwen/loker-id/internal/db"
)

// ErrSourceNotFound is returned by GetSourceBySlug when no source has
// that slug.
var ErrSourceNotFound = errors.New("store: source not found")

// GetSourceBySlug looks up a source by its slug (e.g. "kalibrr").
func (s *Store) GetSourceBySlug(ctx context.Context, slug string) (db.Source, error) {
	src, err := s.queries.GetSourceBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Source{}, ErrSourceNotFound
		}
		return db.Source{}, fmt.Errorf("store: get source by slug %q: %w", slug, err)
	}
	return src, nil
}

// UpdateSourceLastRunAt stamps sourceID as having just been scraped.
func (s *Store) UpdateSourceLastRunAt(ctx context.Context, sourceID int32) error {
	if err := s.queries.UpdateSourceLastRunAt(ctx, sourceID); err != nil {
		return fmt.Errorf("store: update source last_run_at: %w", err)
	}
	return nil
}
