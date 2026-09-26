-- name: UpsertSoreness :one
INSERT INTO soreness_logs (user_id, log_date, region, severity, note)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, log_date, region) DO UPDATE
SET severity = EXCLUDED.severity, note = EXCLUDED.note, logged_at = now()
RETURNING *;

-- name: DeleteSoreness :exec
DELETE FROM soreness_logs WHERE user_id = $1 AND log_date = $2 AND region = $3;

-- name: ListSorenessBetween :many
-- Half-open [since, until) on the calendar date, newest first.
SELECT * FROM soreness_logs
WHERE user_id = $1 AND log_date >= $2 AND log_date < $3
ORDER BY log_date DESC, region;
