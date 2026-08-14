-- name: UpsertSleepLog :one
INSERT INTO sleep_logs (
    user_id, local_date, duration_minutes, quality, bedtime, wake_time, notes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (user_id, local_date) DO UPDATE SET
    duration_minutes = EXCLUDED.duration_minutes,
    quality          = EXCLUDED.quality,
    bedtime          = EXCLUDED.bedtime,
    wake_time        = EXCLUDED.wake_time,
    notes            = EXCLUDED.notes,
    updated_at       = now()
RETURNING *;

-- name: GetSleepLogForDate :one
SELECT * FROM sleep_logs
WHERE user_id = $1 AND local_date = $2;

-- name: ListSleepLogs :many
SELECT * FROM sleep_logs
WHERE user_id = $1
ORDER BY local_date DESC
LIMIT $2;

-- name: ListSleepLogsBetween :many
-- Half-open [since, until).
SELECT * FROM sleep_logs
WHERE user_id = $1 AND local_date >= $2 AND local_date < $3
ORDER BY local_date DESC;

-- name: DeleteSleepLog :exec
DELETE FROM sleep_logs WHERE id = $1 AND user_id = $2;
