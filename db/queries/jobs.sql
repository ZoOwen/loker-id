-- name: UpsertJob :one
-- Inserts a job, or — keyed on (source_id, source_url), which is what a
-- re-scrape of the same listing shares — refreshes its content and marks
-- it seen again. canonical_job_id is deliberately absent from both the
-- insert column list and the UPDATE SET: a re-scrape must never clobber a
-- dedup decision already made for this row. salary_currency is absent
-- too — our parser only ever produces IDR (or "unknown", which carries
-- no currency at all), so there's nothing meaningful to write; the
-- column's own DEFAULT 'IDR' covers the insert path, and an update
-- simply leaves whatever is already there. Returns the full row (not
-- just id): the store layer needs posted_at/first_seen_at/fingerprint/
-- company_id/title_normalized right after upserting to run dedup
-- resolution, and a second SELECT to fetch them would be redundant
-- inside the same transaction.
INSERT INTO jobs (
    title, title_normalized, company_id, description,
    salary_min, salary_max, salary_conf, salary_raw,
    stack, location, location_city, mode, level,
    source_id, source_url, source_job_id,
    fingerprint, posted_at
) VALUES (
    sqlc.arg('title')::text, sqlc.arg('title_normalized')::text, sqlc.arg('company_id')::uuid, sqlc.narg('description')::text,
    sqlc.narg('salary_min')::bigint, sqlc.narg('salary_max')::bigint, sqlc.arg('salary_conf')::salary_confidence, sqlc.narg('salary_raw')::text,
    sqlc.arg('stack')::text[], sqlc.narg('location')::text, sqlc.narg('location_city')::text, sqlc.arg('mode')::work_mode, sqlc.arg('level')::experience_level,
    sqlc.arg('source_id')::int, sqlc.arg('source_url')::text, sqlc.narg('source_job_id')::text,
    sqlc.arg('fingerprint')::text, sqlc.narg('posted_at')::timestamptz
)
ON CONFLICT (source_id, source_url) DO UPDATE SET
    title             = EXCLUDED.title,
    title_normalized  = EXCLUDED.title_normalized,
    company_id        = EXCLUDED.company_id,
    description       = EXCLUDED.description,
    salary_min        = EXCLUDED.salary_min,
    salary_max        = EXCLUDED.salary_max,
    salary_conf       = EXCLUDED.salary_conf,
    salary_raw        = EXCLUDED.salary_raw,
    stack             = EXCLUDED.stack,
    location          = EXCLUDED.location,
    location_city     = EXCLUDED.location_city,
    mode              = EXCLUDED.mode,
    level             = EXCLUDED.level,
    source_job_id     = EXCLUDED.source_job_id,
    fingerprint       = EXCLUDED.fingerprint,
    posted_at         = EXCLUDED.posted_at,
    last_seen_at      = NOW(),
    is_active         = TRUE
RETURNING *;

-- name: FindDuplicateCandidates :many
-- Other canonical (not-yet-deduped) jobs that look like the same posting
-- as the one identified by exclude_job_id: either an exact fingerprint
-- match, or the same company with a fuzzy-matching title. The 0.7
-- similarity threshold and what to do with the results is
-- internal/store's call, not this query's — this just surfaces
-- candidates, ordered oldest-first so the likely canonical sorts first.
SELECT *
FROM jobs
WHERE canonical_job_id IS NULL
  AND id != sqlc.arg('exclude_job_id')::uuid
  AND (
    fingerprint = sqlc.arg('fingerprint')::text
    OR (
      company_id = sqlc.arg('company_id')::uuid
      AND similarity(title_normalized, sqlc.arg('title_normalized')::text) > 0.7
    )
  )
ORDER BY posted_at ASC NULLS LAST, first_seen_at ASC;

-- name: MarkAsDuplicate :exec
UPDATE jobs
SET canonical_job_id = sqlc.arg('canonical_job_id')::uuid
WHERE id = sqlc.arg('job_id')::uuid;

-- name: ReparentDuplicates :exec
-- When a merge displaces a previous canonical (an earlier-posted match
-- for the same job surfaces later), anything already pointing at the
-- demoted job needs to point straight at the new canonical instead —
-- otherwise duplicates would form a two-level chain (dup -> old canonical
-- -> new canonical) instead of every duplicate pointing directly at the
-- true canonical.
UPDATE jobs
SET canonical_job_id = sqlc.arg('new_canonical_id')::uuid
WHERE canonical_job_id = sqlc.arg('old_canonical_id')::uuid;

-- name: ListJobs :many
-- Keyset pagination on (posted_at, id), both descending — never OFFSET.
-- has_cursor distinguishes "first page" from "page after a row whose own
-- posted_at happens to be NULL"; overloading cursor_posted_at itself as
-- that sentinel would conflate the two. NULL posted_at is treated as
-- '-infinity' throughout (both here and in ORDER BY) so unknown-posted-
-- date jobs consistently sort last instead of needing special-case
-- handling in the keyset comparison.
SELECT *
FROM jobs
WHERE canonical_job_id IS NULL
  AND is_active = TRUE
  AND (sqlc.narg('stack')::text[] IS NULL OR stack && sqlc.narg('stack')::text[])
  -- "at least X" reads as "the job could pay at least X" (its upper
  -- bound clears the bar), not "its floor guarantees X" — the more
  -- permissive of the two reasonable readings, and what similar job
  -- boards do. Jobs with no known salary_max never match an active filter.
  AND (sqlc.narg('salary_min')::bigint IS NULL OR salary_max >= sqlc.narg('salary_min')::bigint)
  AND (sqlc.narg('mode')::work_mode IS NULL OR mode = sqlc.narg('mode')::work_mode)
  AND (sqlc.narg('level')::experience_level IS NULL OR level = sqlc.narg('level')::experience_level)
  AND (sqlc.narg('city')::text IS NULL OR location_city ILIKE sqlc.narg('city')::text)
  -- 'simple' must match the config the search_vector trigger indexes
  -- with (see migrations/001_init.sql) — querying with a different
  -- config's stemming/stopword rules would silently stop matching.
  AND (sqlc.narg('query')::text IS NULL OR search_vector @@ websearch_to_tsquery('simple', sqlc.narg('query')::text))
  AND (
    NOT sqlc.arg('has_cursor')::bool
    OR (COALESCE(posted_at, '-infinity'::timestamptz), id) <
       (COALESCE(sqlc.narg('cursor_posted_at')::timestamptz, '-infinity'::timestamptz), sqlc.narg('cursor_id')::uuid)
  )
ORDER BY COALESCE(posted_at, '-infinity'::timestamptz) DESC, id DESC
LIMIT sqlc.arg('page_limit')::int;

-- name: DeactivateStaleJobs :execrows
UPDATE jobs
SET is_active = FALSE
WHERE is_active = TRUE
  AND last_seen_at < NOW() - (sqlc.arg('stale_after_days')::int * INTERVAL '1 day');

-- name: GetJobByID :one
SELECT * FROM jobs WHERE id = sqlc.arg('id')::uuid;

-- name: ListDuplicatesOf :many
-- Every duplicate of canonical_job_id, per our reparenting invariant
-- (see ReparentDuplicates): always a flat one-level pointer, never a
-- chain, so this alone is the complete set — no recursion needed.
SELECT * FROM jobs WHERE canonical_job_id = sqlc.arg('canonical_job_id')::uuid ORDER BY first_seen_at ASC;

-- name: CountActiveJobs :one
SELECT count(*) FROM jobs WHERE is_active = TRUE AND canonical_job_id IS NULL;

-- name: StackStats :many
-- One row per technology across all active, canonical jobs, most common
-- first. unnest() fans a job's stack array out into one row per element.
SELECT unnest(stack)::text AS stack, count(*) AS job_count
FROM jobs
WHERE is_active = TRUE AND canonical_job_id IS NULL
GROUP BY stack
ORDER BY job_count DESC, stack ASC;
