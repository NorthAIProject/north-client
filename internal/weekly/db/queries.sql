-- name: GetFocus :one
SELECT * FROM weekly_focus WHERE user_id = $1 AND week_start = $2;

-- name: UpsertFocus :one
INSERT INTO weekly_focus (user_id, week_start, priorities, volume)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, week_start) DO UPDATE
SET priorities = EXCLUDED.priorities, volume = EXCLUDED.volume, reviewed_at = now()
RETURNING *;
