package store

import (
	"context"
	"testing"

	"github.com/ZoOwen/loker-id/internal/testdb"
)

// newTestStore returns a Store backed by a freshly created, isolated
// Postgres schema migrated from the real migrations/001_init.sql. See
// internal/testdb for the isolation/teardown mechanics shared across the
// project's test suites.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(testdb.NewPool(t))
}

// mustSourceID returns the id of the seeded source with the given slug
// (migrations/001_init.sql seeds "dealls" and "kalibrr"), for tests that
// need a valid jobs.source_id foreign key.
func mustSourceID(t *testing.T, s *Store, slug string) int32 {
	t.Helper()

	var id int32
	err := s.pool.QueryRow(context.Background(), "SELECT id FROM sources WHERE slug = $1", slug).Scan(&id)
	if err != nil {
		t.Fatalf("look up seeded source %q: %v", slug, err)
	}
	return id
}
