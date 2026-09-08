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

// CompanyNames batch-resolves company display names, for enriching a page
// of jobs (which only carry a company_id) into an API response without an
// N+1 query per row. Missing ids are simply absent from the result map.
func (s *Store) CompanyNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]string{}, nil
	}

	companies, err := s.queries.ListCompaniesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("store: list companies by ids: %w", err)
	}

	names := make(map[uuid.UUID]string, len(companies))
	for _, c := range companies {
		names[c.ID] = c.Name
	}
	return names, nil
}
