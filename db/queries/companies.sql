-- name: UpsertCompany :one
-- Inserts a company if name_normalized doesn't exist yet, otherwise
-- refreshes its display name and returns the existing id — callers always
-- get an id to attach jobs to, never an error over "already exists".
INSERT INTO companies (name, name_normalized)
VALUES (sqlc.arg('name')::text, sqlc.arg('name_normalized')::text)
ON CONFLICT (name_normalized) DO UPDATE SET name = EXCLUDED.name
RETURNING id;
