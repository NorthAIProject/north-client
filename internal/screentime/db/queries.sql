-- name: UpsertScreenTime :one
INSERT INTO screen_time_logs (user_id, local_date, minutes, source)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, local_date) DO UPDATE
SET minutes = EXCLUDED.minutes, source = EXCLUDED.source, updated_at = now()
RETURNING *;

-- name: ScreenTimeForDate :one
SELECT * FROM screen_time_logs WHERE user_id = $1 AND local_date = $2;

-- name: ListScreenTimeBetween :many
SELECT * FROM screen_time_logs
WHERE user_id = $1 AND local_date >= $2 AND local_date < $3
ORDER BY local_date DESC;
