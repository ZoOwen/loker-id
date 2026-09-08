// Package testdb provides a real, migrated Postgres instance for tests
// across the project (internal/store, internal/pipeline, internal/server,
// ...), each isolated via its own throwaway schema rather than a shared
// fixture database.
//
// This is a regular (non _test.go) package deliberately: Go never
// compiles _test.go files into anything other packages can import, so a
// helper meant to be shared across multiple packages' test suites has to
// live in an ordinary package instead — it just happens to only ever be
// used from tests.
package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var schemaCounter atomic.Int64

// NewPool returns a pgxpool.Pool backed by a freshly created Postgres
// schema migrated from the real migrations/001_init.sql — the same
// schema production runs against, not a hand-maintained copy — isolated
// from every other test via its own schema name (safe for concurrent
// test runs), and torn down automatically via t.Cleanup. Skips the test
// if TEST_DATABASE_URL isn't set, so the suite degrades gracefully on a
// machine without Postgres available.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping test that needs a real Postgres instance")
	}

	ctx := context.Background()
	schema := newSchemaName()

	setupConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("testdb: connect for schema setup: %v", err)
	}
	defer setupConn.Close(ctx)

	// pg_trgm is database-wide, not schema-scoped; created once
	// (idempotent) so its functions (similarity(), used by
	// FindDuplicateCandidates) are resolvable once the new schema's
	// search_path falls through to public.
	if _, err := setupConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("testdb: create pg_trgm extension: %v", err)
	}
	if _, err := setupConn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("testdb: create test schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("testdb: parse pool config: %v", err)
	}
	// Every connection the pool opens - now or later, there's no way to
	// pin a single connection out of a pool - must have this schema
	// first in its search_path, with public still reachable for
	// pg_trgm.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("testdb: open pool: %v", err)
	}

	schemaSQL, err := os.ReadFile(migrationPath())
	if err != nil {
		pool.Close()
		t.Fatalf("testdb: read migrations/001_init.sql: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schemaSQL)); err != nil {
		pool.Close()
		t.Fatalf("testdb: apply migrations/001_init.sql to test schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		conn, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("testdb: cleanup: connect to drop schema %s: %v", schema, err)
			return
		}
		defer conn.Close(dropCtx)

		if _, err := conn.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Logf("testdb: cleanup: drop schema %s: %v", schema, err)
		}
	})

	return pool
}

func newSchemaName() string {
	n := schemaCounter.Add(1)
	return fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), n)
}

// migrationPath locates migrations/001_init.sql relative to this source
// file via runtime.Caller, rather than the caller's working directory —
// NewPool is called from several different package directories
// (internal/store, internal/pipeline, internal/server, ...), so a path
// relative to "whichever test's cwd happens to invoke this" can't work
// uniformly. This file's own location is fixed (internal/testdb/), so
// walking up from it is reliable regardless of who's calling.
func migrationPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "001_init.sql")
}
