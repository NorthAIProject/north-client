-- name: CreateHydrationEntry :one
INSERT INTO hydration_logs (user_id, log_date, amount_ml)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteHydrationEntry :exec
DELETE FROM hydration_logs WHERE id = $1 AND user_id = $2;

-- name: ListHydrationEntriesForDate :many
SELECT * FROM hydration_logs
WHERE user_id = $1 AND log_date = $2
ORDER BY logged_at DESC;

-- name: SumHydrationForDate :one
SELECT
    COALESCE(SUM(amount_ml), 0)::bigint AS total_ml,
    COUNT(*)::bigint                    AS entry_count
FROM hydration_logs
WHERE user_id = $1 AND log_date = $2;

-- name: SumHydrationByDateSince :many
SELECT
    log_date,
    COALESCE(SUM(amount_ml), 0)::bigint AS total_ml,
    COUNT(*)::bigint                    AS entry_count
FROM hydration_logs
WHERE user_id = $1 AND log_date >= $2
GROUP BY log_date
ORDER BY log_date DESC;
