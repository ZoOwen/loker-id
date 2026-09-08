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

## Deploying to Render

`render.yaml` is a blueprint for a Docker-based web service; `Dockerfile`
builds a static binary (`CGO_ENABLED=0`, pure-Go deps only) on
`golang:alpine` and runs it on `gcr.io/distroless/static-debian12`.

- **Port**: Render assigns its own `PORT` for Docker services (not always
  8080) and routes traffic to it. `internal/config` already reads `PORT`
  from the environment, so nothing needs to change — just don't set `PORT`
  yourself in the Render dashboard/blueprint, or you'd override what
  Render assigns.
- **DATABASE_URL / INTERNAL_TOKEN**: marked `sync: false` in
  `render.yaml` — set them manually in the Render dashboard rather than
  committing them. Neon's connection string must keep `?sslmode=require`
  (see `.env.example`); pgx reads `sslmode` straight off the URL, so
  there's no separate flag to set.
- **Connection pool**: `internal/database.NewPool` caps the pgxpool at
  `DB_MAX_CONNS` (env var, default 5 — see `.env.example`) instead of
  pgxpool's own CPU-count-scaled default, to stay well under Neon
  free-tier's connection limit.
- **`/healthz`**: pings the DB with a 5s timeout, generous enough that
  Neon's scale-to-zero cold start (~1s for the first query after being
  idle) doesn't trip it, while a genuinely unreachable DB still fails the
  check in bounded time.
- **Migrations are not run automatically** by the image or the blueprint.
  Apply `migrations/001_init.sql` to the Neon database yourself (`make
  migrate-up` with `DATABASE_URL` pointed at Neon) before or after the
  first deploy.

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

**Country lock.** Every request is prefixed with `/id-ID` (Next.js's own
locale-routing path prefix). This exists because of a real incident: once
deployed to Render's Singapore region, every scraped job came back
Filipino (Makati, Pasig, Quezon City...) with zero Indonesian listings,
while the same code run from an Indonesia-resident IP was fine. Kalibrr's
unprefixed routes pick a country by geo-IP (confirmed via the page's own
`__NEXT_DATA__`, which carries both a `geoCountry` field and Next.js's
`locale`/`locales` metadata — exactly two configured locales, `en` and
`id-ID`); Render's Singapore egress IP apparently geolocates as the
Philippines on Kalibrr's (or Cloudflare's, which fronts kalibrr.com) side.
The `/id-ID` prefix pins the locale server-side regardless of the
requester's IP — verified by fetching it directly and checking both
`__NEXT_DATA__.locale` and every job's country. See the doc comment on
`kalibrrLocalePrefix` in `internal/scraper/kalibrr.go` for the full
investigation. `checkKalibrrCountry` also logs a warning if a scrape ever
comes back with a job outside Indonesia anyway, so a regression here
surfaces immediately instead of silently polluting the database again.

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
