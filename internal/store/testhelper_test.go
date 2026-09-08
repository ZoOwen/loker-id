package store

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testSchemaCounter atomic.Int64

// newTestStore returns a Store backed by a freshly created Postgres schema
// migrated from the real migrations/001_init.sql — the same schema
// production runs against, not a hand-maintained copy — isolated from
// every other test via its own schema name (so tests can run concurrently
// without touching each other's rows), and torn down automatically via
// t.Cleanup. Skips the test if TEST_DATABASE_URL isn't set, so the suite
// degrades gracefully on a machine without Postgres available rather than
// failing outright.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping test that needs a real Postgres instance")
	}

	ctx := context.Background()
	schema := newTestSchemaName()

	setupConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for schema setup: %v", err)
	}
	defer setupConn.Close(ctx)

	// pg_trgm is database-wide, not schema-scoped; created once (idempotent)
	// so its functions (similarity(), used by FindDuplicateCandidates) are
	// resolvable once the new schema's search_path falls through to public.
	if _, err := setupConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("create pg_trgm extension: %v", err)
	}
	if _, err := setupConn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	// Every connection the pool opens — now or later, there's no way to
	// pin a single connection out of a pool — must have this schema first
	// in its search_path, with public still reachable for pg_trgm.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}

	schemaSQL, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		pool.Close()
		t.Fatalf("read migrations/001_init.sql: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schemaSQL)); err != nil {
		pool.Close()
		t.Fatalf("apply migrations/001_init.sql to test schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: connect to drop schema %s: %v", schema, err)
			return
		}
		defer conn.Close(dropCtx)

		if _, err := conn.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Logf("cleanup: drop schema %s: %v", schema, err)
		}
	})

	return New(pool)
}

func newTestSchemaName() string {
	n := testSchemaCounter.Add(1)
	return fmt.Sprintf("store_test_%d_%d", time.Now().UnixNano(), n)
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
