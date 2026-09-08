// Package database sets up the pgx connection pool shared across the app.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates and verifies a pgx connection pool for databaseURL.
// maxConns caps the pool size — deliberately explicit rather than left at
// pgxpool's own default (which scales with the host's CPU count and, on a
// typical Render instance, comfortably exceeds what a free-tier Neon
// project allows across all of a project's connections). maxConns <= 0
// leaves pgxpool's default in place, for callers (tests, tooling) that
// don't need to think about the limit.
//
// sslmode is not handled here: pgx parses it straight off databaseURL's
// own query string (Neon's connection strings already include
// ?sslmode=require; local/test URLs use ?sslmode=disable — see
// .env.example), so there's nothing this package needs to add or
// override.
func NewPool(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}
