-- name: CreateWatch :one
INSERT INTO coach_watches (user_id, conversation_id, title, condition, spec, cadence, weekday, minute_of_day, next_run_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListWatches :many
SELECT * FROM coach_watches
WHERE user_id = $1 AND active
ORDER BY created_at;

-- name: DueWatches :many
-- Oldest-due first, so a backlog after an outage drains in the order it built.
SELECT * FROM coach_watches
WHERE active AND next_run_at <= $1
ORDER BY next_run_at
LIMIT $2;

-- name: ClaimWatchRun :execrows
-- Moves the watch to its next slot only if nobody else already has. Two
-- sweeps that both read the row as due race here, and exactly one of them
-- sees a row affected; the other skips it. This is the double-run guard.
UPDATE coach_watches
SET next_run_at = sqlc.arg(next_run_at), last_run_at = sqlc.arg(ran_at)
WHERE id = sqlc.arg(id) AND active AND next_run_at = sqlc.arg(due_at);
