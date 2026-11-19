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

-- name: CountUserMediaByKind :one
SELECT count(*)::int FROM media WHERE user_id = $1 AND kind = $2;

-- name: LatestUserMediaCreatedAt :one
SELECT created_at FROM media
WHERE user_id = $1 AND kind = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: LatestUserMediaOfKinds :one
-- The newest of a person's files of the given kinds, optionally the newest
-- with a given name, compared case-insensitively.
SELECT * FROM media
WHERE user_id = @user_id
  AND kind = ANY(@kinds::text[])
  AND (@name::text = '' OR lower(original_name) = lower(@name::text))
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: CreateAnalysis :one
INSERT INTO form_analyses (media_id, user_id, duration_seconds, exercise_slug)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AddAnalysisFrame :exec
INSERT INTO form_analysis_frames (analysis_id, idx, media_id, at_seconds)
VALUES ($1, $2, $3, $4);

-- name: ListAnalysisFrames :many
-- The stills of one check, in clip order, with where each is stored.
SELECT f.idx, f.media_id, f.at_seconds, m.storage_key, m.mime_type
FROM form_analysis_frames f
JOIN media m ON m.id = f.media_id
WHERE f.analysis_id = $1
ORDER BY f.idx;

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

-- name: DeleteMedia :one
-- Owner-scoped. Returns the storage key so the caller can remove the object.
DELETE FROM media WHERE id = $1 AND user_id = $2
RETURNING storage_key;
