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
