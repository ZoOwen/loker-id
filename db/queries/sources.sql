-- name: GetSourceBySlug :one
SELECT * FROM sources WHERE slug = sqlc.arg('slug')::text;

-- name: UpdateSourceLastRunAt :exec
UPDATE sources SET last_run_at = NOW() WHERE id = sqlc.arg('id')::int;

-- name: SourceStats :many
-- Per-source active job count (0 for a source with none, via LEFT JOIN)
-- plus when it was last scraped, for GET /api/stats.
SELECT
    sources.id,
    sources.slug,
    sources.name,
    sources.last_run_at,
    COUNT(jobs.id) FILTER (WHERE jobs.is_active = TRUE AND jobs.canonical_job_id IS NULL) AS active_jobs
FROM sources
LEFT JOIN jobs ON jobs.source_id = sources.id
GROUP BY sources.id, sources.slug, sources.name, sources.last_run_at
ORDER BY sources.slug;
