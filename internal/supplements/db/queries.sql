-- name: CreateSupplementEntry :one
INSERT INTO supplement_logs (user_id, log_date, name, count, nutrients, logged_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteSupplementEntry :execrows
DELETE FROM supplement_logs WHERE id = $1 AND user_id = $2;

-- name: ListSupplementsBetween :many
SELECT * FROM supplement_logs
WHERE user_id = $1 AND logged_at >= $2 AND logged_at < $3
ORDER BY logged_at DESC;
