-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionUser :one
-- Resolves a session to its user in one round trip. Every authenticated
-- request runs this, so splitting it into two queries would double the
-- per-request database cost for no benefit.
SELECT sqlc.embed(u), s.expires_at, s.last_seen_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > now();

-- name: TouchSession :exec
-- Extends a session on activity, giving sliding expiry rather than a hard
-- logout mid-conversation.
UPDATE sessions
SET last_seen_at = now(),
    expires_at   = $2
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
-- Used on password change and on explicit "sign out everywhere".
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();
