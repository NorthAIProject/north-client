-- name: InsertAgentConnection :one
INSERT INTO agent_connections (user_id, name, client_kind, token_hash, token_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- No expiry predicate here, deliberately. One row is one *grant*, not one
-- access token: an OAuth row's token_hash is rotated in place every hour, so a
-- past expires_at means "the access token needs refreshing", not "this
-- connection is gone". Filtering on it would make the settings page hide a
-- working connection an hour after it was made. The page renders the expired
-- state instead.
-- name: ListAgentConnections :many
SELECT * FROM agent_connections
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- The expiry predicate covers OAuth access tokens, which last an hour. A
-- hand-issued token has a NULL expires_at and is unaffected, which is what
-- keeps every connection issued before OAuth existed working unchanged.
-- name: GetAgentConnectionByTokenHash :one
SELECT * FROM agent_connections
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- TouchAgentConnection records that a token was used, but only once the
-- stored value has gone stale. An agent listing tools in a loop would
-- otherwise write on every request, and "last used" is never read at a
-- resolution that would notice the difference.
-- name: TouchAgentConnection :exec
UPDATE agent_connections
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes');

-- RevokeGrantByID turns off a connection without naming its owner.
--
-- Deliberately unscoped, and safe only because of who calls it: the OAuth
-- revocation endpoint, where the caller has already proved possession of a
-- token belonging to this exact row, and the replay path, where the id came
-- from a code row rather than from a request. Never call it with an id that
-- came from a form — that is what RevokeAgentConnection below is for.
-- name: RevokeGrantByID :execrows
UPDATE agent_connections
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- Scoped by user_id as well as id: the id comes from a form, and without the
-- second predicate a guessed id would revoke somebody else's connection.
-- name: RevokeAgentConnection :execrows
UPDATE agent_connections
SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- InsertOAuthGrant creates the connection row for an approved consent.
--
-- Same table as a hand-issued token, so one revoke button covers both kinds
-- and the settings page needs no second list. token_prefix is set once here
-- from the first access token and never updated by a rotation, so the page has
-- something stable to show; it stops being a prefix of the live token after
-- the first refresh, which is fine because it was only ever a label.
-- name: InsertOAuthGrant :one
INSERT INTO agent_connections (
    user_id, name, client_kind, token_hash, token_prefix,
    scopes, expires_at, issuance, resource, oauth_client_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'oauth', $8, $9)
RETURNING *;

-- RotateAgentConnectionToken swaps in a freshly issued access token.
--
-- An update rather than an insert, because a row per hourly token would grow
-- the settings list by twenty-four entries a day and turn one revoke button
-- into a chore. created_at stays the date the grant was approved.
-- name: RotateAgentConnectionToken :execrows
UPDATE agent_connections
SET token_hash = $2,
    expires_at = $3
WHERE id = $1 AND revoked_at IS NULL AND issuance = 'oauth';
