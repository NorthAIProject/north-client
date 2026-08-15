-- name: InsertNudge :one
INSERT INTO user_nudges (user_id, kind, dedupe_key, title, body, href)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, kind, dedupe_key) DO NOTHING
RETURNING *;

-- name: ListOpenNudges :many
SELECT * FROM user_nudges
WHERE user_id = $1 AND dismissed_at IS NULL
ORDER BY created_at DESC
LIMIT $2;

-- name: CountUnreadNudges :one
SELECT count(*)::int FROM user_nudges
WHERE user_id = $1 AND dismissed_at IS NULL AND read_at IS NULL;

-- name: MarkNudgeRead :one
UPDATE user_nudges
SET read_at = now()
WHERE id = $1 AND user_id = $2 AND dismissed_at IS NULL
RETURNING *;

-- name: DismissNudge :one
UPDATE user_nudges
SET dismissed_at = now(),
    read_at      = COALESCE(read_at, now())
WHERE id = $1 AND user_id = $2 AND dismissed_at IS NULL
RETURNING *;
