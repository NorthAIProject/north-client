-- name: CreateMedia :one
INSERT INTO media (user_id, kind, mime_type, size_bytes, storage_key, original_name)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMedia :one
SELECT * FROM media WHERE id = $1 AND user_id = $2;

-- name: GetMediaByID :one
-- Unscoped, for the worker, which acts on behalf of the system rather than a
-- signed-in user. Never reachable from a handler.
SELECT * FROM media WHERE id = $1;

-- name: CreateAnalysis :one
INSERT INTO form_analyses (media_id, user_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetAnalysis :one
SELECT * FROM form_analyses WHERE id = $1 AND user_id = $2;

-- name: GetAnalysisByMedia :one
SELECT * FROM form_analyses WHERE media_id = $1;

-- name: ListAnalyses :many
SELECT * FROM form_analyses
WHERE user_id = $1 AND status = 'done'
ORDER BY created_at DESC
LIMIT $2;

-- name: StartAnalysis :exec
UPDATE form_analyses
SET status = 'running', updated_at = now()
WHERE id = $1;

-- name: CompleteAnalysis :exec
UPDATE form_analyses
SET status = 'done', analysis = $2, model = $3, provider = $4, updated_at = now()
WHERE id = $1;

-- name: FailAnalysis :exec
UPDATE form_analyses
SET status = 'failed', error = $2, updated_at = now()
WHERE id = $1;
