-- name: ListDayRules :many
SELECT * FROM day_rules
WHERE user_id = $1
ORDER BY at_time, kind;

-- name: UpsertDayRule :one
INSERT INTO day_rules (user_id, kind, at_time, enabled)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, kind) DO UPDATE
SET at_time = EXCLUDED.at_time, enabled = EXCLUDED.enabled, updated_at = now()
RETURNING *;

-- name: DeleteDayRule :exec
DELETE FROM day_rules WHERE user_id = $1 AND kind = $2;
