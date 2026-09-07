-- name: InsertOAuthClient :one
INSERT INTO mcp_oauth_clients (id, client_name, redirect_uris, grant_types, software_id, dedupe_key)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- GetOAuthClientByDedupeKey returns an identical earlier registration.
--
-- A client that registers twice with the same metadata gets its existing row
-- back instead of a new one. This dedupes a web client with a stable callback;
-- it deliberately does not dedupe a native client whose callback port changes
-- every launch, which is what the sweep is for.
-- name: GetOAuthClientByDedupeKey :one
SELECT * FROM mcp_oauth_clients
WHERE dedupe_key = $1;

-- name: GetOAuthClient :one
SELECT * FROM mcp_oauth_clients
WHERE id = $1;

-- name: CountOAuthClients :one
SELECT count(*) FROM mcp_oauth_clients;

-- name: TouchOAuthClient :exec
UPDATE mcp_oauth_clients
SET last_used_at = now()
WHERE id = $1;

-- name: InsertAuthorizationRequest :one
INSERT INTO mcp_authorization_requests (
    client_id, redirect_uri, state, code_challenge, code_challenge_method,
    scope, resource, verifier_hash, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- GetAuthorizationRequest reads a parked request that is still usable.
--
-- The verifier hash is a predicate rather than a value compared afterwards, so
-- a guessed request id finds nothing at all: without it, anyone who could
-- guess an id could have a victim approve a request they started.
-- name: GetAuthorizationRequest :one
SELECT * FROM mcp_authorization_requests
WHERE id = $1
  AND verifier_hash = $2
  AND consumed_at IS NULL
  AND expires_at > now();

-- name: MarkAuthorizationRequestAccountCreated :exec
UPDATE mcp_authorization_requests
SET account_created = true
WHERE id = $1 AND verifier_hash = $2;

-- ConsumeAuthorizationRequest closes a request so Approve cannot be replayed.
--
-- execrows rather than exec: the caller needs to know it won the race, because
-- two Approve posts from a double-clicked button must not mint two codes.
-- name: ConsumeAuthorizationRequest :execrows
UPDATE mcp_authorization_requests
SET consumed_at = now()
WHERE id = $1
  AND verifier_hash = $2
  AND consumed_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredAuthorizationRequests :execrows
DELETE FROM mcp_authorization_requests
WHERE expires_at < now() - interval '1 day';

-- name: InsertAuthorizationCode :one
INSERT INTO mcp_authorization_codes (
    code_hash, client_id, user_id, redirect_uri, code_challenge,
    code_challenge_method, scope, resource, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- GetAuthorizationCode reads a code whether or not it has been used.
--
-- Deliberately unfiltered on consumed_at: presenting an already-consumed code
-- must revoke the whole grant (RFC 6749 section 10.5), and a query that hid
-- the row could not tell that case apart from a code that never existed.
-- name: GetAuthorizationCode :one
SELECT * FROM mcp_authorization_codes
WHERE code_hash = $1;

-- name: ConsumeAuthorizationCode :execrows
UPDATE mcp_authorization_codes
SET consumed_at = now(), connection_id = $2
WHERE code_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredAuthorizationCodes :execrows
DELETE FROM mcp_authorization_codes
WHERE expires_at < now() - interval '1 day';

-- name: InsertRefreshToken :one
INSERT INTO mcp_refresh_tokens (connection_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- Unfiltered on consumed_at for the same reason as the authorization code:
-- reuse of a consumed refresh token revokes the grant, so the row has to be
-- findable after it has been spent.
-- name: GetRefreshToken :one
SELECT * FROM mcp_refresh_tokens
WHERE token_hash = $1;

-- name: ConsumeRefreshToken :execrows
UPDATE mcp_refresh_tokens
SET consumed_at = now(), replaced_by = $2
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now();

-- LinkReplacedRefreshToken records which token superseded a spent one.
--
-- A separate statement because the replacement cannot exist before the row it
-- replaces has been claimed: consuming first is what stops a race from leaving
-- two live refresh tokens, so the chain is recorded a moment later.
-- name: LinkReplacedRefreshToken :exec
UPDATE mcp_refresh_tokens
SET replaced_by = $2
WHERE token_hash = $1;

-- name: ConsumeRefreshTokensForConnection :execrows
UPDATE mcp_refresh_tokens
SET consumed_at = now()
WHERE connection_id = $1 AND consumed_at IS NULL;

-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM mcp_refresh_tokens
WHERE expires_at < now() - interval '7 days';

-- DeleteUnusedOAuthClients removes registrations that never produced a grant.
--
-- Open registration means anyone can create a row, and a native client
-- registers a new one on every launch because its callback port changes. This
-- is what keeps that bounded without a human deciding anything.
-- name: DeleteUnusedOAuthClients :execrows
DELETE FROM mcp_oauth_clients c
WHERE c.created_at < now() - interval '30 days'
  AND NOT EXISTS (
      SELECT 1 FROM agent_connections a
      WHERE a.oauth_client_id = c.id
  );
