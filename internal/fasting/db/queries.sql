-- name: StartFast :one
INSERT INTO fasting_sessions (user_id, started_at, target_hours)
VALUES ($1, $2, $3)
RETURNING *;

-- name: StopFast :one
UPDATE fasting_sessions SET ended_at = $2
WHERE user_id = $1 AND ended_at IS NULL
RETURNING *;

-- name: OpenFast :one
SELECT * FROM fasting_sessions WHERE user_id = $1 AND ended_at IS NULL;

-- name: ListFastsOverlapping :many
-- Every fast that touches [since, until), open ones included, newest first.
SELECT * FROM fasting_sessions
WHERE user_id = $1 AND started_at < $3 AND (ended_at IS NULL OR ended_at > $2)
ORDER BY started_at DESC;

-- name: DeleteFast :execrows
DELETE FROM fasting_sessions WHERE id = $1 AND user_id = $2;
