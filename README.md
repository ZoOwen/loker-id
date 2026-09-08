# loker-id

Aggregates Indonesian dev-job listings from job boards (currently Kalibrr)
into a single searchable API: Go, chi, pgx/v5, sqlc, colly, goose.

## Running locally

```
make tools        # one-time: installs sqlc + goose
make migrate-up    # apply migrations/001_init.sql
make run           # starts the API server
make test          # full test suite (needs TEST_DATABASE_URL — see .env.example)
```

See `Makefile` for the rest of the targets (`sqlc`, `migrate-*`, `test-db-setup`).

## API

- `GET /api/jobs` — list active jobs, keyset-paginated, filterable by stack/salary/mode/level/city/query.
- `GET /api/jobs/{id}`
- `GET /api/stats` — active job count + a per-technology breakdown.
- `POST /internal/scrape?source=kalibrr&pages=<1-20>` — trigger a scrape run. Requires `X-Internal-Token`. Async: returns 202 immediately, runs in the background.

## Scraping strategy: keywords, not deep pagination

`internal/scraper.KalibrrScraper` gets its coverage by running one search
per keyword (`DefaultKalibrrKeywords` in `internal/scraper/kalibrr.go`:
backend, frontend, fullstack, devops, mobile, data engineer, qa, golang,
react, python, java, php, nodejs, android, ios) and aggregating the
results, rather than by paging deep into any single keyword's results.

This is a deliberate call, not an unnoticed limitation. As of 2026-09-09,
Kalibrr's own server-side pagination for `/job-board/te/{keyword}/{page}`
is broken: the page number reaches their Next.js router correctly, but
the query offset it computes (`pageProps.filters.offset` in the page's
own `__NEXT_DATA__` payload) stays `0` on every page — verified by
fetching pages 1, 2, and 3 directly and diffing their embedded JSON, and
by trying `?page=N` / `?offset=N` query-string variants, none of which
changed it. Every page just replays the same first ~15 results. Our own
`maxPages`/early-stop logic (page-duplicates-previous-page detection) is
working correctly here — it's what causes a run to stop after one page
per keyword — there's simply nothing further to page into right now.

If Kalibrr fixes their pagination, this scraper starts walking multiple
pages per keyword automatically; nothing here assumes it's permanently
broken, it's just not relied on.

One consequence: the same real posting routinely surfaces under more than
one keyword (e.g. a Go backend role matches both "backend" and "golang").
That's expected, and it's `internal/store`'s existing dedup — keyed on
`(source_id, source_url)` for an exact re-scrape, fingerprint/fuzzy-title
matching for a genuinely different URL — that collapses it back down to
one row. `store.UpsertJobResult.WasInserted` distinguishes a fresh row
from a re-scrape of an already-known one, which is what
`Pipeline.Run`'s `JobsNew`/`JobsDuplicate` counts (and its per-run log
line) are built on — so a run's duplicate count genuinely reflects
cross-keyword overlap, not just cross-source fuzzy matches.
