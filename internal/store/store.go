// Package store persists NormalizedJob rows to Postgres and implements
// the dedup rules on top of the sqlc-generated queries in internal/db:
// an exact fingerprint match, or a same-company fuzzy title match, is a
// duplicate; the canonical posting is whichever has the earliest
// posted_at, tie-broken by the earliest first_seen_at.
package store

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ZoOwen/loker-id/internal/db"
)

// Store wraps a connection pool and the generated queries.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// New builds a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:    pool,
		queries: db.New(pool),
	}
}

func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func pgInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

func pgTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
