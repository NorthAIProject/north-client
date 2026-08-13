-- name: InsertAgentConnection :one
INSERT INTO agent_connections (user_id, name, client_kind, token_hash, token_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAgentConnections :many
SELECT * FROM agent_connections
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: GetAgentConnectionByTokenHash :one
SELECT * FROM agent_connections
WHERE token_hash = $1 AND revoked_at IS NULL;

-- TouchAgentConnection records that a token was used, but only once the
-- stored value has gone stale. An agent listing tools in a loop would
-- otherwise write on every request, and "last used" is never read at a
-- resolution that would notice the difference.
-- name: TouchAgentConnection :exec
UPDATE agent_connections
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes');

-- Scoped by user_id as well as id: the id comes from a form, and without the
-- second predicate a guessed id would revoke somebody else's connection.
-- name: RevokeAgentConnection :execrows
UPDATE agent_connections
SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;
