-- name: UpsertCompany :one
-- Inserts a company if name_normalized doesn't exist yet, otherwise
-- refreshes its display name and returns the existing id — callers always
-- get an id to attach jobs to, never an error over "already exists".
INSERT INTO companies (name, name_normalized)
VALUES (sqlc.arg('name')::text, sqlc.arg('name_normalized')::text)
ON CONFLICT (name_normalized) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: ListCompaniesByIDs :many
-- Batch lookup for enriching a page of jobs with their company's display
-- name, e.g. in an HTTP handler — one query per page instead of one per
-- row.
SELECT * FROM companies WHERE id = ANY(sqlc.arg('ids')::uuid[]);
