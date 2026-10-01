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

-- name: ResolveNudgesForDay :many
-- Closes every nudge of a kind raised for one local day: the day's own key,
-- or a key scoped under it ("2026-10-01:<reminder id>"). Rows already
-- dismissed keep their time and come back too, so a chat message that carried
-- one can still be edited.
UPDATE user_nudges
SET dismissed_at = COALESCE(dismissed_at, now()),
    read_at      = COALESCE(read_at, now())
WHERE user_id = sqlc.arg(user_id)
  AND kind = sqlc.arg(kind)
  AND (dedupe_key = sqlc.arg(day)::text OR dedupe_key LIKE sqlc.arg(day)::text || ':%')
RETURNING *;
