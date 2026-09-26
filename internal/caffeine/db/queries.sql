-- name: CreateCaffeineEntry :one
INSERT INTO caffeine_logs (user_id, log_date, mg, label, logged_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteCaffeineEntry :execrows
DELETE FROM caffeine_logs WHERE id = $1 AND user_id = $2;

-- name: ListCaffeineBetween :many
-- Half-open [since, until) on when it was drunk, newest first.
SELECT * FROM caffeine_logs
WHERE user_id = $1 AND logged_at >= $2 AND logged_at < $3
ORDER BY logged_at DESC;
