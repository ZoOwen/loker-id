-- name: CreateScrapeRun :one
-- Starts a run: the two-function lifecycle (Create/Finish) means there's
-- no separate "pending" phase — by the time a run row is created, the
-- scraper is about to start immediately.
INSERT INTO scrape_runs (source_id, status)
VALUES (sqlc.arg('source_id')::int, 'running')
RETURNING id;

-- name: FinishScrapeRun :exec
UPDATE scrape_runs
SET status         = sqlc.arg('status')::run_status,
    jobs_found     = sqlc.arg('jobs_found')::int,
    jobs_new       = sqlc.arg('jobs_new')::int,
    jobs_duplicate = sqlc.arg('jobs_duplicate')::int,
    error_message  = sqlc.narg('error_message')::text,
    finished_at    = NOW()
WHERE id = sqlc.arg('id')::uuid;
