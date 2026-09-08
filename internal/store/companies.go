package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/db"
)

// UpsertCompany inserts a company keyed on its normalized name, or
// refreshes the display name of an existing one — either way, returns its
// id.
func (s *Store) UpsertCompany(ctx context.Context, name, nameNormalized string) (uuid.UUID, error) {
	id, err := s.queries.UpsertCompany(ctx, db.UpsertCompanyParams{
		Name:           name,
		NameNormalized: nameNormalized,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: upsert company: %w", err)
	}
	return id, nil
}
