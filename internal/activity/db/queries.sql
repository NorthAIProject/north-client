-- name: CreateActivitySession :one
INSERT INTO activity_sessions (user_id, activity_code, weight_kg_snapshot)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetActivitySession :one
SELECT * FROM activity_sessions WHERE id = $1 AND user_id = $2;

-- name: ActiveActivitySession :one
SELECT * FROM activity_sessions WHERE user_id = $1 AND status IN ('active', 'paused');

-- name: PauseActivitySession :one
UPDATE activity_sessions
SET status = 'paused', paused_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ResumeActivitySession :one
UPDATE activity_sessions
SET status = 'active', paused_at = NULL, total_paused_seconds = $3, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CompleteActivitySession :one
UPDATE activity_sessions
SET status = 'completed', ended_at = $3, calories_burned = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CancelActivitySession :exec
UPDATE activity_sessions
SET status = 'cancelled', ended_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: ListActivitySessions :many
SELECT * FROM activity_sessions
WHERE user_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: SumActivityCaloriesSince :one
SELECT COALESCE(SUM(calories_burned), 0)::double precision AS total
FROM activity_sessions
WHERE user_id = $1 AND status = 'completed' AND ended_at >= $2;

-- name: SumActivityCaloriesBetween :one
-- Half-open [since, until), so this window and the one before it can be
-- compared without double-counting the session on the boundary.
SELECT COALESCE(SUM(calories_burned), 0)::double precision AS total
FROM activity_sessions
WHERE user_id = $1 AND status = 'completed'
  AND ended_at >= $2 AND ended_at < $3;

-- name: ListActivitySessionsBetween :many
-- Completed sessions only: an abandoned or still-running session is not
-- something that happened.
SELECT * FROM activity_sessions
WHERE user_id = $1 AND status = 'completed'
  AND ended_at >= $2 AND ended_at < $3
ORDER BY ended_at DESC;

-- name: ImportActivitySession :one
-- A finished session written in one shot, for a provider sync rather than
-- the in-app start/stop lifecycle. Deduped by the existing
-- UNIQUE (source, external_id) index, so re-importing is a no-op.
INSERT INTO activity_sessions (
    user_id, activity_code, source, status, weight_kg_snapshot,
    started_at, ended_at, calories_burned, external_id
) VALUES (
    $1, $2, $3, 'completed', $4, $5, $6, $7, $8
)
ON CONFLICT (source, external_id) WHERE external_id IS NOT NULL DO NOTHING
RETURNING *;
