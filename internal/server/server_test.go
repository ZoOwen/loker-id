package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ZoOwen/loker-id/internal/pipeline"
	"github.com/ZoOwen/loker-id/internal/scraper"
	"github.com/ZoOwen/loker-id/internal/store"
	"github.com/ZoOwen/loker-id/internal/testdb"
)

const testInternalToken = "test-internal-token"

// fakeScraper is a canned scraper.Scraper — no network calls, so tests
// are fast and deterministic instead of depending on kalibrr.com.
type fakeScraper struct {
	slug string
	jobs []scraper.RawJob
	err  error

	// gotMaxPages records the maxPages Scrape was actually called with,
	// so a test can assert the "pages" query param made it all the way
	// through the handler and pipeline unchanged. It's an atomic because
	// the handler runs Scrape in a background goroutine — a test reads
	// this from a different goroutine, after polling for some other
	// side effect (e.g. a persisted job row) to know the run finished.
	gotMaxPages atomic.Int32
}

func (f *fakeScraper) Source() string { return f.slug }

func (f *fakeScraper) Scrape(ctx context.Context, maxPages int) ([]scraper.RawJob, error) {
	f.gotMaxPages.Store(int32(maxPages))
	return f.jobs, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer starts an httptest.Server backed by a real, isolated
// Postgres schema, wired with a fake "kalibrr" scraper so POST
// /internal/scrape tests don't hit the real site. Returns the server, the
// Store (for seeding fixtures directly), the underlying pool (for
// assertions the Store API doesn't otherwise expose), and the fake
// scraper (for tests that control what it returns).
func newTestServer(t *testing.T) (*httptest.Server, *store.Store, *pgxpool.Pool, *fakeScraper) {
	t.Helper()

	pool := testdb.NewPool(t)
	st := store.New(pool)

	sc := &fakeScraper{slug: "kalibrr"}
	pl := pipeline.New(st, map[string]scraper.Scraper{"kalibrr": sc}, pipeline.Config{Logger: testLogger()})

	srv := New(pool, st, pl, testInternalToken, testLogger())

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	return ts, st, pool, sc
}
